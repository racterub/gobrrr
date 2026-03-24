package google

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockGmailAPI implements GmailAPI for use in tests.
type MockGmailAPI struct {
	Messages    []*MessageSummary
	Detail      *MessageDetail
	SentTo      string
	SentSubject string
	SentBody    string
	ReplyID     string
	ReplyBody   string
	ListErr     error
	ReadErr     error
	SendErr     error
	ReplyErr    error
}

func (m *MockGmailAPI) ListMessages(query string, maxResults int) ([]*MessageSummary, error) {
	if m.ListErr != nil {
		return nil, m.ListErr
	}
	return m.Messages, nil
}

func (m *MockGmailAPI) ReadMessage(id string) (*MessageDetail, error) {
	if m.ReadErr != nil {
		return nil, m.ReadErr
	}
	return m.Detail, nil
}

func (m *MockGmailAPI) SendMessage(to, subject, body string) error {
	m.SentTo = to
	m.SentSubject = subject
	m.SentBody = body
	return m.SendErr
}

func (m *MockGmailAPI) ReplyMessage(messageID, body string) error {
	m.ReplyID = messageID
	m.ReplyBody = body
	return m.ReplyErr
}

func TestListMessages(t *testing.T) {
	mock := &MockGmailAPI{
		Messages: []*MessageSummary{
			{ID: "msg1", From: "alice@example.com", Subject: "Hello", Unread: true},
			{ID: "msg2", From: "bob@example.com", Subject: "Hi", Unread: false},
		},
	}

	msgs, err := mock.ListMessages("is:unread", 10)
	require.NoError(t, err)
	assert.Len(t, msgs, 2)
	assert.Equal(t, "msg1", msgs[0].ID)
	assert.Equal(t, "alice@example.com", msgs[0].From)
	assert.True(t, msgs[0].Unread)
}

func TestReadMessage(t *testing.T) {
	mock := &MockGmailAPI{
		Detail: &MessageDetail{
			MessageSummary: MessageSummary{
				ID:      "msg1",
				From:    "alice@example.com",
				Subject: "Hello",
			},
			Body: WrapEmail("alice@example.com", "Hello", "Hi there"),
		},
	}

	detail, err := mock.ReadMessage("msg1")
	require.NoError(t, err)
	assert.Equal(t, "msg1", detail.ID)
	assert.Contains(t, detail.Body, "UNTRUSTED")
	assert.Contains(t, detail.Body, "alice@example.com")
}

func TestSendMessage(t *testing.T) {
	mock := &MockGmailAPI{}

	err := mock.SendMessage("bob@example.com", "Greetings", "Hello Bob!")
	require.NoError(t, err)
	assert.Equal(t, "bob@example.com", mock.SentTo)
	assert.Equal(t, "Greetings", mock.SentSubject)
	assert.Equal(t, "Hello Bob!", mock.SentBody)
}

func TestReplyMessage(t *testing.T) {
	mock := &MockGmailAPI{}

	err := mock.ReplyMessage("msg1", "Thanks for reaching out!")
	require.NoError(t, err)
	assert.Equal(t, "msg1", mock.ReplyID)
	assert.Equal(t, "Thanks for reaching out!", mock.ReplyBody)
}

func TestListMessages_PropagatesError(t *testing.T) {
	mock := &MockGmailAPI{ListErr: assert.AnError}
	_, err := mock.ListMessages("", 10)
	assert.ErrorIs(t, err, assert.AnError)
}

func TestReadMessage_PropagatesError(t *testing.T) {
	mock := &MockGmailAPI{ReadErr: assert.AnError}
	_, err := mock.ReadMessage("msg1")
	assert.ErrorIs(t, err, assert.AnError)
}
