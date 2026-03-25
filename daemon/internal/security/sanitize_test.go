package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectsOAuthToken(t *testing.T) {
	result := Check("Here is a token: ya29.a0ARrdaM8something", nil)
	assert.True(t, result.HasLeak)
	assert.NotEmpty(t, result.Matches)
}

func TestDetectsBearerToken(t *testing.T) {
	result := Check("Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.long.token", nil)
	assert.True(t, result.HasLeak)
}

func TestDetectsKnownSecret(t *testing.T) {
	secret := "my-super-secret-master-key-hex-value"
	result := Check("The key is "+secret, []string{secret})
	assert.True(t, result.HasLeak)
}

func TestDetectsJSONSecretPattern(t *testing.T) {
	result := Check(`{"token":"abc123def456"}`, nil)
	assert.True(t, result.HasLeak)
}

func TestCleanOutputPasses(t *testing.T) {
	result := Check("Here is your calendar summary for today. You have 3 meetings.", nil)
	assert.False(t, result.HasLeak)
}

func TestNormalCodePasses(t *testing.T) {
	result := Check("The function returns a map[string]interface{} with the results", nil)
	assert.False(t, result.HasLeak)
}

func TestDetectsAPIKeyHexPattern(t *testing.T) {
	// 32+ hex chars
	result := Check("api_key=0123456789abcdef0123456789abcdef", nil)
	assert.True(t, result.HasLeak)
}

func TestDetectsBase64Secret(t *testing.T) {
	// 40+ chars matching base64 alphabet
	result := Check("token=SGVsbG9Xb3JsZEhlbGxvV29ybGRIZWxsb1dvcmxk", nil)
	assert.True(t, result.HasLeak)
}

func TestDetectsJSONPasswordPattern(t *testing.T) {
	result := Check(`{"password":"supersecretpassword123"}`, nil)
	assert.True(t, result.HasLeak)
}

func TestDetectsJSONApiKeyPattern(t *testing.T) {
	result := Check(`{"api_key":"myapikey12345"}`, nil)
	assert.True(t, result.HasLeak)
}

func TestDetectsJSONSecretFieldPattern(t *testing.T) {
	result := Check(`{"secret":"someSecretValue"}`, nil)
	assert.True(t, result.HasLeak)
}

func TestMultipleMatchesReported(t *testing.T) {
	result := Check(`{"token":"abc","secret":"xyz"} ya29.something`, nil)
	assert.True(t, result.HasLeak)
	assert.GreaterOrEqual(t, len(result.Matches), 2)
}

func TestKnownSecretNotInOutput(t *testing.T) {
	secret := "my-super-secret-master-key-hex-value"
	result := Check("nothing suspicious here", []string{secret})
	assert.False(t, result.HasLeak)
}

func TestEmptyOutput(t *testing.T) {
	result := Check("", nil)
	assert.False(t, result.HasLeak)
	assert.Empty(t, result.Matches)
}
