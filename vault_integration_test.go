//go:build integration

package vltconfig

import (
	"context"
	"errors"
	"os"
	"testing"
)

type integrationConfiguration struct {
	ServiceName string `json:"service_name" vlt:"required"`
	Workers     int    `json:"workers" vlt:"required"`
}

func TestDisposableVaultAuthenticationAndKVv2(t *testing.T) {
	address := requiredIntegrationEnvironment(t, "VLTCONFIG_INTEGRATION_VAULT_ADDRESS")
	readToken := requiredIntegrationEnvironment(t, "VLTCONFIG_INTEGRATION_VAULT_READ_TOKEN")
	roleID := requiredIntegrationEnvironment(t, "VLTCONFIG_INTEGRATION_VAULT_ROLE_ID")
	secretID := requiredIntegrationEnvironment(t, "VLTCONFIG_INTEGRATION_VAULT_SECRET_ID")
	username := requiredIntegrationEnvironment(t, "VLTCONFIG_INTEGRATION_VAULT_USERNAME")
	password := requiredIntegrationEnvironment(t, "VLTCONFIG_INTEGRATION_VAULT_PASSWORD")

	tests := []struct {
		name        string
		credentials map[string]string
	}{
		{name: "token", credentials: map[string]string{VaultTokenEnvironment: readToken}},
		{name: "AppRole", credentials: map[string]string{
			VaultRoleIDEnvironment: roleID, VaultSecretIDEnvironment: secretID,
		}},
		{name: "userpass", credentials: map[string]string{
			VaultUsernameEnvironment: username, VaultPasswordEnvironment: password,
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			configureIntegrationVaultEnvironment(t, address, "integration/config", testCase.credentials)
			configuration, err := Load[integrationConfiguration](context.Background())
			if err != nil {
				t.Fatalf("load through %s: %v", testCase.name, err)
			}
			if configuration.ServiceName != "integration-service" || configuration.Workers != 3 {
				t.Fatalf("configuration = %#v", configuration)
			}
		})
	}
}

func TestDisposableVaultFailureCategories(t *testing.T) {
	address := requiredIntegrationEnvironment(t, "VLTCONFIG_INTEGRATION_VAULT_ADDRESS")
	readToken := requiredIntegrationEnvironment(t, "VLTCONFIG_INTEGRATION_VAULT_READ_TOKEN")
	deniedToken := requiredIntegrationEnvironment(t, "VLTCONFIG_INTEGRATION_VAULT_DENIED_TOKEN")

	t.Run("missing path", func(t *testing.T) {
		configureIntegrationVaultEnvironment(t, address, "integration/missing", map[string]string{
			VaultTokenEnvironment: readToken,
		})
		_, err := Load[integrationConfiguration](context.Background())
		if !errors.Is(err, ErrVaultPathNotFound) {
			t.Fatalf("error = %v, want path not found", err)
		}
	})

	t.Run("forbidden path", func(t *testing.T) {
		configureIntegrationVaultEnvironment(t, address, "integration/config", map[string]string{
			VaultTokenEnvironment: deniedToken,
		})
		_, err := Load[integrationConfiguration](context.Background())
		if !errors.Is(err, ErrVaultAuthorization) {
			t.Fatalf("error = %v, want authorization", err)
		}
	})
}

func requiredIntegrationEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Skip("run through scripts/test-integration.sh")
	}
	return value
}

func configureIntegrationVaultEnvironment(
	t *testing.T,
	address string,
	path string,
	credentials map[string]string,
) {
	t.Helper()
	for _, name := range vaultAccessEnvironmentNames {
		unsetIntegrationEnvironment(t, name)
	}
	for _, name := range []string{
		VaultCacheDisabledEnvironment,
		VaultCacheTTLEnvironment,
		VaultCacheMaxEntriesEnvironment,
		JSONPathEnvironment,
	} {
		unsetIntegrationEnvironment(t, name)
	}
	t.Setenv(VaultAddressEnvironment, address)
	t.Setenv(VaultAppNameEnvironment, path)
	t.Setenv(VaultMountPointEnvironment, "secret")
	t.Setenv(VaultCacheDisabledEnvironment, "true")
	for name, value := range credentials {
		t.Setenv(name, value)
	}
}

func unsetIntegrationEnvironment(t *testing.T, name string) {
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
