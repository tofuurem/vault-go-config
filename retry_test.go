package vltconfig

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestRetryUsesThreeAttemptsAndExponentialBackoff(t *testing.T) {
	t.Parallel()

	attempts := 0
	waits := make([]time.Duration, 0, 2)
	notifiedAttempts := make([]int, 0, 2)
	retry := defaultVaultRetry()
	retry.sleep = func(_ context.Context, duration time.Duration) error {
		waits = append(waits, duration)
		return nil
	}
	retry.notify = func(_ context.Context, failedAttempt int, _ time.Duration) {
		notifiedAttempts = append(notifiedAttempts, failedAttempt)
	}
	err := retry.call(context.Background(), func() error {
		attempts++
		return &net.DNSError{Err: "temporary", Name: "vault.invalid"}
	})
	if err == nil {
		t.Fatal("expected exhausted retry")
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	if !reflect.DeepEqual(waits, []time.Duration{2 * time.Second, 4 * time.Second}) {
		t.Fatalf("waits = %#v", waits)
	}
	if !reflect.DeepEqual(notifiedAttempts, []int{1, 2}) {
		t.Fatalf("notified attempts = %#v", notifiedAttempts)
	}
}

func TestRetryClassifiesTransientFailures(t *testing.T) {
	t.Parallel()

	cases := map[string]error{
		"timeout":      context.DeadlineExceeded,
		"network":      &net.DNSError{Err: "temporary", Name: "vault.invalid"},
		"rate limit":   statusError(429),
		"server error": statusError(503),
	}
	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			attempts := 0
			retry := noWaitVaultRetry()
			err := retry.call(context.Background(), func() error {
				attempts++
				if attempts < 3 {
					return failure
				}
				return nil
			})
			if err != nil || attempts != 3 {
				t.Fatalf("attempts = %d, error = %v", attempts, err)
			}
		})
	}
}

func TestRetryDoesNotRepeatPermanentFailures(t *testing.T) {
	t.Parallel()

	cases := map[string]error{
		"authentication": errVaultAuthenticationRejected,
		"authorization":  statusError(403),
		"path":           statusError(404),
		"bad request":    statusError(400),
		"malformed":      errVaultMalformedResponse,
	}
	for name, failure := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			attempts := 0
			err := noWaitVaultRetry().call(context.Background(), func() error {
				attempts++
				return failure
			})
			if err == nil || attempts != 1 {
				t.Fatalf("attempts = %d, error = %v", attempts, err)
			}
		})
	}
}

func TestRetryStopsWhenContextIsCancelledDuringWait(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	retry := defaultVaultRetry()
	retry.sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}
	err := retry.call(ctx, func() error {
		attempts++
		return statusError(503)
	})
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("attempts = %d, error = %v", attempts, err)
	}
}

func TestRetryDoesNotRetryCallerDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	err := noWaitVaultRetry().call(ctx, func() error {
		attempts++
		return context.DeadlineExceeded
	})
	if !errors.Is(err, context.Canceled) || attempts != 0 {
		t.Fatalf("attempts = %d, error = %v", attempts, err)
	}
}

func TestVaultSourceRecreatesClientForEveryTransientAttempt(t *testing.T) {
	t.Parallel()

	created := 0
	source := vaultSource{
		lookup: mapLookup(validTokenVaultEnvironment()),
		policy: VaultFailureStrict,
		factory: func(vaultConfig) (vaultClient, error) {
			created++
			return &fakeVaultClient{readError: statusError(503)}, nil
		},
		retry: noWaitVaultRetry(),
	}
	_, err := source.load(context.Background(), nil)
	if !errors.Is(err, ErrVaultUnavailable) || created != 3 {
		t.Fatalf("clients = %d, error = %v", created, err)
	}
}

func TestSleepWithContextReturnsImmediatelyAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	err := sleepWithContext(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("cancelled sleep took %s", elapsed)
	}
}
