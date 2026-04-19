package clawhub

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxBundleBytes caps the ZIP body we will read from the registry. At time of
// writing the largest skill bundle is a few hundred KB; 50 MiB is a defensive
// ceiling that prevents a hostile or misconfigured registry from exhausting
// memory on the daemon host.
const maxBundleBytes = 50 << 20 // 50 MiB

// Client talks to a ClawHub registry over HTTP. Zero auth required for reads.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient returns a Client targeting baseURL (usually DefaultBaseURL).
// Passing "" uses DefaultBaseURL.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Search calls GET /api/v1/search?q=<query>&limit=<n> and returns the envelope's
// results array. limit<=0 omits the limit parameter (server default applies).
func (c *Client) Search(query string, limit int) ([]SkillSummary, error) {
	u, err := url.Parse(c.baseURL + "/api/v1/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("clawhub search %s: %s", resp.Status, string(body))
	}
	var env searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("clawhub search: decode: %w", err)
	}
	return env.Results, nil
}

// Fetch resolves the version (falling back to tags.latest when empty),
// fetches the version detail to learn the bundle's sha256, downloads the
// ZIP bundle, and verifies the integrity hash.
//
// On hash mismatch, returns an error — the bundle bytes are never exposed.
func (c *Client) Fetch(slug, version string) (*SkillPackage, error) {
	if slug == "" {
		return nil, fmt.Errorf("clawhub: empty slug")
	}

	if version == "" {
		meta, err := c.getMetadata(slug)
		if err != nil {
			return nil, err
		}
		switch {
		case meta.LatestVersion != nil && meta.LatestVersion.Version != "":
			version = meta.LatestVersion.Version
		case meta.Skill.Tags["latest"] != "":
			version = meta.Skill.Tags["latest"]
		default:
			return nil, fmt.Errorf("clawhub: %s has no latest version", slug)
		}
	}

	detail, err := c.getVersionDetail(slug, version)
	if err != nil {
		return nil, err
	}
	if detail.Version.Security == nil || detail.Version.Security.SHA256Hash == nil || *detail.Version.Security.SHA256Hash == "" {
		return nil, fmt.Errorf("clawhub: %s@%s has no sha256 hash in version detail", slug, version)
	}
	expectedHash := *detail.Version.Security.SHA256Hash

	zipBytes, err := c.download(slug, version)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(zipBytes)
	got := hex.EncodeToString(sum[:])
	if got != expectedHash {
		return nil, fmt.Errorf("clawhub: checksum mismatch for %s@%s: got %s, expected %s", slug, version, got, expectedHash)
	}

	return &SkillPackage{
		Slug:        slug,
		Version:     version,
		SHA256:      expectedHash,
		BundleBytes: zipBytes,
	}, nil
}

func (c *Client) getMetadata(slug string) (*skillMetadata, error) {
	u := c.baseURL + "/api/v1/skills/" + url.PathEscape(slug)
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("clawhub metadata %s: %s: %s", slug, resp.Status, string(body))
	}
	var out skillMetadata
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("clawhub metadata %s: decode: %w", slug, err)
	}
	return &out, nil
}

func (c *Client) getVersionDetail(slug, version string) (*versionDetail, error) {
	u := c.baseURL + "/api/v1/skills/" + url.PathEscape(slug) + "/versions/" + url.PathEscape(version)
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("clawhub version %s@%s: %s: %s", slug, version, resp.Status, string(body))
	}
	var out versionDetail
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("clawhub version %s@%s: decode: %w", slug, version, err)
	}
	return &out, nil
}

func (c *Client) download(slug, version string) ([]byte, error) {
	u, err := url.Parse(c.baseURL + "/api/v1/download")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("slug", slug)
	q.Set("version", version)
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("clawhub download %s@%s: %s: %s", slug, version, resp.Status, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBundleBytes {
		return nil, fmt.Errorf("clawhub download %s@%s: bundle exceeds %d bytes", slug, version, maxBundleBytes)
	}
	return body, nil
}
