package vltconfig

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	VaultAddressEnvironment         = "VAULT_ADDRESS"
	VaultTokenEnvironment           = "VAULT_TOKEN"
	VaultUsernameEnvironment        = "VAULT_USERNAME"
	VaultPasswordEnvironment        = "VAULT_PASSWORD"
	VaultRoleIDEnvironment          = "VAULT_ROLE_ID"
	VaultSecretIDEnvironment        = "VAULT_SECRET_ID"
	VaultAppNameEnvironment         = "VAULT_APP_NAME"
	VaultMountPointEnvironment      = "VAULT_MOUNT_POINT"
	VaultCacheDisabledEnvironment   = "VAULT_CACHE_DISABLED"
	VaultCacheTTLEnvironment        = "VAULT_CACHE_TTL"
	VaultCacheMaxEntriesEnvironment = "VAULT_CACHE_MAX_ENTRIES"
)

var vaultAccessEnvironmentNames = [...]string{
	VaultAddressEnvironment,
	VaultTokenEnvironment,
	VaultUsernameEnvironment,
	VaultPasswordEnvironment,
	VaultRoleIDEnvironment,
	VaultSecretIDEnvironment,
	VaultAppNameEnvironment,
	VaultMountPointEnvironment,
}

type vaultConfig struct {
	address    string
	token      string
	username   string
	password   string
	roleID     string
	secretID   string
	appName    string
	mountPoint string
	cache      vaultCacheConfig
}

type vaultCacheConfig struct {
	enabled    bool
	ttl        time.Duration
	maxEntries int
}

func vaultConfigFromEnvironment(
	lookup func(string) (string, bool),
) (vaultConfig, bool, error) {
	if !vaultEnvironmentPresent(lookup) {
		return vaultConfig{}, false, nil
	}

	configuration := vaultConfig{
		address:    strings.TrimSpace(environmentValueOrEmpty(lookup, VaultAddressEnvironment)),
		token:      environmentValueOrEmpty(lookup, VaultTokenEnvironment),
		username:   environmentValueOrEmpty(lookup, VaultUsernameEnvironment),
		password:   environmentValueOrEmpty(lookup, VaultPasswordEnvironment),
		roleID:     environmentValueOrEmpty(lookup, VaultRoleIDEnvironment),
		secretID:   environmentValueOrEmpty(lookup, VaultSecretIDEnvironment),
		appName:    normalizeVaultPath(environmentValueOrEmpty(lookup, VaultAppNameEnvironment)),
		mountPoint: normalizeVaultPath(environmentValueOrEmpty(lookup, VaultMountPointEnvironment)),
		cache: vaultCacheConfig{
			enabled:    true,
			ttl:        5 * time.Minute,
			maxEntries: 128,
		},
	}
	if configuration.mountPoint == "" {
		configuration.mountPoint = "secret"
	}

	if err := configuration.validate(); err != nil {
		return vaultConfig{}, true, err
	}
	cache, err := parseVaultCacheConfig(lookup, configuration.cache)
	if err != nil {
		return vaultConfig{}, true, err
	}
	configuration.cache = cache
	return configuration, true, nil
}

func vaultEnvironmentPresent(lookup func(string) (string, bool)) bool {
	for _, name := range vaultAccessEnvironmentNames {
		if _, present := lookup(name); present {
			return true
		}
	}
	return false
}

func (configuration vaultConfig) validate() error {
	if err := validateVaultAddress(configuration.address); err != nil {
		return vaultConfigurationError(err)
	}
	if configuration.appName == "" {
		return vaultConfigurationError(fmt.Errorf("%s is required", VaultAppNameEnvironment))
	}
	if !configuration.hasCompleteAuthMethod() {
		return vaultConfigurationError(fmt.Errorf("no complete authentication method"))
	}
	return nil
}

func (configuration vaultConfig) hasCompleteAuthMethod() bool {
	return configuration.token != "" ||
		(configuration.roleID != "" && configuration.secretID != "") ||
		(configuration.username != "" && configuration.password != "")
}

func validateVaultAddress(address string) error {
	if strings.TrimSpace(address) == "" {
		return fmt.Errorf("%s is required", VaultAddressEnvironment)
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("vault address must use HTTP or HTTPS")
	}
	if parsed.Host == "" {
		return fmt.Errorf("vault address must contain a host")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("vault address must not contain user information, a query, or a fragment")
	}
	return nil
}

func parseVaultCacheConfig(
	lookup func(string) (string, bool),
	defaults vaultCacheConfig,
) (vaultCacheConfig, error) {
	configuration := defaults
	if raw, present := lookup(VaultCacheDisabledEnvironment); present {
		disabled, err := parseVaultBoolean(raw)
		if err != nil {
			return vaultCacheConfig{}, vaultConfigurationError(err)
		}
		configuration.enabled = !disabled
	}
	if raw, present := lookup(VaultCacheTTLEnvironment); present {
		seconds, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		maximumSeconds := float64(math.MaxInt64) / float64(time.Second)
		if err != nil || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > maximumSeconds {
			return vaultCacheConfig{}, vaultConfigurationError(fmt.Errorf("invalid cache TTL"))
		}
		configuration.ttl = time.Duration(seconds * float64(time.Second))
	}
	if raw, present := lookup(VaultCacheMaxEntriesEnvironment); present {
		maximum, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || maximum < 1 {
			return vaultCacheConfig{}, vaultConfigurationError(fmt.Errorf("invalid cache maximum"))
		}
		configuration.maxEntries = maximum
	}
	return configuration, nil
}

func parseVaultBoolean(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean")
	}
}

func vaultConfigurationError(cause error) *VaultError {
	return &VaultError{
		Operation: "configure",
		CauseType: fmt.Sprintf("%T", cause),
		kind:      ErrVaultConfiguration,
	}
}

func environmentValueOrEmpty(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)
	return value
}

func normalizeVaultPath(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}
