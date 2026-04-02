package retry

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

var testLogger = slog.Default()

func TestDo_SucceedsFirstAttempt(t *testing.T) {
	calls := 0
	err := Do(context.Background(), 3, "test", testLogger, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestDo_SucceedsAfterRetry(t *testing.T) {
	calls := 0
	err := Do(context.Background(), 3, "test", testLogger, func() error {
		calls++
		if calls < 3 {
			return errors.New("transient error")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestDo_ExhaustsRetries(t *testing.T) {
	calls := 0
	err := Do(context.Background(), 2, "test", testLogger, func() error {
		calls++
		return errors.New("persistent error")
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// 1 initial + 2 retries = 3 attempts
	if calls != 3 {
		t.Errorf("calls = %d, want 3", calls)
	}
}

func TestDo_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := Do(ctx, 5, "test", testLogger, func() error {
		calls++
		cancel() // cancel after first attempt
		return errors.New("error")
	})
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestDo_ZeroRetries(t *testing.T) {
	calls := 0
	err := Do(context.Background(), 0, "test", testLogger, func() error {
		calls++
		return errors.New("error")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (no retries)", calls)
	}
}
