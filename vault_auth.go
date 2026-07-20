package vltconfig

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

var (
	errVaultAuthenticationRejected = errors.New("vault authentication rejected")
	errVaultMalformedResponse      = errors.New("vault returned a malformed response")
)

type vaultClient interface {
	setToken(string)
	lookupToken(context.Context) error
	loginAppRole(context.Context, string, string) (string, error)
	loginUserpass(context.Context, string, string) (string, error)
	readKVv2(context.Context, string, string) (map[string]any, error)
}

type vaultClientFactory func(vaultConfig) (vaultClient, error)

type vaultAuthenticator struct{}

func (vaultAuthenticator) authenticate(
	ctx context.Context,
	configuration vaultConfig,
	client vaultClient,
) error {
	rejected := false

	if configuration.token != "" {
		client.setToken(configuration.token)
		err := client.lookupToken(ctx)
		switch {
		case err == nil:
			return nil
		case isAuthenticationRejection(err):
			rejected = true
		default:
			return err
		}
	}

	if configuration.roleID != "" && configuration.secretID != "" {
		token, err := client.loginAppRole(ctx, configuration.roleID, configuration.secretID)
		switch {
		case err == nil:
			if token == "" {
				return errVaultMalformedResponse
			}
			client.setToken(token)
			return nil
		case isAuthenticationRejection(err):
			rejected = true
		default:
			return err
		}
	}

	if configuration.username != "" && configuration.password != "" {
		token, err := client.loginUserpass(ctx, configuration.username, configuration.password)
		switch {
		case err == nil:
			if token == "" {
				return errVaultMalformedResponse
			}
			client.setToken(token)
			return nil
		case isAuthenticationRejection(err):
			rejected = true
		default:
			return err
		}
	}

	if rejected {
		return errVaultAuthenticationRejected
	}
	return errVaultAuthenticationRejected
}

func isAuthenticationRejection(err error) bool {
	var response *responseStatusError
	if errors.As(err, &response) {
		return response.statusCode == 400 || response.statusCode == 401 || response.statusCode == 403
	}
	return errors.Is(err, errVaultAuthenticationRejected)
}

type responseStatusError struct {
	statusCode int
	cause      error
}

func (e *responseStatusError) Error() string {
	return fmt.Sprintf("Vault returned HTTP status %d", e.statusCode)
}

func (e *responseStatusError) Unwrap() error {
	return e.cause
}

type apiVaultClient struct {
	client apiClient
}

type apiClient interface {
	SetToken(string)
	lookupSelf(context.Context) (bool, error)
	write(context.Context, string, map[string]any) (string, error)
	readKV(context.Context, string, string) (map[string]any, bool, error)
}

func (client *apiVaultClient) setToken(token string) {
	client.client.SetToken(token)
}

func (client *apiVaultClient) lookupToken(ctx context.Context) error {
	valid, err := client.client.lookupSelf(ctx)
	if err != nil {
		return err
	}
	if !valid {
		return errVaultMalformedResponse
	}
	return nil
}

func (client *apiVaultClient) loginAppRole(
	ctx context.Context,
	roleID string,
	secretID string,
) (string, error) {
	return client.client.write(ctx, "auth/approle/login", map[string]any{
		"role_id":   roleID,
		"secret_id": secretID,
	})
}

func (client *apiVaultClient) loginUserpass(
	ctx context.Context,
	username string,
	password string,
) (string, error) {
	return client.client.write(ctx, "auth/userpass/login/"+url.PathEscape(username), map[string]any{
		"password": password,
	})
}

func (client *apiVaultClient) readKVv2(
	ctx context.Context,
	mount string,
	path string,
) (map[string]any, error) {
	data, found, err := client.client.readKV(ctx, mount, path)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, &responseStatusError{statusCode: 404}
	}
	if data == nil {
		return nil, errVaultMalformedResponse
	}
	return cloneStringMap(data), nil
}
