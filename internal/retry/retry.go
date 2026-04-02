package retry

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Do retries fn with exponential backoff.
// Returns nil on first success, or the last error after maxRetries attempts.
func Do(ctx context.Context, maxRetries int, operation string, logger *slog.Logger, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		lastErr = fn()
		if lastErr == nil {
			if attempt > 0 {
				logger.Info("retry succeeded", "operation", operation, "attempt", attempt+1)
			}
			return nil
		}

		if attempt < maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * time.Second // 1s, 2s, 4s, ...
			logger.Warn("operation failed, retrying",
				"operation", operation,
				"attempt", attempt+1,
				"max_retries", maxRetries,
				"backoff_s", backoff.Seconds(),
				"error", lastErr,
			)

			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return fmt.Errorf("%s: context cancelled during retry: %w", operation, ctx.Err())
			}
		}
	}

	return fmt.Errorf("%s: all %d retries exhausted: %w", operation, maxRetries, lastErr)
}
