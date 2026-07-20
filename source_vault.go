package vltconfig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"
)

type vaultSource struct {
	lookup  func(string) (string, bool)
	policy  VaultFailurePolicy
	factory vaultClientFactory
	retry   vaultRetry
	cache   *vaultCache
	logger  *slog.Logger
}

func (vaultSource) name() string {
	return "Vault"
}

func (source vaultSource) load(ctx context.Context, _ *compiledSchema) (map[string]any, error) {
	configuration, configured, err := vaultConfigFromEnvironment(source.lookup)
	if err != nil {
		return source.handleFailure(ctx, err)
	}
	if !configured {
		if source.logger != nil {
			source.logger.DebugContext(ctx, "configuration source skipped",
				"source", "vault", "reason", "not configured")
		}
		return map[string]any{}, nil
	}
	if cached, found := source.cache.get(configuration); found {
		if source.logger != nil {
			source.logger.DebugContext(ctx, "Vault cache hit",
				"source", "vault", "mount", configuration.mountPoint,
				"path", configuration.appName)
		}
		return cached, nil
	}
	if source.logger != nil && source.retry.notify == nil {
		source.retry.notify = func(ctx context.Context, failedAttempt int, delay time.Duration) {
			source.logger.WarnContext(ctx, "Vault retry scheduled",
				"source", "vault", "failed_attempt", failedAttempt,
				"next_attempt", failedAttempt+1, "wait", delay)
		}
	}

	var data map[string]any
	err = source.retry.call(ctx, func() error {
		client, err := source.factory(configuration)
		if err != nil {
			return &vaultClientCreationError{causeType: fmt.Sprintf("%T", err)}
		}
		if err := (vaultAuthenticator{}).authenticate(ctx, configuration, client); err != nil {
			return err
		}
		loaded, err := client.readKVv2(ctx, configuration.mountPoint, configuration.appName)
		if err != nil {
			return err
		}
		data = loaded
		return nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return source.handleFailure(ctx, classifyVaultError(err, configuration, "load"))
	}
	source.cache.put(configuration, data)
	return data, nil
}

func (source vaultSource) handleFailure(ctx context.Context, err error) (map[string]any, error) {
	if source.policy == VaultFailureFallback && errors.Is(err, ErrVault) {
		if source.logger != nil {
			source.logger.WarnContext(ctx, "Vault failure ignored by fallback policy",
				"source", "vault", "category", vaultFailureCategory(err))
		}
		return map[string]any{}, nil
	}
	return nil, err
}

func vaultFailureCategory(err error) string {
	switch {
	case errors.Is(err, ErrVaultConfiguration):
		return "configuration"
	case errors.Is(err, ErrVaultAuthentication):
		return "authentication"
	case errors.Is(err, ErrVaultAuthorization):
		return "authorization"
	case errors.Is(err, ErrVaultPathNotFound):
		return "path_not_found"
	case errors.Is(err, ErrVaultUnavailable):
		return "unavailable"
	case errors.Is(err, ErrVaultResponse):
		return "response"
	default:
		return "vault"
	}
}

type vaultClientCreationError struct {
	causeType string
}

func (e *vaultClientCreationError) Error() string {
	return "vault client could not be created"
}

func classifyVaultError(err error, configuration vaultConfig, operation string) error {
	kind := ErrVaultResponse
	causeType := fmt.Sprintf("%T", err)
	var creationError *vaultClientCreationError
	switch {
	case errors.As(err, &creationError):
		kind = ErrVaultConfiguration
		causeType = creationError.causeType
	case errors.Is(err, errVaultAuthenticationRejected):
		kind = ErrVaultAuthentication
	case errors.Is(err, errVaultMalformedResponse):
		kind = ErrVaultResponse
	default:
		var response *responseStatusError
		if errors.As(err, &response) {
			switch {
			case response.statusCode == 401:
				kind = ErrVaultAuthentication
			case response.statusCode == 403:
				kind = ErrVaultAuthorization
			case response.statusCode == 404:
				kind = ErrVaultPathNotFound
			case response.statusCode == 429 || response.statusCode >= 500:
				kind = ErrVaultUnavailable
			default:
				kind = ErrVaultResponse
			}
		} else if isNetworkError(err) {
			kind = ErrVaultUnavailable
		}
	}
	return &VaultError{
		Operation: operation,
		Mount:     configuration.mountPoint,
		Path:      configuration.appName,
		CauseType: causeType,
		kind:      kind,
	}
}

func isNetworkError(err error) bool {
	var networkError net.Error
	if errors.As(err, &networkError) {
		return true
	}
	var operationError *net.OpError
	return errors.As(err, &operationError)
}
