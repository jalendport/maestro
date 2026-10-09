// Package discovery keeps the generated cache of what each Harness can run
// today: path, version, login hint, catalog. Maestro rewrites it freely.
package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jalendport/maestro/internal/config"
	"github.com/jalendport/maestro/internal/harness"
)

// Record is one Harness's discovery result.
type Record struct {
	Path         string          `json:"path"`
	Version      string          `json:"version"`
	LoginHint    string          `json:"login_hint,omitempty"`
	Models       []harness.Model `json:"models"`
	SoloTool     int             `json:"solo_tool,omitempty"`
	DiscoveredAt time.Time       `json:"discovered_at"`
	Error        string          `json:"error,omitempty"`
}

// Cache is the discovery file.
type Cache struct {
	Harness     map[string]Record `json:"harness"`
	RefreshedAt time.Time         `json:"refreshed_at"`
}

// MaxAge is how old the cache may be before a Route refreshes it.
const MaxAge = 24 * time.Hour

// Load reads the cache; a missing file is an empty, stale cache.
func Load() (*Cache, error) {
	b, err := os.ReadFile(config.DiscoveryFile())
	if errors.Is(err, os.ErrNotExist) {
		return &Cache{Harness: map[string]Record{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Cache
	if err := json.Unmarshal(b, &c); err != nil {
		return &Cache{Harness: map[string]Record{}}, nil
	}
	if c.Harness == nil {
		c.Harness = map[string]Record{}
	}
	return &c, nil
}

// Stale reports whether the cache is missing or older than MaxAge.
func (c *Cache) Stale() bool {
	return c.RefreshedAt.IsZero() || time.Since(c.RefreshedAt) > MaxAge
}

// Refresh runs every adapter's version and catalog commands and writes the cache.
func Refresh(reg *harness.Registry) (*Cache, error) {
	c := &Cache{Harness: map[string]Record{}, RefreshedAt: time.Now()}
	for _, name := range reg.Names() {
		h, _ := reg.Get(name)
		rec := Record{Path: h.Bin(), DiscoveredAt: time.Now()}
		if h.Bin() == "" {
			rec.Error = "not installed"
			c.Harness[name] = rec
			continue
		}
		v, err := h.Version()
		if err != nil {
			rec.Error = fmt.Sprintf("version: %v", err)
		}
		rec.Version = v
		models, hint, err := h.Catalog()
		if err != nil && rec.Error == "" {
			rec.Error = err.Error()
		}
		rec.Models = models
		rec.LoginHint = hint
		c.Harness[name] = rec
	}
	return c, c.Save()
}

// Save writes the cache file.
func (c *Cache) Save() error {
	if err := config.EnsureHome(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(config.DiscoveryFile(), b, 0o644)
}

// Model returns the catalog entry for a Harness model id.
func (c *Cache) Model(harnessName, id string) (harness.Model, bool) {
	rec, ok := c.Harness[harnessName]
	if !ok {
		return harness.Model{}, false
	}
	for _, m := range rec.Models {
		if m.ID == id {
			return m, true
		}
	}
	return harness.Model{}, false
}

// Resolve returns the first family model present in the catalog.
func (c *Cache) Resolve(harnessName string, models []string) (string, bool) {
	for _, m := range models {
		if _, ok := c.Model(harnessName, m); ok {
			return m, true
		}
	}
	return "", false
}
