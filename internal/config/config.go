// Package config reads and writes lazyforge's config and state files.
// It is a leaf package: it imports nothing else from internal/.
package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the settings file the app manages (ADR 0004).
type Config struct {
	DefaultHost string          `toml:"default_host,omitempty"`
	Update      Update          `toml:"update"`
	Hosts       map[string]Host `toml:"hosts"`
}

// Update holds self-update settings.
type Update struct {
	Check bool `toml:"check"` // defaults to true when absent from the file
}

// Host is one configured forge. Type uses the same strings as forge.Kind,
// but config must not import forge.
type Host struct {
	Type           string                  `toml:"type"` // "forgejo" | "gitea" | "github" | "gitlab"
	URL            string                  `toml:"url,omitempty"`
	TokenCmd       string                  `toml:"token_cmd,omitempty"`
	Token          string                  `toml:"token,omitempty"`
	RenovateUser   string                  `toml:"renovate_user,omitempty"`
	RequireGreenCI bool                    `toml:"require_green_ci,omitempty"`
	Repos          map[string]RepoSettings `toml:"repos,omitempty"` // key "owner/name"
}

// RepoSettings overrides host settings for one repo.
type RepoSettings struct {
	RequireGreenCI *bool `toml:"require_green_ci,omitempty"` // nil = inherit host
}

// RequiresGreenCI reports whether merges in repo ("owner/name") must wait for
// green CI: the repo override when set, else the host setting.
func (h Host) RequiresGreenCI(repo string) bool {
	if r := h.Repos[repo]; r.RequireGreenCI != nil {
		return *r.RequireGreenCI
	}
	return h.RequireGreenCI
}

// Clone deep-copies Hosts and every host's Repos, so edits to the clone never reach c.
// RepoSettings pointers stay shared: they are replaced, never written through.
func (c Config) Clone() Config {
	c.Hosts = maps.Clone(c.Hosts)
	for n, h := range c.Hosts {
		h.Repos = maps.Clone(h.Repos)
		c.Hosts[n] = h
	}
	return c
}

// Load reads and validates the config at path. A missing file yields an error
// satisfying errors.Is(err, fs.ErrNotExist), which the caller treats as "start
// onboarding".
func Load(path string) (Config, error) {
	c := Config{Update: Update{Check: true}}
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return Config{}, fmt.Errorf("load config %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	if c.hasPastedToken() {
		fi, err := os.Stat(path)
		if err != nil {
			return Config{}, fmt.Errorf("load config %s: %w", path, err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return Config{}, fmt.Errorf("config %s holds a token and must be mode 0600, found %04o", path, fi.Mode().Perm())
		}
	}
	return c, nil
}

// Save writes c to path atomically with mode 0600.
func Save(path string, c Config) error {
	return writeTOML(path, c)
}

func (c Config) hasPastedToken() bool {
	for _, h := range c.Hosts {
		if h.Token != "" {
			return true
		}
	}
	return false
}

var hostTypes = map[string]bool{"forgejo": true, "gitea": true, "github": true, "gitlab": true}

// Validate checks the config and returns every problem, each naming its field
// path (for example "hosts.homelab.url").
func (c Config) Validate() error {
	var errs []error
	if c.DefaultHost != "" {
		if _, ok := c.Hosts[c.DefaultHost]; !ok {
			errs = append(errs, fmt.Errorf("default_host: %q is not a configured host", c.DefaultHost))
		}
	}
	for _, name := range sortedHosts(c) {
		h := c.Hosts[name]
		p := "hosts." + name
		if !hostTypes[h.Type] {
			errs = append(errs, fmt.Errorf("%s.type: %q must be one of forgejo, gitea, github, gitlab", p, h.Type))
		}
		if h.URL == "" && (h.Type == "forgejo" || h.Type == "gitea") {
			errs = append(errs, fmt.Errorf("%s.url: required for type %q", p, h.Type))
		}
		if (h.Token == "") == (h.TokenCmd == "") {
			errs = append(errs, fmt.Errorf("%s.token: set exactly one of token or token_cmd", p))
		}
	}
	return errors.Join(errs...)
}

func sortedHosts(c Config) []string {
	names := make([]string, 0, len(c.Hosts))
	for n := range c.Hosts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ErrNeedPicker means no host could be chosen without asking the user.
var ErrNeedPicker = errors.New("config: no host selected")

// SelectHost picks the host name for the session: the --host flag, the only
// configured host, or default_host, in that order.
func SelectHost(c Config, flagHost string) (string, error) {
	if flagHost != "" {
		if _, ok := c.Hosts[flagHost]; !ok {
			return "", fmt.Errorf("unknown host %q (configured: %s)", flagHost, strings.Join(sortedHosts(c), ", "))
		}
		return flagHost, nil
	}
	if len(c.Hosts) == 1 {
		return sortedHosts(c)[0], nil
	}
	if c.DefaultHost != "" {
		return c.DefaultHost, nil
	}
	return "", ErrNeedPicker
}
