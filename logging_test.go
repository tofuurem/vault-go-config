package vltconfig

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestWithLoggerRejectsNil(t *testing.T) {
	t.Parallel()

	if _, err := New(WithLogger(nil)); !errors.Is(err, ErrConfig) || errors.Is(err, ErrVault) {
		t.Fatalf("error = %v, want non-Vault configuration error", err)
	}
}

func TestWithLoggerReportsAbsentVaultThroughPublicLoader(t *testing.T) {
	for _, name := range vaultAccessEnvironmentNames {
		unsetLoggingTestEnvironment(t, name)
	}

	var output bytes.Buffer
	logger := testLogger(&output)
	jsonPath := writeJSONFixture(t, `{}`)
	loader, err := New(WithLogger(logger), WithJSONFile(jsonPath))
	if err != nil {
		t.Fatalf("create loader: %v", err)
	}
	var configuration struct{}
	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !strings.Contains(output.String(), "configuration source skipped") {
		t.Fatalf("missing absent-source event: %s", output.String())
	}
}

func TestVaultOperationalLogsAreValueFree(t *testing.T) {
	t.Parallel()

	const (
		credentialCanary = "credential-canary-never-log"
		payloadCanary    = "payload-canary-never-log"
		upstreamCanary   = "upstream-canary-never-log"
	)
	var output bytes.Buffer
	logger := testLogger(&output)

	environment := validTokenVaultEnvironment()
	environment[VaultTokenEnvironment] = credentialCanary
	client := &fakeVaultClient{data: map[string]any{"value": payloadCanary}}
	source := vaultSourceFromEnvironment(environment, client, VaultFailureStrict)
	source.cache = testVaultCache(nil)
	source.logger = logger
	if _, err := source.load(context.Background(), nil); err != nil {
		t.Fatalf("prime cache: %v", err)
	}
	if _, err := source.load(context.Background(), nil); err != nil {
		t.Fatalf("load cache: %v", err)
	}

	failingClient := &fakeVaultClient{readError: &responseStatusError{
		statusCode: 503,
		cause:      errors.New(upstreamCanary),
	}}
	failing := vaultSourceFromEnvironment(environment, failingClient, VaultFailureFallback)
	failing.logger = logger
	if _, err := failing.load(context.Background(), nil); err != nil {
		t.Fatalf("fallback: %v", err)
	}

	logs := output.String()
	for _, expected := range []string{
		"Vault cache hit",
		"Vault retry scheduled",
		"Vault failure ignored by fallback policy",
		`"category":"unavailable"`,
	} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("logs do not contain %q: %s", expected, logs)
		}
	}
	for _, canary := range []string{credentialCanary, payloadCanary, upstreamCanary} {
		if strings.Contains(logs, canary) {
			t.Fatalf("logs leaked canary %q: %s", canary, logs)
		}
	}
}

func testLogger(output *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func unsetLoggingTestEnvironment(t *testing.T, name string) {
	t.Helper()
	previous, present := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(name, previous)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}
