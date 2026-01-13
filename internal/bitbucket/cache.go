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

type Cache struct {
	dir string
	ttl time.Duration
}

type cacheEntry struct {
	Data      json.RawMessage `json:"data"`
	CachedAt  time.Time       `json:"cached_at"`
	ExpiresAt time.Time       `json:"expires_at"`
}

func NewCache() (*Cache, error) {
	cacheDir, err := cacheDir()
	if err != nil {
		return nil, err
	}
	return &Cache{dir: cacheDir, ttl: defaultTTL}, nil
}

func cacheDir() (string, error) {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "atlas"), nil
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
			os.Remove(filepath.Join(c.dir, entry.Name()))
		}
	}
	return nil
}

func (c *Cache) keyPath(key string) string {
	hash := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(hash[:]))
}
