package vltconfig

import (
	"container/list"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"sync"
	"time"
)

const vaultCacheHMACKeySize = 32

type vaultCache struct {
	mutex   sync.Mutex
	entries map[vaultCacheKey]*list.Element
	order   *list.List
	hmacKey []byte
	now     func() time.Time
}

type vaultCacheKey struct {
	address               string
	mount                 string
	path                  string
	credentialFingerprint [sha256.Size]byte
}

type vaultCacheEntry struct {
	key       vaultCacheKey
	data      map[string]any
	createdAt time.Time
	expiresAt time.Time
}

func newVaultCache() (*vaultCache, error) {
	key := make([]byte, vaultCacheHMACKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return newVaultCacheWithClock(key, time.Now), nil
}

func newVaultCacheWithClock(key []byte, now func() time.Time) *vaultCache {
	return &vaultCache{
		entries: make(map[vaultCacheKey]*list.Element),
		order:   list.New(),
		hmacKey: append([]byte(nil), key...),
		now:     now,
	}
}

func (cache *vaultCache) get(configuration vaultConfig) (map[string]any, bool) {
	if cache == nil || !configuration.cache.enabled || configuration.cache.ttl <= 0 {
		return nil, false
	}

	key := cache.keyFor(configuration)
	now := cache.now()
	cache.mutex.Lock()
	cache.removeExpiredLocked(now)
	element, found := cache.entries[key]
	if !found {
		cache.trimLocked(configuration.cache.maxEntries)
		cache.mutex.Unlock()
		return nil, false
	}
	entry := element.Value.(*vaultCacheEntry)
	if !now.Before(entry.createdAt.Add(configuration.cache.ttl)) {
		cache.removeLocked(element)
		cache.trimLocked(configuration.cache.maxEntries)
		cache.mutex.Unlock()
		return nil, false
	}
	cache.order.MoveToFront(element)
	cache.trimLocked(configuration.cache.maxEntries)
	data := entry.data
	cache.mutex.Unlock()
	return cloneStringMap(data), true
}

func (cache *vaultCache) put(configuration vaultConfig, data map[string]any) {
	if cache == nil || !configuration.cache.enabled || configuration.cache.ttl <= 0 ||
		configuration.cache.maxEntries < 1 {
		return
	}

	key := cache.keyFor(configuration)
	now := cache.now()
	entryData := cloneStringMap(data)
	cache.mutex.Lock()
	cache.removeExpiredLocked(now)
	if element, found := cache.entries[key]; found {
		entry := element.Value.(*vaultCacheEntry)
		entry.data = entryData
		entry.createdAt = now
		entry.expiresAt = now.Add(configuration.cache.ttl)
		cache.order.MoveToFront(element)
	} else {
		entry := &vaultCacheEntry{
			key:       key,
			data:      entryData,
			createdAt: now,
			expiresAt: now.Add(configuration.cache.ttl),
		}
		cache.entries[key] = cache.order.PushFront(entry)
	}
	cache.trimLocked(configuration.cache.maxEntries)
	cache.mutex.Unlock()
}

func (cache *vaultCache) invalidate() {
	if cache == nil {
		return
	}
	cache.mutex.Lock()
	clear(cache.entries)
	cache.order.Init()
	cache.mutex.Unlock()
}

func (cache *vaultCache) keyFor(configuration vaultConfig) vaultCacheKey {
	digest := hmac.New(sha256.New, cache.hmacKey)
	writeCacheKeyPart(digest, configuration.token)
	writeCacheKeyPart(digest, configuration.username)
	writeCacheKeyPart(digest, configuration.password)
	writeCacheKeyPart(digest, configuration.roleID)
	writeCacheKeyPart(digest, configuration.secretID)
	var fingerprint [sha256.Size]byte
	copy(fingerprint[:], digest.Sum(nil))
	return vaultCacheKey{
		address:               configuration.address,
		mount:                 configuration.mountPoint,
		path:                  configuration.appName,
		credentialFingerprint: fingerprint,
	}
}

func writeCacheKeyPart(destination io.Writer, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = io.WriteString(destination, value)
}

func (cache *vaultCache) removeExpiredLocked(now time.Time) {
	for element := cache.order.Back(); element != nil; {
		previous := element.Prev()
		entry := element.Value.(*vaultCacheEntry)
		if !now.Before(entry.expiresAt) {
			cache.removeLocked(element)
		}
		element = previous
	}
}

func (cache *vaultCache) trimLocked(maxEntries int) {
	for cache.order.Len() > maxEntries {
		cache.removeLocked(cache.order.Back())
	}
}

func (cache *vaultCache) removeLocked(element *list.Element) {
	entry := element.Value.(*vaultCacheEntry)
	delete(cache.entries, entry.key)
	cache.order.Remove(element)
}
