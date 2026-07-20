package vltconfig

import (
	"context"
	"errors"

	"github.com/hashicorp/vault/api"
)

type hashicorpAPIClient struct {
	client *api.Client
}

func newAPIVaultClient(configuration vaultConfig) (vaultClient, error) {
	clientConfiguration := api.DefaultConfig()
	if clientConfiguration.Error != nil {
		return nil, clientConfiguration.Error
	}
	clientConfiguration.Address = configuration.address
	clientConfiguration.MaxRetries = 0
	client, err := api.NewClient(clientConfiguration)
	if err != nil {
		return nil, err
	}
	return &apiVaultClient{client: &hashicorpAPIClient{client: client}}, nil
}

func (client *hashicorpAPIClient) SetToken(token string) {
	client.client.SetToken(token)
}

func (client *hashicorpAPIClient) lookupSelf(ctx context.Context) (bool, error) {
	secret, err := client.client.Auth().Token().LookupSelfWithContext(ctx)
	if err != nil {
		return false, wrapAPIResponseError(err)
	}
	return secret != nil, nil
}

func (client *hashicorpAPIClient) write(
	ctx context.Context,
	path string,
	data map[string]any,
) (string, error) {
	secret, err := client.client.Logical().WriteWithContext(ctx, path, data)
	if err != nil {
		return "", wrapAPIResponseError(err)
	}
	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		return "", errVaultMalformedResponse
	}
	return secret.Auth.ClientToken, nil
}

func (client *hashicorpAPIClient) readKV(
	ctx context.Context,
	mount string,
	path string,
) (map[string]any, bool, error) {
	secret, err := client.client.KVv2(mount).Get(ctx, path)
	if err != nil {
		return nil, false, wrapAPIKVReadError(err)
	}
	if secret == nil {
		return nil, false, errVaultMalformedResponse
	}
	if secret.Data == nil {
		if secret.VersionMetadata != nil && !secret.VersionMetadata.DeletionTime.IsZero() {
			return nil, false, nil
		}
		return nil, false, errVaultMalformedResponse
	}
	return cloneStringMap(secret.Data), true, nil
}

func wrapAPIKVReadError(err error) error {
	if errors.Is(err, api.ErrSecretNotFound) {
		return &responseStatusError{statusCode: 404}
	}
	return wrapAPIResponseError(err)
}

func wrapAPIResponseError(err error) error {
	var response *api.ResponseError
	if errors.As(err, &response) {
		return &responseStatusError{statusCode: response.StatusCode, cause: err}
	}
	return err
}
