package ui

import (
	"reflect"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func settingsFixture() settingsView {
	on := true
	return settingsView{
		cfg: config.Config{
			DefaultHost: "a",
			Update:      config.Update{Check: true},
			Hosts: map[string]config.Host{
				"a": {Type: "forgejo", URL: "https://a", Repos: map[string]config.RepoSettings{"x/old": {RequireGreenCI: &on}}},
				"b": {Type: "gitea", URL: "https://b", Repos: map[string]config.RepoSettings{"y/only": {RequireGreenCI: &on}}},
			},
		},
		active: "a",
		repos:  []domain.RepoRef{{Owner: "x", Name: "new"}},
	}
}

// sKeys sends keys to a fresh settings screen and returns the last non-none intent.
func sKeys(v settingsView, keys ...string) (settings, settingsIntent) {
	s, last := settings{}, settingsIntent{}
	for _, k := range keys {
		var in settingsIntent
		s, in = s.update(keyMsg(k), v, defaultKeys())
		if in.kind != intentNone {
			last = in
		}
	}
	return s, last
}

func TestSettingsRowsAndMarkers(t *testing.T) {
	v := settingsFixture()
	var got []string
	for _, r := range settingsRows(v) {
		got = append(got, strings.Join([]string{r.host, r.repo}, "|"))
	}
	want := []string{"a|", "b|", "|", "|", "|", "|", "a|", "a|x/new", "a|x/old", "b|", "b|y/only"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	out := (settings{}).view(80, 30, v)
	for _, s := range []string{"(default)", "(connected)", "Check for updates at startup", "Show the splash screen at startup"} {
		if !strings.Contains(out, s) {
			t.Errorf("view lacks %q:\n%s", s, out)
		}
	}
}

func TestSettingsToggles(t *testing.T) {
	v := settingsFixture()
	before := v.cfg.Clone()

	_, in := sKeys(v, "j", "j", "j", "space") // update row
	if in.kind != intentChange || in.cfg.Update.Check {
		t.Fatalf("update toggle: %+v", in)
	}
	_, in = sKeys(v, "j", "j", "j", "j", "enter") // splash row
	if in.kind != intentChange || !in.cfg.Splash.Show {
		t.Fatalf("splash toggle: %+v", in)
	}
	_, in = sKeys(v, "j", "j", "j", "j", "j", "j", "enter") // host a merging
	if !in.cfg.Hosts["a"].RequireGreenCI {
		t.Fatalf("host merge toggle: %+v", in.cfg.Hosts["a"])
	}

	// repo x/new cycles nil -> true -> false -> nil, and nil removes the key.
	c := v.cfg
	var seen []*bool
	for range 3 {
		c = cycleRepoCI(c, "a", "x/new")
		seen = append(seen, c.Hosts["a"].Repos["x/new"].RequireGreenCI)
	}
	if seen[0] == nil || !*seen[0] || seen[1] == nil || *seen[1] || seen[2] != nil {
		t.Fatalf("cycle = %v", seen)
	}
	if _, ok := c.Hosts["a"].Repos["x/new"]; ok {
		t.Fatal("repo key left behind at inherit")
	}
	if !reflect.DeepEqual(v.cfg, before) {
		t.Fatal("input config mutated")
	}
}

func TestSettingsImagesRow(t *testing.T) {
	v := settingsFixture()
	v.cfg.Images.Show = true
	out := (settings{}).view(80, 30, v)
	lines := strings.Split(out, "\n")
	at := func(s string) int {
		for i, l := range lines {
			if strings.Contains(strip(l), s) {
				return i
			}
		}
		t.Fatalf("view lacks %q:\n%s", s, out)
		return -1
	}
	splash, images, merging := at("Splash screen"), at("Show images"), at("Merging")
	if splash >= images || images >= merging {
		t.Fatalf("Show images at line %d, want between Splash screen (%d) and Merging (%d)", images, splash, merging)
	}
	if !strings.Contains(strip(lines[images]), "on") || strings.Contains(strip(lines[images]), "off") {
		t.Fatalf("images row reads %q with the setting on", strip(lines[images]))
	}
	if !strings.Contains(out, "Images") {
		t.Fatalf("view lacks the Images section:\n%s", out)
	}

	for _, on := range []bool{false, true} {
		v.cfg.Images.Show = on
		_, in := sKeys(v, "j", "j", "j", "j", "j", "space") // images row
		if in.kind != intentChange || in.cfg.Images.Show == on {
			t.Fatalf("images toggle from %v: %+v", on, in)
		}
	}
}

func TestSettingsHostMenu(t *testing.T) {
	v := settingsFixture()
	before := v.cfg.Clone()

	_, in := sKeys(v, "l", "j", "enter") // default is a: no change
	if in.kind != intentNone {
		t.Fatalf("set default on default: %+v", in)
	}
	_, in = sKeys(v, "j", "l", "j", "enter")
	if in.kind != intentChange || in.cfg.DefaultHost != "b" {
		t.Fatalf("set default: %+v", in)
	}
	_, in = sKeys(v, "j", "l", "j", "j", "enter", "enter") // remove b, confirm
	if _, ok := in.cfg.Hosts["b"]; in.kind != intentChange || ok {
		t.Fatalf("remove: %+v", in)
	}
	_, in = sKeys(v, "j", "l", "j", "j", "enter", "esc")
	if in.kind != intentNone {
		t.Fatalf("esc at confirm: %+v", in)
	}
	s, in := sKeys(v, "l", "j", "j", "enter") // active host a
	if in.kind != intentNone || !strings.Contains(s.msg, "Switch to another host first") {
		t.Fatalf("active removal: %+v %q", in, s.msg)
	}
	_, in = sKeys(v, "l", "enter")
	if in.kind != intentEdit || in.host != "a" {
		t.Fatalf("edit: %+v", in)
	}
	_, in = sKeys(v, "j", "j", "enter")
	if in.kind != intentAdd {
		t.Fatalf("add: %+v", in)
	}
	if _, in = sKeys(v, "esc"); in.kind != intentClose {
		t.Fatalf("esc: %+v", in)
	}

	d := removeHost(v.cfg, "a")
	if d.DefaultHost != "" || len(d.Hosts) != 1 {
		t.Fatalf("removing default host: %+v", d)
	}
	if !reflect.DeepEqual(v.cfg, before) {
		t.Fatal("input config mutated")
	}
}

func TestSettingsRemoveRefusedWhenHostBecomesActive(t *testing.T) {
	v := settingsFixture()
	v.active = ""
	s := settings{}
	var in settingsIntent
	for _, k := range []string{"j", "l", "j", "j", "enter"} { // host b, Remove, confirm prompt open
		s, in = s.update(keyMsg(k), v, defaultKeys())
	}
	if s.mode != modeConfirm || in.kind != intentNone {
		t.Fatalf("mode %v intent %+v", s.mode, in)
	}
	v.active = "b"
	s, in = s.update(keyMsg("y"), v, defaultKeys())
	if in.kind != intentNone || !strings.Contains(s.msg, "Switch to another host first") {
		t.Fatalf("intent %+v msg %q", in, s.msg)
	}
}
