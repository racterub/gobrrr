package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/racterub/gobrrr/internal/skills"
)

// ListSkills returns all installed skills known to the daemon.
func (c *Client) ListSkills() ([]skills.Skill, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/skills")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list skills: %s: %s", resp.Status, string(body))
	}
	var out []skills.Skill
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}
