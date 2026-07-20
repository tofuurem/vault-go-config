package vltconfig

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Option configures a Loader. The option interface is sealed so package
// invariants cannot be bypassed by external implementations.
type Option interface {
	apply(*loaderOptions) error
}

type optionFunc func(*loaderOptions) error

func (function optionFunc) apply(options *loaderOptions) error {
	return function(options)
}

type loaderOptions struct {
	lookupEnv   func(string) (string, bool)
	jsonFile    string
	vaultPolicy VaultFailurePolicy
	logger      *slog.Logger
}

func defaultLoaderOptions() loaderOptions {
	return loaderOptions{
		lookupEnv:   os.LookupEnv,
		vaultPolicy: VaultFailureStrict,
	}
}

// VaultFailurePolicy controls whether a configured Vault failure stops a load.
type VaultFailurePolicy string

const (
	// VaultFailureStrict returns typed Vault failures and is the default.
	VaultFailureStrict VaultFailurePolicy = "strict"
	// VaultFailureFallback explicitly treats a typed Vault failure as an empty source.
	VaultFailureFallback VaultFailurePolicy = "fallback"
)

// WithVaultFailurePolicy selects strict or explicit fallback behavior.
func WithVaultFailurePolicy(policy VaultFailurePolicy) Option {
	return optionFunc(func(options *loaderOptions) error {
		if policy != VaultFailureStrict && policy != VaultFailureFallback {
			return &VaultError{CauseType: "unsupported failure policy", kind: ErrVaultConfiguration}
		}
		options.vaultPolicy = policy
		return nil
	})
}

// WithJSONFile selects an exact JSON file and takes precedence over
// PYDANTIC_JSON_PATH. The file must exist and contain one JSON object.
func WithJSONFile(path string) Option {
	return optionFunc(func(options *loaderOptions) error {
		if strings.TrimSpace(path) == "" {
			return &JSONError{CauseType: "empty path", kind: ErrJSON}
		}
		options.jsonFile = path
		return nil
	})
}

// WithLogger enables value-free operational events through a caller-owned
// logger. The library never changes the logger's handler or level.
func WithLogger(logger *slog.Logger) Option {
	return optionFunc(func(options *loaderOptions) error {
		if logger == nil {
			return fmt.Errorf("%w: logger must not be nil", ErrConfig)
		}
		options.logger = logger
		return nil
	})
}
