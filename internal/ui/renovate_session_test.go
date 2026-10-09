package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core/renovate"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// Seam: session wiring. The host's renovate_user reaches core, so author-detected PRs show up in ★.
func TestSessionPassesRenovateUserToCore(t *testing.T) {
	cfg := twoHosts()
	h := cfg.Hosts["a"]
	h.RenovateUser = "botty"
	cfg.Hosts["a"] = h
	a, _ := testApp(t, Deps{Config: cfg, Host: "a", Forge: newDemo()})
	if got := a.session.svc.RenovateUser(); got != "botty" {
		t.Errorf("session renovate user %q, want botty", got)
	}
	b, _ := testApp(t, Deps{Config: cfg, Host: "b", Forge: newDemo()})
	if got := b.session.svc.RenovateUser(); got != "" {
		t.Errorf("host without renovate_user got %q", got)
	}
}

// Seam: stamping. A ★ scan result from a replaced session never reaches the new one.
func TestStaleSessionScanResultDropped(t *testing.T) {
	a, _ := testApp(t, Deps{Config: twoHosts(), Host: "a", Forge: newDemo()})
	a = appRun(t, a, appExec(a.Init())...)
	oldGen := a.gen
	a = appPress(t, a, "h", "j", "l") // switch to b
	bogus := renovateScannedMsg{seq: a.session.star.seq, repo: homelab, scan: renovate.RepoScan{
		Repo: domain.Repo{RepoRef: homelab, Access: domain.AccessWrite},
		PRs:  []domain.ChangeRequest{{Number: 999, Title: "stale", SourceBranch: "renovate/stale"}},
	}}
	a = appRun(t, a, appExec(stamp(oldGen, func() tea.Msg { return bogus }))...)
	for _, mb := range a.session.star.view.PRs {
		if mb.CR.Number == 999 {
			t.Fatal("scan result from the replaced session reached the new one")
		}
	}
}
