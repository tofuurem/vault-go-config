package vltconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type compositionDatabase struct {
	Host     string `json:"host" vlt:"required"`
	Port     int    `json:"port" vlt:"required"`
	Username string `json:"username"`
	SSL      bool   `json:"ssl"`
}

type compositionConfiguration struct {
	Database compositionDatabase `json:"db"`
	Mode     string              `json:"mode"`
	Labels   map[string]string   `json:"labels"`
	Features []string            `json:"features"`
}

func TestAllSourcesComposeByLeafInDocumentedPriority(t *testing.T) {
	jsonPath := filepath.Join(t.TempDir(), "config.json")
	jsonPayload := []byte(`{
		"db": {"host": "json-host", "port": 2000, "username": "json-user"},
		"mode": "json-mode",
		"labels": {"json": "yes", "shared": "json"},
		"features": ["json"]
	}`)
	if err := os.WriteFile(jsonPath, jsonPayload, 0o600); err != nil {
		t.Fatalf("write JSON fixture: %v", err)
	}

	environment := validTokenVaultEnvironment()
	environment["DB__HOST"] = "env-host"
	environment["FEATURES"] = `["env"]`
	client := &fakeVaultClient{data: map[string]any{
		"db":       map[string]any{"host": "vault-host", "port": 3000},
		"mode":     "vault-mode",
		"labels":   map[string]any{"vault": "yes", "shared": "vault"},
		"features": []any{"vault"},
	}}
	loader := newLoaderWithSources(
		jsonSource{lookup: mapLookup(environment), explicitFile: jsonPath},
		vaultSource{
			lookup:  mapLookup(environment),
			policy:  VaultFailureStrict,
			factory: func(vaultConfig) (vaultClient, error) { return client, nil },
			retry:   noWaitVaultRetry(),
		},
		environmentSource{lookup: mapLookup(environment)},
	)
	configuration := compositionConfiguration{
		Database: compositionDatabase{
			Host:     "default-host",
			Port:     1000,
			Username: "default-user",
			SSL:      true,
		},
		Mode: "default-mode",
		Labels: map[string]string{
			"default": "yes",
			"shared":  "default",
		},
		Features: []string{"default"},
	}

	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load composed configuration: %v", err)
	}
	want := compositionConfiguration{
		Database: compositionDatabase{
			Host:     "env-host",
			Port:     3000,
			Username: "json-user",
			SSL:      true,
		},
		Mode: "vault-mode",
		Labels: map[string]string{
			"default": "yes",
			"json":    "yes",
			"vault":   "yes",
			"shared":  "vault",
		},
		Features: []string{"env"},
	}
	if !reflect.DeepEqual(configuration, want) {
		t.Fatalf("configuration = %#v, want %#v", configuration, want)
	}
	if wantCalls := []string{"set-token", "token", "read"}; !reflect.DeepEqual(client.calls, wantCalls) {
		t.Fatalf("Vault calls = %#v, want %#v", client.calls, wantCalls)
	}
}

func TestAbsentVaultSkipsAndEnvironmentStillLoads(t *testing.T) {
	t.Parallel()

	created := false
	environment := map[string]string{"MODE": "environment-mode"}
	loader := newLoaderWithSources(
		vaultSource{
			lookup: mapLookup(environment),
			policy: VaultFailureStrict,
			factory: func(vaultConfig) (vaultClient, error) {
				created = true
				return nil, errors.New("must not run")
			},
		},
		environmentSource{lookup: mapLookup(environment)},
	)
	configuration := struct {
		Mode string `json:"mode"`
	}{Mode: "default-mode"}
	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load without Vault: %v", err)
	}
	if created || configuration.Mode != "environment-mode" {
		t.Fatalf("created = %t, configuration = %#v", created, configuration)
	}
}

func TestBrokenVaultStopsCompositionInStrictMode(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		VaultAddressEnvironment: "https://vault.example.test",
		"MODE":                  "environment-mode",
	}
	loader := newLoaderWithSources(
		vaultSource{
			lookup:  mapLookup(environment),
			policy:  VaultFailureStrict,
			factory: func(vaultConfig) (vaultClient, error) { return nil, errors.New("must not run") },
		},
		environmentSource{lookup: mapLookup(environment)},
	)
	configuration := struct {
		Mode string `json:"mode"`
	}{Mode: "default-mode"}
	err := loader.Load(context.Background(), &configuration)
	if !errors.Is(err, ErrVaultConfiguration) {
		t.Fatalf("error = %v, want Vault configuration error", err)
	}
	if configuration.Mode != "default-mode" {
		t.Fatalf("strict failure mutated destination: %#v", configuration)
	}
}

func TestExplicitVaultFallbackContinuesComposition(t *testing.T) {
	t.Parallel()

	environment := map[string]string{
		VaultAddressEnvironment: "not-a-url",
		"MODE":                  "environment-mode",
	}
	loader := newLoaderWithSources(
		vaultSource{
			lookup:  mapLookup(environment),
			policy:  VaultFailureFallback,
			factory: func(vaultConfig) (vaultClient, error) { return nil, errors.New("must not run") },
		},
		environmentSource{lookup: mapLookup(environment)},
	)
	configuration := struct {
		Mode string `json:"mode"`
	}{Mode: "default-mode"}
	if err := loader.Load(context.Background(), &configuration); err != nil {
		t.Fatalf("load with fallback: %v", err)
	}
	if configuration.Mode != "environment-mode" {
		t.Fatalf("configuration = %#v", configuration)
	}
}
