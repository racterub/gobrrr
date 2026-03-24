package google

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// GmailAPI is the interface for Gmail operations. Implementations include
// GmailService (real API) and MockGmailAPI (tests).
type GmailAPI interface {
	ListMessages(query string, maxResults int) ([]*MessageSummary, error)
	ReadMessage(id string) (*MessageDetail, error)
	SendMessage(to, subject, body string) error
	ReplyMessage(messageID, body string) error
}

// MessageSummary holds a brief summary of an email message.
type MessageSummary struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Snippet string `json:"snippet"`
	Unread  bool   `json:"unread"`
}

// MessageDetail extends MessageSummary with the full message body and
// attachment filenames.
type MessageDetail struct {
	MessageSummary
	Body        string   `json:"body"`
	Attachments []string `json:"attachments"`
}

// GmailService implements GmailAPI using the real Google Gmail API.
type GmailService struct {
	svc *gmail.Service
}

// NewGmailService creates a GmailService backed by the provided HTTP client.
func NewGmailService(client *http.Client) (*GmailService, error) {
	svc, err := gmail.NewService(nil, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("gmail: create service: %w", err)
	}
	return &GmailService{svc: svc}, nil
}

// ListMessages returns message summaries matching the given query. The results
// are limited to maxResults (capped at 500). If maxResults is 0, defaults to 10.
func (g *GmailService) ListMessages(query string, maxResults int) ([]*MessageSummary, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 500 {
		maxResults = 500
	}

	var result []*MessageSummary
	err := WithRetry(func() error {
		resp, err := g.svc.Users.Messages.List("me").Q(query).MaxResults(int64(maxResults)).Do()
		if err != nil {
			return err
		}

		summaries := make([]*MessageSummary, 0, len(resp.Messages))
		for _, m := range resp.Messages {
			msg, err := g.svc.Users.Messages.Get("me", m.Id).Format("metadata").
				MetadataHeaders("From", "Subject", "Date").Do()
			if err != nil {
				return err
			}
			s := &MessageSummary{
				ID:      msg.Id,
				Snippet: msg.Snippet,
				Unread:  hasLabel(msg.LabelIds, "UNREAD"),
			}
			for _, h := range msg.Payload.Headers {
				switch h.Name {
				case "From":
					s.From = h.Value
				case "Subject":
					s.Subject = h.Value
				case "Date":
					s.Date = h.Value
				}
			}
			summaries = append(summaries, s)
		}
		result = summaries
		return nil
	})
	return result, err
}

// ReadMessage fetches the full content of the message with the given ID.
// The Body field is wrapped with UNTRUSTED boundaries.
func (g *GmailService) ReadMessage(messageID string) (*MessageDetail, error) {
	var detail *MessageDetail
	err := WithRetry(func() error {
		msg, err := g.svc.Users.Messages.Get("me", messageID).Format("full").Do()
		if err != nil {
			return err
		}

		d := &MessageDetail{
			MessageSummary: MessageSummary{
				ID:      msg.Id,
				Snippet: msg.Snippet,
				Unread:  hasLabel(msg.LabelIds, "UNREAD"),
			},
		}
		for _, h := range msg.Payload.Headers {
			switch h.Name {
			case "From":
				d.From = h.Value
			case "Subject":
				d.Subject = h.Value
			case "Date":
				d.Date = h.Value
			}
		}

		body, attachments := extractBody(msg.Payload)
		d.Body = WrapEmail(d.From, d.Subject, body)
		d.Attachments = attachments
		detail = d
		return nil
	})
	return detail, err
}

// SendMessage sends an email to the given recipient.
func (g *GmailService) SendMessage(to, subject, body string) error {
	return WithRetry(func() error {
		raw := buildRawMessage("", to, subject, body)
		msg := &gmail.Message{Raw: raw}
		_, err := g.svc.Users.Messages.Send("me", msg).Do()
		return err
	})
}

// ReplyMessage sends a reply to the message with the given ID.
func (g *GmailService) ReplyMessage(messageID, body string) error {
	return WithRetry(func() error {
		// Fetch the original message to get headers needed for threading.
		orig, err := g.svc.Users.Messages.Get("me", messageID).
			Format("metadata").
			MetadataHeaders("From", "Subject", "Message-ID", "References").
			Do()
		if err != nil {
			return err
		}

		var from, subject, msgID, references string
		for _, h := range orig.Payload.Headers {
			switch h.Name {
			case "From":
				from = h.Value
			case "Subject":
				subject = h.Value
			case "Message-ID":
				msgID = h.Value
			case "References":
				references = h.Value
			}
		}

		// Build the In-Reply-To / References chain.
		replySubject := subject
		if !strings.HasPrefix(strings.ToLower(subject), "re:") {
			replySubject = "Re: " + subject
		}
		refs := references
		if msgID != "" {
			if refs != "" {
				refs = refs + " " + msgID
			} else {
				refs = msgID
			}
		}

		raw := buildReplyRawMessage("", from, replySubject, body, msgID, refs, orig.ThreadId)
		msg := &gmail.Message{
			Raw:      raw,
			ThreadId: orig.ThreadId,
		}
		_, err = g.svc.Users.Messages.Send("me", msg).Do()
		return err
	})
}

// --- helpers ---

// hasLabel returns true if labelID is present in the list.
func hasLabel(labels []string, labelID string) bool {
	for _, l := range labels {
		if l == labelID {
			return true
		}
	}
	return false
}

// extractBody recursively walks the MIME part tree and returns the plain-text
// body and a list of attachment filenames.
func extractBody(part *gmail.MessagePart) (body string, attachments []string) {
	if part == nil {
		return "", nil
	}

	if part.Filename != "" {
		attachments = append(attachments, part.Filename)
	}

	switch part.MimeType {
	case "text/plain":
		if part.Body != nil && part.Body.Data != "" {
			data, err := base64.URLEncoding.DecodeString(part.Body.Data)
			if err == nil {
				body = string(data)
			}
		}
	default:
		for _, p := range part.Parts {
			b, a := extractBody(p)
			if body == "" && b != "" {
				body = b
			}
			attachments = append(attachments, a...)
		}
	}
	return body, attachments
}

// buildRawMessage constructs a base64url-encoded RFC 2822 message.
func buildRawMessage(from, to, subject, body string) string {
	var sb strings.Builder
	if from != "" {
		sb.WriteString("From: " + from + "\r\n")
	}
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return base64.URLEncoding.EncodeToString([]byte(sb.String()))
}

// buildReplyRawMessage constructs a base64url-encoded RFC 2822 reply message
// with proper threading headers.
func buildReplyRawMessage(from, to, subject, body, inReplyTo, references, _ string) string {
	var sb strings.Builder
	if from != "" {
		sb.WriteString("From: " + from + "\r\n")
	}
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + subject + "\r\n")
	if inReplyTo != "" {
		sb.WriteString("In-Reply-To: " + inReplyTo + "\r\n")
	}
	if references != "" {
		sb.WriteString("References: " + references + "\r\n")
	}
	sb.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(body)
	return base64.URLEncoding.EncodeToString([]byte(sb.String()))
}
