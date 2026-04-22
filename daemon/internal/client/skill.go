package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/racterub/gobrrr/internal/clawhub"
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

// SearchSkills queries the daemon's ClawHub proxy.
func (c *Client) SearchSkills(q string) ([]clawhub.SkillSummary, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/skills/search?q=" + url.QueryEscape(q))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("search skills: %s: %s", resp.Status, string(body))
	}
	var out []clawhub.SkillSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// InstallResult is what the daemon returns after staging an install.
type InstallResult struct {
	RequestID string                 `json:"request_id"`
	Request   clawhub.InstallRequest `json:"request"`
}

// InstallSkill asks the daemon to fetch & stage <slug>[@version]; version="" for latest.
func (c *Client) InstallSkill(slug, version string) (*InstallResult, error) {
	body, _ := json.Marshal(map[string]string{"slug": slug, "version": version})
	resp, err := c.httpClient.Post(c.baseURL+"/skills/install", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("install skill: %s: %s", resp.Status, string(b))
	}
	var out InstallResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UninstallSkill removes an installed skill from ~/.gobrrr/skills/<slug>.
func (c *Client) UninstallSkill(slug string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/skills/"+url.PathEscape(slug), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("uninstall skill: %s: %s", resp.Status, string(b))
	}
	return nil
}
