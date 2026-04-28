package client

// Request body types — one per daemon endpoint family.

type submitTaskRequest struct {
	Prompt      string `json:"prompt"`
	ReplyTo     string `json:"reply_to"`
	Priority    int    `json:"priority"`
	AllowWrites bool   `json:"allow_writes"`
	TimeoutSec  int    `json:"timeout_sec"`
	Warm        bool   `json:"warm"`
}

type saveMemoryRequest struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Source  string   `json:"source"`
}

type (
	gmailListRequest struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
		Account    string `json:"account"`
	}
	gmailReadRequest struct {
		MessageID string `json:"message_id"`
		Account   string `json:"account"`
	}
	gmailSendRequest struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
		Account string `json:"account"`
	}
	gmailReplyRequest struct {
		MessageID string `json:"message_id"`
		Body      string `json:"body"`
		Account   string `json:"account"`
	}
)

type (
	gcalAccountRequest struct {
		Account string `json:"account"`
	}
	gcalGetRequest struct {
		EventID string `json:"event_id"`
		Account string `json:"account"`
	}
	gcalCreateRequest struct {
		Title       string `json:"title"`
		Start       string `json:"start"`
		End         string `json:"end"`
		Description string `json:"description"`
		Account     string `json:"account"`
	}
	gcalUpdateRequest struct {
		EventID string `json:"event_id"`
		Title   string `json:"title"`
		Start   string `json:"start"`
		End     string `json:"end"`
		Account string `json:"account"`
	}
	gcalDeleteRequest struct {
		EventID string `json:"event_id"`
		Account string `json:"account"`
	}
)
