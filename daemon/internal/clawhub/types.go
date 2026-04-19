// Package clawhub is a Go client for the ClawHub skill registry.
//
// API reference: docs/superpowers/notes/2026-04-19-clawhub-api.md
// Default base URL: https://clawhub.ai
package clawhub

// DefaultBaseURL is the canonical ClawHub registry.
// clawhub.com 307-redirects here; the ClawHub CLI hardcodes this value.
const DefaultBaseURL = "https://clawhub.ai"

// SkillSummary is one entry in the search results array.
// Version is always null in search responses; callers that need a concrete
// version must call Fetch (which reads the "latest" tag from metadata).
type SkillSummary struct {
	Score       float64 `json:"score"`
	Slug        string  `json:"slug"`
	DisplayName string  `json:"displayName"`
	Summary     *string `json:"summary"`
	Version     *string `json:"version"`
	UpdatedAt   int64   `json:"updatedAt"`
}

// searchResponse wraps the envelope returned by /api/v1/search.
type searchResponse struct {
	Results []SkillSummary `json:"results"`
}

// skillMetadata is the /api/v1/skills/<slug> response. We only unmarshal the
// fields needed to resolve "latest" and present the skill to users.
type skillMetadata struct {
	Skill struct {
		Slug        string            `json:"slug"`
		DisplayName string            `json:"displayName"`
		Summary     *string           `json:"summary"`
		Tags        map[string]string `json:"tags"`
	} `json:"skill"`
	LatestVersion *struct {
		Version string `json:"version"`
	} `json:"latestVersion"`
}

// versionDetail is the /api/v1/skills/<slug>/versions/<version> response.
// The integrity hash we verify against lives at version.security.sha256hash.
type versionDetail struct {
	Version struct {
		Version  string `json:"version"`
		Security *struct {
			Status     string  `json:"status"`
			SHA256Hash *string `json:"sha256hash"`
		} `json:"security"`
	} `json:"version"`
}

// SkillPackage is the resolved download: raw ZIP bytes plus integrity proof.
// Downstream Task 13 extracts BundleBytes with archive/zip.
type SkillPackage struct {
	Slug        string
	Version     string
	SHA256      string
	BundleBytes []byte
}
