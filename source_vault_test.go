package vltconfig

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"
)

type fakeVaultClient struct {
	calls        []string
	tokens       []string
	lookupError  error
	appRoleToken string
	appRoleError error
	userToken    string
	userError    error
	data         map[string]any
	readError    error
}

func (client *fakeVaultClient) setToken(token string) {
	client.calls = append(client.calls, "set-token")
	client.tokens = append(client.tokens, token)
}

func (client *fakeVaultClient) lookupToken(context.Context) error {
	client.calls = append(client.calls, "token")
	return client.lookupError
}

func (client *fakeVaultClient) loginAppRole(context.Context, string, string) (string, error) {
	client.calls = append(client.calls, "approle")
	return client.appRoleToken, client.appRoleError
}

func (client *fakeVaultClient) loginUserpass(context.Context, string, string) (string, error) {
	client.calls = append(client.calls, "userpass")
	return client.userToken, client.userError
}

func (client *fakeVaultClient) readKVv2(context.Context, string, string) (map[string]any, error) {
	client.calls = append(client.calls, "read")
	return cloneStringMap(client.data), client.readError
}

func TestVaultSourceSkipsAbsentConfigurationWithoutCreatingClient(t *testing.T) {
	t.Parallel()

	created := false
	source := vaultSource{
		lookup: mapLookup(map[string]string{VaultCacheTTLEnvironment: "10"}),
		policy: VaultFailureStrict,
		factory: func(vaultConfig) (vaultClient, error) {
			created = true
			return nil, errors.New("must not run")
		},
	}
	data, err := source.load(context.Background(), nil)
	if err != nil || len(data) != 0 || created {
		t.Fatalf("data = %#v, created = %t, error = %v", data, created, err)
	}
}

func TestVaultSourceReturnsPartialNestedPayload(t *testing.T) {
	t.Parallel()

	client := &fakeVaultClient{data: map[string]any{
		"db": map[string]any{"host": "vault-host"},
	}}
	source := tokenVaultSource(client, VaultFailureStrict)
	data, err := source.load(context.Background(), nil)
	if err != nil {
		t.Fatalf("load Vault: %v", err)
	}
	want := map[string]any{"db": map[string]any{"host": "vault-host"}}
	if !reflect.DeepEqual(data, want) {
		t.Fatalf("data = %#v, want %#v", data, want)
	}
	if !reflect.DeepEqual(client.calls, []string{"set-token", "token", "read"}) {
		t.Fatalf("calls = %#v", client.calls)
	}
}

func TestVaultAuthFallsBackFromTokenToAppRole(t *testing.T) {
	t.Parallel()

	environment := validTokenVaultEnvironment()
	environment[VaultRoleIDEnvironment] = "fake-role"
	environment[VaultSecretIDEnvironment] = "fake-secret"
	client := &fakeVaultClient{
		lookupError:  statusError(403),
		appRoleToken: "fake-approle-token",
		data:         map[string]any{"value": "loaded"},
	}
	source := vaultSourceFromEnvironment(environment, client, VaultFailureStrict)

	if _, err := source.load(context.Background(), nil); err != nil {
		t.Fatalf("load Vault: %v", err)
	}
	if !reflect.DeepEqual(client.calls, []string{"set-token", "token", "approle", "set-token", "read"}) {
		t.Fatalf("calls = %#v", client.calls)
	}
	if got := client.tokens[len(client.tokens)-1]; got != "fake-approle-token" {
		t.Fatalf("active token = %q", got)
	}
}

func TestVaultAuthFallsBackThroughUserpass(t *testing.T) {
	t.Parallel()

	environment := validTokenVaultEnvironment()
	environment[VaultRoleIDEnvironment] = "fake-role"
	environment[VaultSecretIDEnvironment] = "fake-secret"
	environment[VaultUsernameEnvironment] = "fake-user"
	environment[VaultPasswordEnvironment] = "fake-password"
	client := &fakeVaultClient{
		lookupError:  statusError(401),
		appRoleError: statusError(400),
		userToken:    "fake-user-token",
		data:         map[string]any{"value": "loaded"},
	}
	source := vaultSourceFromEnvironment(environment, client, VaultFailureStrict)

	if _, err := source.load(context.Background(), nil); err != nil {
		t.Fatalf("load Vault: %v", err)
	}
	wantCalls := []string{"set-token", "token", "approle", "userpass", "set-token", "read"}
	if !reflect.DeepEqual(client.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", client.calls, wantCalls)
	}
}

func TestVaultAuthDoesNotFallBackOnNetworkFailure(t *testing.T) {
	t.Parallel()

	environment := validTokenVaultEnvironment()
	environment[VaultRoleIDEnvironment] = "fake-role"
	environment[VaultSecretIDEnvironment] = "fake-secret"
	client := &fakeVaultClient{lookupError: &net.DNSError{Err: "unavailable", Name: "vault.invalid"}}
	source := vaultSourceFromEnvironment(environment, client, VaultFailureStrict)

	_, err := source.load(context.Background(), nil)
	if !errors.Is(err, ErrVaultUnavailable) {
		t.Fatalf("error = %v, want unavailable", err)
	}
	wantCalls := []string{
		"set-token", "token",
		"set-token", "token",
		"set-token", "token",
	}
	if !reflect.DeepEqual(client.calls, wantCalls) {
		t.Fatalf("unexpected fallback calls: %#v", client.calls)
	}
}

