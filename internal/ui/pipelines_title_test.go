package ui

import (
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

func TestBoxThreeTitleFollowsHostKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind forge.Kind
		term string
		want string
		not  string
	}{
		{"gitlab", forge.KindGitLab, "MR", "[3] Pipelines", "[3] Actions"},
		{"forgejo", forge.KindForgejo, "PR", "[3] Actions", "[3] Pipelines"},
		{"gitea", forge.KindGitea, "PR", "[3] Actions", "[3] Pipelines"},
		{"github", forge.KindGitHub, "PR", "[3] Actions", "[3] Pipelines"},
	} {
		f := forgetest.NewFake(forge.HostInfo{Kind: tc.kind, URL: "https://h.test", ChangeRequestTerm: tc.term, User: "you"})
		f.AddRepo(domain.Repo{RepoRef: homelab, WebURL: "https://h.test/home/homelab", Access: domain.AccessWrite})
		m := press(t, newModel(t, f), "j")
		view := strings.Join(lines(m), "\n")
		if !strings.Contains(view, tc.want) || strings.Contains(view, tc.not) {
			t.Errorf("%s: box [3] title: want %q and not %q in view:\n%s", tc.name, tc.want, tc.not, view)
		}
	}
}
