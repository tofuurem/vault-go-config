package vltconfig

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestVaultCacheReturnsMutationIsolatedCopies(t *testing.T) {
	t.Parallel()

	cache := testVaultCache(nil)
	configuration := testVaultCacheConfiguration("token-a", "app")
	original := map[string]any{
		"db":    map[string]any{"host": "stable"},
		"items": []any{map[string]any{"name": "first"}},
	}
	cache.put(configuration, original)
	original["db"].(map[string]any)["host"] = "original-mutated"

	first, found := cache.get(configuration)
	if !found {
		t.Fatal("expected cached value")
	}
	first["db"].(map[string]any)["host"] = "result-mutated"
	first["items"].([]any)[0].(map[string]any)["name"] = "result-mutated"
	second, found := cache.get(configuration)
	if !found || second["db"].(map[string]any)["host"] != "stable" ||
		second["items"].([]any)[0].(map[string]any)["name"] != "first" {
		t.Fatalf("cached data was mutated: %#v", second)
	}
}

func TestVaultCacheExpiresAndHonorsShorterCurrentTTL(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	cache := testVaultCache(func() time.Time { return now })
	configuration := testVaultCacheConfiguration("token-a", "app")
	cache.put(configuration, map[string]any{"value": "cached"})

	now = now.Add(30 * time.Second)
	configuration.cache.ttl = 20 * time.Second
	if _, found := cache.get(configuration); found {
		t.Fatal("cache entry survived the current shorter TTL")
	}

	configuration.cache.ttl = time.Minute
	cache.put(configuration, map[string]any{"value": "fresh"})
	now = now.Add(time.Minute)
	if _, found := cache.get(configuration); found {
		t.Fatal("cache entry survived expiration")
	}
}

func TestVaultCacheEvictsLeastRecentlyUsedEntry(t *testing.T) {
	t.Parallel()

	cache := testVaultCache(nil)
	first := testVaultCacheConfiguration("token-a", "first")
	first.cache.maxEntries = 2
	second := testVaultCacheConfiguration("token-a", "second")
	second.cache.maxEntries = 2
	third := testVaultCacheConfiguration("token-a", "third")
	third.cache.maxEntries = 2
	cache.put(first, map[string]any{"value": "first"})
	cache.put(second, map[string]any{"value": "second"})
	if _, found := cache.get(first); !found {
		t.Fatal("expected first entry")
	}
	cache.put(third, map[string]any{"value": "third"})

	if _, found := cache.get(second); found {
		t.Fatal("least recently used entry was not evicted")
	}
	if _, found := cache.get(first); !found {
		t.Fatal("recently used entry was evicted")
	}
}

func TestVaultCacheKeyIsolatesEveryConnectionDimension(t *testing.T) {
	t.Parallel()

	cache := testVaultCache(nil)
	base := testVaultCacheConfiguration("secret-canary-token", "app")
	baseKey := cache.keyFor(base)
	if strings.Contains(fmt.Sprintf("%#v", baseKey), base.token) {
		t.Fatal("cache key contains a raw credential")
	}

	cases := map[string]func(*vaultConfig){
		"address":  func(value *vaultConfig) { value.address = "https://other.example.test" },
		"mount":    func(value *vaultConfig) { value.mountPoint = "other" },
		"path":     func(value *vaultConfig) { value.appName = "other" },
		"token":    func(value *vaultConfig) { value.token = "rotated" },
		"username": func(value *vaultConfig) { value.username = "user" },
		"password": func(value *vaultConfig) { value.password = "password" },
		"role ID":  func(value *vaultConfig) { value.roleID = "role" },
		"secret ID": func(value *vaultConfig) {
			value.secretID = "secret"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			if key := cache.keyFor(changed); key == baseKey {
				t.Fatalf("%s did not isolate cache key", name)
			}
		})
	}
}

func TestVaultCacheLengthPrefixesCredentialParts(t *testing.T) {
	t.Parallel()

	cache := testVaultCache(nil)
	first := testVaultCacheConfiguration("a", "app")
	first.username = "bc"
	second := testVaultCacheConfiguration("ab", "app")
	second.username = "c"
	if cache.keyFor(first) == cache.keyFor(second) {
		t.Fatal("different credential tuples produced one fingerprint")
	}
}

func TestVaultCacheDisabledAndZeroTTLBypassStorage(t *testing.T) {
	t.Parallel()

	cache := testVaultCache(nil)
	disabled := testVaultCacheConfiguration("token-a", "disabled")
	disabled.cache.enabled = false
	cache.put(disabled, map[string]any{"value": "ignored"})
	if _, found := cache.get(disabled); found {
		t.Fatal("disabled cache stored a value")
	}

	zeroTTL := testVaultCacheConfiguration("token-a", "zero")
	zeroTTL.cache.ttl = 0
	cache.put(zeroTTL, map[string]any{"value": "ignored"})
	if _, found := cache.get(zeroTTL); found {
		t.Fatal("zero-TTL cache stored a value")
	}
}

func TestVaultCacheIsConcurrentAndInvalidatable(t *testing.T) {
	t.Parallel()

	cache := testVaultCache(nil)
	configuration := testVaultCacheConfiguration("token-a", "app")
	var group sync.WaitGroup
	for index := range 64 {
		group.Add(1)
		go func() {
			defer group.Done()
			cache.put(configuration, map[string]any{"index": index})
			if data, found := cache.get(configuration); found {
				data["index"] = -1
			}
		}()
	}
	group.Wait()
	loader := &Loader{vaultCache: cache}
	loader.InvalidateVaultCache()
	if _, found := cache.get(configuration); found {
		t.Fatal("cache invalidation left an entry")
	}
	var nilLoader *Loader
	nilLoader.InvalidateVaultCache()
}

func TestVaultSourceUsesCacheWithoutSharingReturnedData(t *testing.T) {
	t.Parallel()

	cache := testVaultCache(nil)
	client := &fakeVaultClient{data: map[string]any{
		"db": map[string]any{"host": "vault-host"},
	}}
	source := tokenVaultSource(client, VaultFailureStrict)
	source.cache = cache
	first, err := source.load(context.Background(), nil)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	first["db"].(map[string]any)["host"] = "mutated"
	second, err := source.load(context.Background(), nil)
	if err != nil {
		t.Fatalf("cached load: %v", err)
	}
	if second["db"].(map[string]any)["host"] != "vault-host" {
		t.Fatalf("cached payload was mutated: %#v", second)
	}
	if want := []string{"set-token", "token", "read"}; !reflect.DeepEqual(client.calls, want) {
		t.Fatalf("cache did not bypass Vault: calls = %#v", client.calls)
	}

	(&Loader{vaultCache: cache}).InvalidateVaultCache()
	if _, err := source.load(context.Background(), nil); err != nil {
		t.Fatalf("load after invalidation: %v", err)
	}
	if len(client.calls) != 6 {
		t.Fatalf("Vault was not called after invalidation: %#v", client.calls)
	}
}

func testVaultCache(now func() time.Time) *vaultCache {
	if now == nil {
		now = time.Now
	}
	return newVaultCacheWithClock([]byte("0123456789abcdef0123456789abcdef"), now)
}

func testVaultCacheConfiguration(token, path string) vaultConfig {
	return vaultConfig{
		address:    "https://vault.example.test",
		token:      token,
		mountPoint: "secret",
		appName:    path,
		cache: vaultCacheConfig{
			enabled:    true,
			ttl:        time.Minute,
			maxEntries: 128,
		},
	}
}
