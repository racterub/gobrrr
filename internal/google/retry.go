package google

import (
	"math/rand"
	"time"

	"google.golang.org/api/googleapi"
)

// WithRetry executes fn and retries on transient Google API errors (429 and
// 5xx status codes) using exponential backoff with jitter. It makes up to 5
// retry attempts before returning the last error.
func WithRetry(fn func() error) error {
	maxRetries := 5
	for i := 0; i <= maxRetries; i++ {
		err := fn()
		if err == nil {
			return nil
		}
		if apiErr, ok := err.(*googleapi.Error); ok {
			if apiErr.Code == 429 || apiErr.Code >= 500 {
				if i < maxRetries {
					backoff := time.Duration(1<<uint(i)) * time.Second
					if backoff > 30*time.Second {
						backoff = 30 * time.Second
					}
					jitter := time.Duration(rand.Int63n(int64(backoff / 2)))
					time.Sleep(backoff + jitter)
					continue
				}
			}
		}
		return err
	}
	return nil
}
