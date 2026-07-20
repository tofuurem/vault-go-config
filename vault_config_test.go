package vltconfig

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVaultConfigSkipsOnlyWhenAccessEnvironmentIsAbsent(t *testing.T) {
	t.Parallel()

	configuration, configured, err := vaultConfigFromEnvironment(mapLookup(map[string]string{
		VaultCacheTTLEnvironment: "10",
	}))
	if err != nil {
		t.Fatalf("parse absent Vault: %v", err)
	}
	if configured {
		t.Fatalf("cache controls alone activated Vault: %#v", configuration)
	}
}

func TestVaultConfigParsesTokenAndDefaults(t *testing.T) {
	t.Parallel()

	configuration, configured, err := vaultConfigFromEnvironment(mapLookup(validTokenVaultEnvironment()))
	if err != nil {
		t.Fatalf("parse Vault: %v", err)
	}
	if !configured {
		t.Fatal("Vault was not configured")
	}
	if configuration.address != "https://vault.example.test" {
		t.Fatalf("address = %q", configuration.address)
	}
	if configuration.appName != "apps/service/config" {
		t.Fatalf("app path = %q", configuration.appName)
	}
	if configuration.mountPoint != "secret" {
		t.Fatalf("mount = %q", configuration.mountPoint)
	}
	if !configuration.cache.enabled || configuration.cache.ttl != 5*time.Minute || configuration.cache.maxEntries != 128 {
		t.Fatalf("cache defaults = %#v", configuration.cache)
	}
}

func TestVaultConfigAcceptsEveryCompleteAuthenticationMethod(t *testing.T) {
	t.Parallel()

	methods := map[string]map[string]string{
		"token":    {VaultTokenEnvironment: "fake-token"},
		"AppRole":  {VaultRoleIDEnvironment: "fake-role", VaultSecretIDEnvironment: "fake-secret"},
		"userpass": {VaultUsernameEnvironment: "fake-user", VaultPasswordEnvironment: "fake-password"},
	}
	for name, method := range methods {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			environment := baseVaultEnvironment()
			for key, value := range method {
				environment[key] = value
			}
			_, configured, err := vaultConfigFromEnvironment(mapLookup(environment))
			if err != nil || !configured {
				t.Fatalf("configured = %t, error = %v", configured, err)
			}
		})
	}
}

func TestVaultConfigAllowsIncompleteSecondaryMethod(t *testing.T) {
	t.Parallel()

	environment := validTokenVaultEnvironment()
	environment[VaultRoleIDEnvironment] = "incomplete-role"
	_, configured, err := vaultConfigFromEnvironment(mapLookup(environment))
	if err != nil || !configured {
		t.Fatalf("configured = %t, error = %v", configured, err)
	}
}

func TestVaultConfigPreservesCredentialWhitespace(t *testing.T) {
	t.Parallel()

	environment := baseVaultEnvironment()
	environment[VaultUsernameEnvironment] = " fake-user "
	environment[VaultPasswordEnvironment] = " fake-password "
	configuration, _, err := vaultConfigFromEnvironment(mapLookup(environment))
	if err != nil {
		t.Fatalf("parse Vault: %v", err)
	}
	if configuration.username != " fake-user " || configuration.password != " fake-password " {
		t.Fatal("credentials were unexpectedly normalized")
	}
}

func TestVaultConfigRejectsPartialOrInvalidConfigurationSafely(t *testing.T) {
	t.Parallel()

	const canary = "credential-secret-canary"
	cases := map[string]map[string]string{
		"missing address": {
			VaultTokenEnvironment:   canary,
			VaultAppNameEnvironment: "apps/service",
		},
		"missing path": {
			VaultAddressEnvironment: "https://vault.example.test",
			VaultTokenEnvironment:   canary,
		},
		"incomplete auth": {
			VaultAddressEnvironment:  "https://vault.example.test",
			VaultAppNameEnvironment:  "apps/service",
			VaultUsernameEnvironment: canary,
		},
	}
	for name, environment := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, configured, err := vaultConfigFromEnvironment(mapLookup(environment))
			if !configured || !errors.Is(err, ErrVaultConfiguration) || !errors.Is(err, ErrVault) {
				t.Fatalf("configured = %t, error = %v", configured, err)
			}
			if strings.Contains(err.Error(), canary) {
				t.Fatalf("error leaked credential: %v", err)
			}
		})
	}
}

func TestVaultConfigValidatesAddress(t *testing.T) {
	t.Parallel()

	addresses := []string{
		"vault.example.test",
		"ftp://vault.example.test",
		"https://user:password@vault.example.test",
		"https://vault.example.test?token=secret",
		"https://vault.example.test#fragment",
	}
	for _, address := range addresses {
		t.Run(address, func(t *testing.T) {
			t.Parallel()
			environment := validTokenVaultEnvironment()
			environment[VaultAddressEnvironment] = address
			_, _, err := vaultConfigFromEnvironment(mapLookup(environment))
			if !errors.Is(err, ErrVaultConfiguration) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestVaultConfigParsesCacheControls(t *testing.T) {
	t.Parallel()

	environment := validTokenVaultEnvironment()
	environment[VaultMountPointEnvironment] = "/application/"
	environment[VaultCacheDisabledEnvironment] = "yes"
	environment[VaultCacheTTLEnvironment] = "1.5"
	environment[VaultCacheMaxEntriesEnvironment] = "7"

	configuration, _, err := vaultConfigFromEnvironment(mapLookup(environment))
	if err != nil {
		t.Fatalf("parse Vault cache: %v", err)
	}
	if configuration.mountPoint != "application" {
		t.Fatalf("mount = %q", configuration.mountPoint)
	}
	if configuration.cache.enabled || configuration.cache.ttl != 1500*time.Millisecond || configuration.cache.maxEntries != 7 {
		t.Fatalf("cache = %#v", configuration.cache)
	}
}

func TestVaultConfigRejectsInvalidCacheControls(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]string{
		"disabled":     {VaultCacheDisabledEnvironment: "sometimes"},
		"ttl text":     {VaultCacheTTLEnvironment: "later"},
		"ttl negative": {VaultCacheTTLEnvironment: "-1"},
		"ttl NaN":      {VaultCacheTTLEnvironment: "NaN"},
		"ttl infinity": {VaultCacheTTLEnvironment: "+Inf"},
		"maximum text": {VaultCacheMaxEntriesEnvironment: "many"},
		"maximum zero": {VaultCacheMaxEntriesEnvironment: "0"},
	}
	for name, addition := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			environment := validTokenVaultEnvironment()
			for key, value := range addition {
				environment[key] = value
			}
			_, _, err := vaultConfigFromEnvironment(mapLookup(environment))
			if !errors.Is(err, ErrVaultConfiguration) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestVaultFailurePolicyOption(t *testing.T) {
	t.Parallel()

	if _, err := New(WithVaultFailurePolicy(VaultFailureFallback)); err != nil {
		t.Fatalf("fallback policy: %v", err)
	}
	if _, err := New(WithVaultFailurePolicy("unknown")); !errors.Is(err, ErrVaultConfiguration) {
		t.Fatalf("invalid policy error = %v", err)
	}
}

func validTokenVaultEnvironment() map[string]string {
	environment := baseVaultEnvironment()
	environment[VaultTokenEnvironment] = "fake-token"
	return environment
}

func baseVaultEnvironment() map[string]string {
	return map[string]string{
		VaultAddressEnvironment: "https://vault.example.test",
		VaultAppNameEnvironment: "/apps/service/config/",
	}
}
