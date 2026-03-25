package google

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/googleapi"
)

func TestWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	attempts := 0
	err := WithRetry(func() error {
		attempts++
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 1, attempts)
}

func TestWithRetry_NonRetryableError(t *testing.T) {
	attempts := 0
	sentinel := errors.New("some error")
	err := WithRetry(func() error {
		attempts++
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 1, attempts, "non-retryable errors should not be retried")
}

func TestWithRetry_RetryOn429(t *testing.T) {
	attempts := 0
	err := WithRetry(func() error {
		attempts++
		if attempts < 3 {
			return &googleapi.Error{Code: 429}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestWithRetry_RetryOn503(t *testing.T) {
	attempts := 0
	err := WithRetry(func() error {
		attempts++
		if attempts < 2 {
			return &googleapi.Error{Code: 503}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestWithRetry_GivesUpAfterMaxRetries(t *testing.T) {
	attempts := 0
	err := WithRetry(func() error {
		attempts++
		return &googleapi.Error{Code: 429}
	})
	// Should return the last error after exhausting retries
	var apiErr *googleapi.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 429, apiErr.Code)
	// maxRetries=5, so i goes from 0..5, fn called on each iteration: 6 total
	assert.Equal(t, 6, attempts)
}

func TestWithRetry_Non5xxApiError_NotRetried(t *testing.T) {
	attempts := 0
	err := WithRetry(func() error {
		attempts++
		return &googleapi.Error{Code: 404}
	})
	var apiErr *googleapi.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 1, attempts, "404 errors should not be retried")
}
