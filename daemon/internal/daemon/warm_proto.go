package daemon

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// streamMsg is used for initial type dispatch when reading NDJSON lines.
type streamMsg struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`
}

// userMsg is the envelope sent to a warm worker's stdin.
type userMsg struct {
	Type            string     `json:"type"`
	Message         msgContent `json:"message"`
	ParentToolUseID *string    `json:"parent_tool_use_id"`
}

// msgContent holds the role and content of a message.
type msgContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// resultMsg is emitted by Claude after completing a request.
type resultMsg struct {
	Type       string   `json:"type"`
	Subtype    string   `json:"subtype"`
	Result     string   `json:"result"`
	IsError    bool     `json:"is_error"`
	Errors     []string `json:"errors,omitempty"`
	DurationMs int      `json:"duration_ms"`
}

// writeUserMessage writes one NDJSON user message line to w.
func writeUserMessage(w io.Writer, content string) error {
	msg := userMsg{
		Type: "user",
		Message: msgContent{
			Role:    "user",
			Content: content,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshalling user message: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// readUntilInit reads NDJSON lines from scanner until a system/init message.
// Non-init lines are silently skipped.
func readUntilInit(scanner *bufio.Scanner) error {
	for scanner.Scan() {
		var msg streamMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Type == "system" && msg.Subtype == "init" {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading stdout: %w", err)
	}
	return fmt.Errorf("unexpected EOF: no init message received")
}

// readUntilResult reads NDJSON lines from scanner until a result message.
// All non-result lines (assistant, system, stream_event) are skipped.
func readUntilResult(scanner *bufio.Scanner) (*resultMsg, error) {
	for scanner.Scan() {
		var msg streamMsg
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			continue
		}
		if msg.Type == "result" {
			var result resultMsg
			if err := json.Unmarshal(scanner.Bytes(), &result); err != nil {
				return nil, fmt.Errorf("parsing result message: %w", err)
			}
			return &result, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading stdout: %w", err)
	}
	return nil, fmt.Errorf("unexpected EOF: no result message received")
}
