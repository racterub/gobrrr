// Package mcpserver wires the MCP stdio server for the Telegram channel.
package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
)

// AssertSendable refuses to send files that live inside STATE_DIR except
// for STATE_DIR/inbox/. Best-effort: unresolved paths are permitted (the
// actual open/send will surface real errors).
func AssertSendable(path, stateDir string) error {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil
	}
	stateReal, err := filepath.EvalSymlinks(stateDir)
	if err != nil {
		return nil
	}
	inbox := filepath.Join(stateReal, "inbox")
	sep := string(filepath.Separator)
	if strings.HasPrefix(real, stateReal+sep) && !strings.HasPrefix(real, inbox+sep) {
		return fmt.Errorf("refusing to send channel state: %s", path)
	}
	return nil
}
