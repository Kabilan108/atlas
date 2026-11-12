package bitbucket

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const defaultTTL = 5 * time.Minute

// Cache keeps GET responses on disk for a few minutes. A client uses one only
// when given WithCache.
type Cache struct {
	dir            string
	ttl            time.Duration
	invalidateOnly bool
}

type cacheEntry struct {
	Data      json.RawMessage `json:"data"`
	CachedAt  time.Time       `json:"cached_at"`
	ExpiresAt time.Time       `json:"expires_at"`
}

// NewCache stores entries in dir, creating it on the first write.
func NewCache(dir string) *Cache {
	return &Cache{dir: dir, ttl: defaultTTL}
}

// InvalidateOnly returns a cache that serves and stores nothing but is still
// cleared after writes, so a write made while bypassing the cache does not
// leave stale entries for later cached reads.
func (c *Cache) InvalidateOnly() *Cache {
	return &Cache{dir: c.dir, ttl: c.ttl, invalidateOnly: true}
}

func (c *Cache) serves() bool {
	return c != nil && !c.invalidateOnly
}

func (c *Cache) Get(key string) ([]byte, bool) {
	path := c.keyPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	if time.Now().After(entry.ExpiresAt) {
		os.Remove(path)
		return nil, false
	}

	return entry.Data, true
}

func (c *Cache) Set(key string, data []byte) error {
	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return err
	}

	now := time.Now()
	entry := cacheEntry{
		Data:      data,
		CachedAt:  now,
		ExpiresAt: now.Add(c.ttl),
	}

	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	return os.WriteFile(c.keyPath(key), encoded, 0600)
}

func (c *Cache) Delete(key string) error {
	return os.Remove(c.keyPath(key))
}

func (c *Cache) Clear() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			if err := os.Remove(filepath.Join(c.dir, entry.Name())); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func (c *Cache) keyPath(key string) string {
	hash := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(hash[:]))
}
