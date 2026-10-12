package ui

import (
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

func TestBoxThreeTitleFollowsTheHostsRunsTerm(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind forge.Kind
		term string
		runs string
		want string
		not  string
	}{
		{"gitlab", forge.KindGitLab, "MR", "Pipelines", "[3] Pipelines", "[3] Actions"},
		{"forgejo", forge.KindForgejo, "PR", "Actions", "[3] Actions", "[3] Pipelines"},
		{"gitea", forge.KindGitea, "PR", "Actions", "[3] Actions", "[3] Pipelines"},
		{"github", forge.KindGitHub, "PR", "Actions", "[3] Actions", "[3] Pipelines"},
	} {
		f := forgetest.NewFake(forge.HostInfo{Kind: tc.kind, URL: "https://h.test", ChangeRequestTerm: tc.term, RunsTerm: tc.runs, User: "you"})
		f.AddRepo(domain.Repo{RepoRef: homelab, WebURL: "https://h.test/home/homelab", Access: domain.AccessWrite})
		m := press(t, newModel(t, f), "j")
		view := strings.Join(lines(m), "\n")
		if !strings.Contains(view, tc.want) || strings.Contains(view, tc.not) {
			t.Errorf("%s: box [3] title: want %q and not %q in view:\n%s", tc.name, tc.want, tc.not, view)
		}
	}
}

func TestBoxTitleReadsRunsTermNotKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		info forge.HostInfo
		want string
	}{
		{"pipelines term", forge.HostInfo{RunsTerm: "Pipelines"}, "Pipelines"},
		{"actions term", forge.HostInfo{RunsTerm: "Actions"}, "Actions"},
		{"gitlab kind with no term falls back", forge.HostInfo{Kind: forge.KindGitLab}, "Actions"},
		{"empty info falls back", forge.HostInfo{}, "Actions"},
		{"github kind with a pipelines term", forge.HostInfo{Kind: forge.KindGitHub, RunsTerm: "Pipelines"}, "Pipelines"},
	} {
		if got := boxTitle(boxRuns, tc.info); got != tc.want {
			t.Errorf("%s: boxTitle(boxRuns, %+v) = %q, want %q", tc.name, tc.info, got, tc.want)
		}
	}
}