func TestVaultAuthFailsAfterEveryMethodIsRejected(t *testing.T) {
	t.Parallel()

	environment := validTokenVaultEnvironment()
	environment[VaultRoleIDEnvironment] = "fake-role"
	environment[VaultSecretIDEnvironment] = "fake-secret"
	environment[VaultUsernameEnvironment] = "fake-user"
	environment[VaultPasswordEnvironment] = "fake-password"
	client := &fakeVaultClient{
		lookupError:  statusError(403),
		appRoleError: statusError(403),
		userError:    statusError(403),
	}
	source := vaultSourceFromEnvironment(environment, client, VaultFailureStrict)

	_, err := source.load(context.Background(), nil)
	if !errors.Is(err, ErrVaultAuthentication) {
		t.Fatalf("error = %v, want authentication", err)
	}
}

func TestVaultAuthRejectsEmptyIssuedTokenAsMalformedResponse(t *testing.T) {
	t.Parallel()

	environment := baseVaultEnvironment()
	environment[VaultRoleIDEnvironment] = "fake-role"
	environment[VaultSecretIDEnvironment] = "fake-secret"
	client := &fakeVaultClient{appRoleToken: ""}
	source := vaultSourceFromEnvironment(environment, client, VaultFailureStrict)

	_, err := source.load(context.Background(), nil)
	if !errors.Is(err, ErrVaultResponse) {
		t.Fatalf("error = %v, want malformed response", err)
	}
}

func TestVaultSourceClassifiesReadFailures(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err  error
		kind error
	}{
		"authorization": {err: statusError(403), kind: ErrVaultAuthorization},
		"missing":       {err: statusError(404), kind: ErrVaultPathNotFound},
		"rate limit":    {err: statusError(429), kind: ErrVaultUnavailable},
		"server":        {err: statusError(503), kind: ErrVaultUnavailable},
		"bad response":  {err: statusError(400), kind: ErrVaultResponse},
		"malformed":     {err: errVaultMalformedResponse, kind: ErrVaultResponse},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := &fakeVaultClient{readError: testCase.err}
			_, err := tokenVaultSource(client, VaultFailureStrict).load(context.Background(), nil)
			if !errors.Is(err, testCase.kind) || !errors.Is(err, ErrVault) || !errors.Is(err, ErrConfig) {
				t.Fatalf("error = %v, want %v hierarchy", err, testCase.kind)
			}
		})
	}
}

func TestVaultSourceStrictPartialConfigurationFailsBeforeClient(t *testing.T) {
	t.Parallel()

	created := false
	source := vaultSource{
		lookup: mapLookup(map[string]string{VaultAddressEnvironment: "https://vault.example.test"}),
		policy: VaultFailureStrict,
		factory: func(vaultConfig) (vaultClient, error) {
			created = true
			return nil, nil
		},
	}
	_, err := source.load(context.Background(), nil)
	if !errors.Is(err, ErrVaultConfiguration) || created {
		t.Fatalf("created = %t, error = %v", created, err)
	}
}

func TestVaultSourceExplicitFallbackSkipsTypedFailure(t *testing.T) {
	t.Parallel()

	source := vaultSource{
		lookup:  mapLookup(map[string]string{VaultAddressEnvironment: "not-a-url"}),
		policy:  VaultFailureFallback,
		factory: func(vaultConfig) (vaultClient, error) { return nil, errors.New("must not run") },
	}
	data, err := source.load(context.Background(), nil)
	if err != nil || len(data) != 0 {
		t.Fatalf("data = %#v, error = %v", data, err)
	}
}

func TestVaultSourceDoesNotLeakCredentialOrPayloadValues(t *testing.T) {
	t.Parallel()

	const canary = "vault-secret-canary"
	environment := validTokenVaultEnvironment()
	environment[VaultTokenEnvironment] = canary
	client := &fakeVaultClient{lookupError: statusError(403)}
	_, err := vaultSourceFromEnvironment(environment, client, VaultFailureStrict).load(context.Background(), nil)
	if err == nil {
		t.Fatal("expected authentication failure")
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("error leaked credential: %v", err)
	}
}

func TestVaultSourcePropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &fakeVaultClient{lookupError: context.Canceled}
	_, err := tokenVaultSource(client, VaultFailureFallback).load(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

func tokenVaultSource(client vaultClient, policy VaultFailurePolicy) vaultSource {
	return vaultSourceFromEnvironment(validTokenVaultEnvironment(), client, policy)
}

func vaultSourceFromEnvironment(
	environment map[string]string,
	client vaultClient,
	policy VaultFailurePolicy,
) vaultSource {
	return vaultSource{
		lookup: mapLookup(environment),
		policy: policy,
		factory: func(vaultConfig) (vaultClient, error) {
			return client, nil
		},
		retry: noWaitVaultRetry(),
	}
}

func statusError(status int) error {
	return &responseStatusError{statusCode: status, cause: fmt.Errorf("upstream status %d", status)}
}
