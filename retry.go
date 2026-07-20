package vltconfig

import (
	"context"
	"errors"
	"net"
	"time"
)

const defaultVaultAttempts = 3

type vaultRetry struct {
	maxAttempts int
	backoff     func(int) time.Duration
	sleep       func(context.Context, time.Duration) error
	notify      func(context.Context, int, time.Duration)
}

func defaultVaultRetry() vaultRetry {
	return vaultRetry{
		maxAttempts: defaultVaultAttempts,
		backoff: func(failedAttempt int) time.Duration {
			return time.Duration(1<<failedAttempt) * time.Second
		},
		sleep: sleepWithContext,
	}
}

func (retry vaultRetry) call(ctx context.Context, operation func() error) error {
	for attempt := 1; attempt <= retry.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := operation()
		if err == nil {
			return nil
		}
		if !isTransientVaultError(ctx, err) || attempt == retry.maxAttempts {
			return err
		}
		delay := retry.backoff(attempt)
		if retry.notify != nil {
			retry.notify(ctx, attempt, delay)
		}
		if err := retry.sleep(ctx, delay); err != nil {
			return err
		}
	}
	return nil
}

func isTransientVaultError(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var response *responseStatusError
	if errors.As(err, &response) {
		return response.statusCode == 429 || response.statusCode >= 500
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func noWaitVaultRetry() vaultRetry {
	return vaultRetry{
		maxAttempts: defaultVaultAttempts,
		backoff:     func(int) time.Duration { return 0 },
		sleep:       func(context.Context, time.Duration) error { return nil },
	}
}
