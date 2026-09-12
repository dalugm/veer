// Package settings persists profile references and Veer preferences.
package settings

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/dalugm/veer/engine"
)

// Profile names an existing native configuration without copying it.
type Profile struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Engine string `json:"engine"`
	Path   string `json:"path"`
}

// Config contains persisted profiles and application preferences.
type Config struct {
	CoreUpdateChannel string    `json:"core_update_channel,omitempty"`
	CoreBackupPath    string    `json:"core_backup_path,omitempty"`
	CoreBackupFor     string    `json:"core_backup_for,omitempty"`
	CoreBackupTarget  string    `json:"core_backup_target,omitempty"`
	DNSMode           string    `json:"dns_mode,omitempty"`
	DNS               string    `json:"dns,omitempty"`
	NetworkService    string    `json:"network_service,omitempty"`
	Version           int       `json:"version"`
	EnginePath        string    `json:"engine_path"`
	GeoDir            string    `json:"geo_dir,omitempty"`
	Selected          string    `json:"selected,omitempty"`
	Profiles          []Profile `json:"profiles"`
}

// Defaults returns the initial preferences for a new installation.
func Defaults() Config {
	return Config{
		CoreUpdateChannel: "stable",
		DNSMode:           "auto",
		Version:           1,
		EnginePath:        "xray",
		Profiles:          []Profile{},
	}
}

// DefaultPath resolves the settings file using the environment and OS config directory.
func DefaultPath() (string, error) {
	if dir := os.Getenv("VEER_CONFIG_DIR"); dir != "" {
		dir, err := filepath.Abs(ExpandPath(dir))
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "settings.json"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "veer", "settings.json"), nil
}

// Load reads settings or returns defaults when the file does not exist.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Config{}, err
	}
	c := Defaults()
	c.DNSMode = ""
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, fmt.Errorf("read settings (original file retained): %w", err)
	}
	if strings.TrimSpace(string(data)) == "null" || c.Version != 1 {
		return Config{}, errors.New("unsupported Veer settings version")
	}
	if c.DNSMode == "" {
		c.DNSMode = "auto"
		if strings.TrimSpace(c.DNS) != "" {
			c.DNSMode = "custom"
		}
	}
	if c.EnginePath == "" {
		c.EnginePath = "xray"
	}
	if c.CoreUpdateChannel != "preview" {
		c.CoreUpdateChannel = "stable"
	}
	ids := map[string]bool{}
	for _, p := range c.Profiles {
		if p.ID == "" || ids[p.ID] || p.Name == "" || !filepath.IsAbs(p.Path) {
			return Config{}, errors.New("invalid profile in settings")
		}
		ids[p.ID] = true
	}
	if c.Selected != "" && !ids[c.Selected] {
		return Config{}, errors.New("selected profile does not exist")
	}
	return c, nil
}

// Save writes settings using a temporary file in the destination directory.
func Save(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".veer-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// ExpandPath expands a leading home-directory marker in a path.
func ExpandPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimLeft(path[1:], `/\`))
		}
	}
	return path
}

// AddProfile adds a named absolute configuration path and rejects duplicates.
func (c *Config) AddProfile(name, path string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("enter a profile name")
	}
	path, err := filepath.Abs(ExpandPath(path))
	if err != nil {
		return err
	}
	if _, err = engine.Inspect(path); err != nil {
		return err
	}
	for _, p := range c.Profiles {
		if p.Path == path {
			return errors.New("this config is already in Profiles")
		}
	}
	var id [12]byte
	if _, err = rand.Read(id[:]); err != nil {
		return err
	}
	p := Profile{ID: hex.EncodeToString(id[:]), Name: name, Engine: "xray", Path: path}
	c.Profiles = append(c.Profiles, p)
	if c.Selected == "" {
		c.Selected = p.ID
	}
	return nil
}

// Active looks up the currently selected profile.
func (c Config) Active() (Profile, bool) {
	for _, p := range c.Profiles {
		if p.ID == c.Selected {
			return p, true
		}
	}
	return Profile{}, false
}

// DNSServers resolves the DNS mode into the requested system override.
func (c Config) DNSServers() ([]string, error) {
	mode := c.DNSMode
	if mode == "" {
		mode = "auto"
		if strings.TrimSpace(c.DNS) != "" {
			mode = "custom"
		}
	}
	switch mode {
	case "auto":
		return []string{"1.1.1.1", "8.8.8.8"}, nil
	case "off":
		return nil, nil
	case "custom":
		servers := strings.FieldsFunc(
			c.DNS,
			func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' },
		)
		if len(servers) == 0 {
			return nil, errors.New("enter at least one custom DNS server")
		}
		for _, server := range servers {
			if net.ParseIP(server) == nil {
				return nil, fmt.Errorf("invalid DNS server %q", server)
			}
		}
		return servers, nil
	default:
		return nil, fmt.Errorf("invalid DNS mode %q", mode)
	}
}
