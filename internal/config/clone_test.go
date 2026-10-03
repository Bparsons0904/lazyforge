package config_test

import (
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
)

func cloneFixture() config.Config {
	on := true
	return config.Config{
		DefaultHost: "a",
		Update:      config.Update{Check: true},
		Hosts: map[string]config.Host{
			"a": {Type: "forgejo", URL: "https://a", Repos: map[string]config.RepoSettings{"o/r": {RequireGreenCI: &on}}},
			"b": {Type: "gitea", URL: "https://b"},
		},
	}
}

func TestCloneIsolatesOriginal(t *testing.T) {
	t.Run("hosts map", func(t *testing.T) {
		orig := cloneFixture()
		c := orig.Clone()
		delete(c.Hosts, "b")
		c.Hosts["z"] = config.Host{Type: "gitea"}
		h := c.Hosts["a"]
		h.URL = "https://changed"
		c.Hosts["a"] = h
		if len(orig.Hosts) != 2 || orig.Hosts["a"].URL != "https://a" || orig.Hosts["b"].URL != "https://b" {
			t.Fatalf("original changed: %+v", orig.Hosts)
		}
	})
	t.Run("repos map", func(t *testing.T) {
		orig := cloneFixture()
		c := orig.Clone()
		off := false
		c.Hosts["a"].Repos["o/r"] = config.RepoSettings{RequireGreenCI: &off}
		c.Hosts["a"].Repos["o/new"] = config.RepoSettings{RequireGreenCI: &off}
		delete(c.Hosts["a"].Repos, "o/r")
		got := orig.Hosts["a"].Repos
		if len(got) != 1 || got["o/r"].RequireGreenCI == nil || !*got["o/r"].RequireGreenCI {
			t.Fatalf("original repos changed: %+v", got)
		}
	})
	t.Run("adding repos to a host that had none", func(t *testing.T) {
		orig := cloneFixture()
		c := orig.Clone()
		on := true
		h := c.Hosts["b"]
		h.Repos = map[string]config.RepoSettings{"o/x": {RequireGreenCI: &on}}
		c.Hosts["b"] = h
		if len(orig.Hosts["b"].Repos) != 0 {
			t.Fatalf("original gained repos: %+v", orig.Hosts["b"].Repos)
		}
	})
	t.Run("scalars", func(t *testing.T) {
		orig := cloneFixture()
		c := orig.Clone()
		c.DefaultHost = "b"
		c.Update.Check = false
		if orig.DefaultHost != "a" || !orig.Update.Check {
			t.Fatalf("original scalars changed: %+v", orig)
		}
	})
	t.Run("clone equals original", func(t *testing.T) {
		orig := cloneFixture()
		c := orig.Clone()
		if c.DefaultHost != orig.DefaultHost || len(c.Hosts) != 2 || *c.Hosts["a"].Repos["o/r"].RequireGreenCI != true {
			t.Fatalf("clone differs: %+v", c)
		}
	})
	t.Run("nil hosts", func(t *testing.T) {
		c := config.Config{}.Clone()
		if len(c.Hosts) != 0 {
			t.Fatalf("got %+v", c)
		}
	})
}
