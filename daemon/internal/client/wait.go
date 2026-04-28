package client

import (
	"fmt"
	"time"
)

// WaitForTask polls GetTask every 500ms until the task reaches a terminal
// state (completed, failed, or cancelled). It returns the result string on
// success, an error on failure/cancellation, or a descriptive error if the
// daemon connection is lost.
func (c *Client) WaitForTask(taskID string) (string, error) {
	for {
		task, err := c.GetTask(taskID)
		if err != nil {
			return "", fmt.Errorf("daemon connection lost, result will be in ~/.gobrrr/logs/%s.log", taskID)
		}
		switch task.Status {
		case "completed":
			if task.Result != nil {
				return *task.Result, nil
			}
			return "", nil
		case "failed":
			errMsg := "unknown error"
			if task.Error != nil {
				errMsg = *task.Error
			}
			return "", fmt.Errorf("task failed: %s", errMsg)
		case "cancelled":
			return "", fmt.Errorf("task was cancelled")
		}
		time.Sleep(500 * time.Millisecond)
	}
}
