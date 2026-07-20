package vltconfig

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hashicorp/vault/api"
)

func TestOfficialVaultMissingSecretMapsToPathNotFound(t *testing.T) {
	t.Parallel()

	upstream := fmt.Errorf("SDK path context: %w", api.ErrSecretNotFound)
	wrapped := wrapAPIKVReadError(upstream)
	if !isStatus(wrapped, 404) || errors.Is(wrapped, api.ErrSecretNotFound) {
		t.Fatalf("wrapped error = %v, want value-free status 404", wrapped)
	}
}
