package vltconfig

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type recordingAPIClient struct {
	token      string
	lookupOK   bool
	lookupErr  error
	writePath  string
	writeData  map[string]any
	writeToken string
	writeErr   error
	readMount  string
	readPath   string
	readData   map[string]any
	readFound  bool
	readErr    error
}

func (client *recordingAPIClient) SetToken(token string) {
	client.token = token
}

func (client *recordingAPIClient) lookupSelf(context.Context) (bool, error) {
	return client.lookupOK, client.lookupErr
}

func (client *recordingAPIClient) write(
	_ context.Context,
	path string,
	data map[string]any,
) (string, error) {
	client.writePath = path
	client.writeData = cloneStringMap(data)
	return client.writeToken, client.writeErr
}

func (client *recordingAPIClient) readKV(
	_ context.Context,
	mount string,
	path string,
) (map[string]any, bool, error) {
	client.readMount = mount
	client.readPath = path
	if client.readData == nil {
		return nil, client.readFound, client.readErr
	}
	return cloneStringMap(client.readData), client.readFound, client.readErr
}

func TestAPIVaultClientMapsAuthenticationRequests(t *testing.T) {
	t.Parallel()

	recorder := &recordingAPIClient{lookupOK: true, writeToken: "fake-issued-token"}
	client := &apiVaultClient{client: recorder}
	client.setToken("fake-static-token")
	if err := client.lookupToken(context.Background()); err != nil {
		t.Fatalf("lookup token: %v", err)
	}
	if recorder.token != "fake-static-token" {
		t.Fatalf("token = %q", recorder.token)
	}

	token, err := client.loginAppRole(context.Background(), "fake-role", "fake-secret")
	if err != nil || token != "fake-issued-token" {
		t.Fatalf("AppRole token = %q, error = %v", token, err)
	}
	if recorder.writePath != "auth/approle/login" || !reflect.DeepEqual(recorder.writeData, map[string]any{
		"role_id": "fake-role", "secret_id": "fake-secret",
	}) {
		t.Fatalf("AppRole request = %q %#v", recorder.writePath, recorder.writeData)
	}

	_, err = client.loginUserpass(context.Background(), "fake/user", "fake-password")
	if err != nil {
		t.Fatalf("userpass: %v", err)
	}
	if recorder.writePath != "auth/userpass/login/fake%2Fuser" {
		t.Fatalf("userpass path = %q", recorder.writePath)
	}
	if !reflect.DeepEqual(recorder.writeData, map[string]any{"password": "fake-password"}) {
		t.Fatalf("userpass request = %#v", recorder.writeData)
	}
}

func TestAPIVaultClientValidatesResponses(t *testing.T) {
	t.Parallel()

	recorder := &recordingAPIClient{}
	client := &apiVaultClient{client: recorder}
	if err := client.lookupToken(context.Background()); !errors.Is(err, errVaultMalformedResponse) {
		t.Fatalf("lookup error = %v", err)
	}

	recorder.writeToken = ""
	if token, err := client.loginAppRole(context.Background(), "fake-role", "fake-secret"); err != nil || token != "" {
		t.Fatalf("login token = %q, error = %v", token, err)
	}

	recorder.readFound = false
	if _, err := client.readKVv2(context.Background(), "secret", "app"); !isStatus(err, 404) {
		t.Fatalf("missing read error = %v", err)
	}

	recorder.readFound = true
	recorder.readData = nil
	if _, err := client.readKVv2(context.Background(), "secret", "app"); !errors.Is(err, errVaultMalformedResponse) {
		t.Fatalf("nil read error = %v", err)
	}
}

func TestAPIVaultClientReadsIsolatedKVData(t *testing.T) {
	t.Parallel()

	recorder := &recordingAPIClient{
		readFound: true,
		readData:  map[string]any{"nested": map[string]any{"value": "stable"}},
	}
	client := &apiVaultClient{client: recorder}
	data, err := client.readKVv2(context.Background(), "application", "service/config")
	if err != nil {
		t.Fatalf("read KV: %v", err)
	}
	data["nested"].(map[string]any)["value"] = "mutated"
	if recorder.readData["nested"].(map[string]any)["value"] != "stable" {
		t.Fatal("read result shared nested state")
	}
	if recorder.readMount != "application" || recorder.readPath != "service/config" {
		t.Fatalf("read location = %q %q", recorder.readMount, recorder.readPath)
	}
}

func TestOfficialVaultClientDisablesInternalRetries(t *testing.T) {
	t.Parallel()

	client, err := newAPIVaultClient(vaultConfig{address: "https://vault.example.test"})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	adapter := client.(*apiVaultClient)
	production := adapter.client.(*hashicorpAPIClient)
	if retries := production.client.CloneConfig().MaxRetries; retries != 0 {
		t.Fatalf("official client retries = %d, want 0", retries)
	}
}

func isStatus(err error, status int) bool {
	var response *responseStatusError
	return errors.As(err, &response) && response.statusCode == status
}
