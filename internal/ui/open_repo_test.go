package ui

import (
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func TestOpenRepoPage(t *testing.T) {
	f := rvFake("PR", rvRepo{ref: repoA, access: domain.AccessWrite, prs: []domain.ChangeRequest{rvPostgres(1, domain.CIPass)}})
	for _, tc := range []struct {
		name, keys, want string
	}{
		{"repo list", "j o", "https://f.test/o/a"},
		{"Repo box", "j l 6 o", "https://f.test/o/a"},
		{"★ By repo row", "l o", "https://f.test/o/a"},
		{"★ PR row still opens the PR", "l 3 o", "https://f.test/pulls/1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var opened []string
			m := rvSized(t, f, core.Options{}, 120, 40)
			m.openURL = func(u string) error {
				opened = append(opened, u)
				return nil
			}
			press(t, m, strings.Fields(tc.keys)...)
			if strings.Join(opened, " ") != tc.want {
				t.Errorf("opened %v, want %s", opened, tc.want)
			}
		})
	}
}

func TestRepoListHintsOpenOnlyOnARepo(t *testing.T) {
	m := rvSized(t, rvFake("PR", rvRepo{ref: repoA, access: domain.AccessWrite}), core.Options{}, 120, 40)
	if h := strip(m.statusBar()); strings.Contains(h, "open") {
		t.Errorf("★ row hints open: %s", h)
	}
	if _, cmd := update(m, keyMsg("o")); cmd != nil {
		t.Error("o on the ★ row produced a command")
	}
	if h := strip(press(t, m, "j").statusBar()); !strings.Contains(h, "o open") {
		t.Errorf("repo row doesn't hint open: %s", h)
	}
}
