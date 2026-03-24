package security

import (
	"fmt"
	"regexp"
	"strings"
)

// ScanResult holds the outcome of a credential scan.
type ScanResult struct {
	HasLeak bool
	Matches []string
}

var (
	// oauthTokenRe matches Google OAuth access tokens (ya29. prefix).
	oauthTokenRe = regexp.MustCompile(`ya29\.[A-Za-z0-9._\-]+`)

	// bearerTokenRe matches Bearer authorization headers with a long token.
	bearerTokenRe = regexp.MustCompile(`Bearer\s+[A-Za-z0-9\-._~+/]+=*`)

	// hexKeyRe matches runs of 32 or more hex characters (potential API keys or
	// raw key material like the gobrrr master key hex).
	hexKeyRe = regexp.MustCompile(`[0-9a-fA-F]{32,}`)

	// base64SecretRe matches long base64-looking strings (40+ chars) that are
	// likely encoded secrets or tokens.
	base64SecretRe = regexp.MustCompile(`[A-Za-z0-9+/=]{40,}`)

	// jsonSecretRe matches JSON fields commonly used to hold credentials.
	jsonSecretRe = regexp.MustCompile(`"(?:token|secret|password|api_key)"\s*:\s*"[^"]+"`)
)

// Check scans output for credential-like patterns.
// knownSecrets is a list of specific strings to check for (e.g., the master key hex).
func Check(output string, knownSecrets []string) *ScanResult {
	result := &ScanResult{}

	if output == "" {
		return result
	}

	// Check known secrets first (exact substring match).
	for _, secret := range knownSecrets {
		if secret != "" && strings.Contains(output, secret) {
			result.HasLeak = true
			result.Matches = append(result.Matches, "known secret found in output")
		}
	}

	// OAuth access tokens.
	if matches := oauthTokenRe.FindAllString(output, -1); len(matches) > 0 {
		result.HasLeak = true
		for _, m := range matches {
			result.Matches = append(result.Matches, fmt.Sprintf("OAuth token: %s", truncate(m, 20)))
		}
	}

	// Bearer tokens.
	if matches := bearerTokenRe.FindAllString(output, -1); len(matches) > 0 {
		result.HasLeak = true
		for _, m := range matches {
			result.Matches = append(result.Matches, fmt.Sprintf("Bearer token: %s", truncate(m, 20)))
		}
	}

	// JSON credential fields.
	if matches := jsonSecretRe.FindAllString(output, -1); len(matches) > 0 {
		result.HasLeak = true
		for _, m := range matches {
			result.Matches = append(result.Matches, fmt.Sprintf("JSON secret field: %s", truncate(m, 30)))
		}
	}

	// Hex key patterns (32+ hex chars).
	if matches := hexKeyRe.FindAllString(output, -1); len(matches) > 0 {
		result.HasLeak = true
		for _, m := range matches {
			result.Matches = append(result.Matches, fmt.Sprintf("hex key pattern (%d chars)", len(m)))
		}
	}

	// Base64 secret patterns (40+ chars).
	if matches := base64SecretRe.FindAllString(output, -1); len(matches) > 0 {
		result.HasLeak = true
		for _, m := range matches {
			result.Matches = append(result.Matches, fmt.Sprintf("base64 secret pattern (%d chars)", len(m)))
		}
	}

	return result
}

// truncate returns s trimmed to at most n characters, appending "..." if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
