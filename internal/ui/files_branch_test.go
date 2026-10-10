package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// refFile seeds body at p on ref and returns its listing entry.
func refFile(f *forgetest.Fake, ref, p, body string) domain.TreeEntry {
	f.SetFileAt(homelab, ref, p, []byte(body))
	return fxEntry(p, domain.EntryFile, int64(len(body)))
}

// branchFixture is the fxFixture homelab with three branches. main is the default, and its root is fxFixture's.
// feature/x has a root (cmd/ and new.txt) and a cmd/ of its own, and a&b#c/d has one file.
func branchFixture(t *testing.T) *forgetest.Fake {
	t.Helper()
	f := fxFixture(t)
	f.AddBranch(homelab, domain.Branch{
		Name: "main", Default: true, WebURL: "https://f.test/home/homelab/src/branch/main",
		Commit: branchOn("1111111111111111111111111111111111111111", "Fix the build", "Ada", branchDay0),
	})
	f.AddBranch(homelab, domain.Branch{
		Name: "feature/x", WebURL: "https://f.test/home/homelab/src/branch/feature/x",
		Commit: branchOn("2222222222222222222222222222222222222222", "Add the x box", "Grace", branchDay0.AddDate(0, 0, 5)),
	})
	f.AddBranch(homelab, domain.Branch{
		Name: "a&b#c/d", WebURL: "https://f.test/home/homelab/src/branch/a&b%23c/d",
		Commit: branchOn("3333333333333333333333333333333333333333", "Odd name", "Linus", branchDay0.AddDate(0, 0, 3)),
	})
	f.SetTreeAt(homelab, "feature/x", "", []domain.TreeEntry{fxEntry("cmd", domain.EntryDir, 0), refFile(f, "feature/x", "new.txt", "fresh\n")})
	f.SetTreeAt(homelab, "feature/x", "cmd", []domain.TreeEntry{refFile(f, "feature/x", "cmd/feature.go", "package feature\n")})
	f.SetTreeAt(homelab, "a&b#c/d", "", []domain.TreeEntry{refFile(f, "a&b#c/d", "odd.txt", "odd\n")})
	return f
}

func TestBranchEnterShowsItsRootOnFiles(t *testing.T) {
	m := press(t, openBranchesTab(t, branchFixture(t)), "j", "enter")
	v := screen(m)
	if !strings.Contains(v, "› feature/x:") || !strings.Contains(v, "new.txt") {
		t.Fatalf("enter on feature/x: want its root on the Files tab under the crumb:\n%s", v)
	}
	for _, gone := range []string{"docs/", "many/", "void/", "vendor", "notes.txt", "big.iso"} {
		if strings.Contains(v, gone) {
			t.Errorf("feature/x root shows the default branch's %q:\n%s", gone, v)
		}
	}
}

func TestBranchLKeyMatchesEnter(t *testing.T) {
	for _, key := range []string{"enter", "l"} {
		t.Run(key, func(t *testing.T) {
			m := press(t, openBranchesTab(t, branchFixture(t)), "j", key)
			if v := screen(m); !strings.Contains(v, "› feature/x:") || !strings.Contains(v, "new.txt") {
				t.Errorf("%s on feature/x: want its root on the Files tab:\n%s", key, v)
			}
		})
	}
}

// Moving the cursor starts loading the entry under it. The test drops that load so src/ is entered before its listing arrives.
func TestBrowsedBranchCrumbFollowsDirectories(t *testing.T) {
	f := branchFixture(t)
	f.SetTreeAt(homelab, "feature/x", "", []domain.TreeEntry{fxEntry("lib", domain.EntryDir, 0), fxEntry("src", domain.EntryDir, 0), refFile(f, "feature/x", "new.txt", "fresh\n")})
	f.SetTreeAt(homelab, "feature/x", "lib", []domain.TreeEntry{refFile(f, "feature/x", "lib/y.go", "package y\n")})
	f.SetTreeAt(homelab, "feature/x", "src", []domain.TreeEntry{refFile(f, "feature/x", "src/x.go", "package x\n")})
	m := press(t, openBranchesTab(t, f), "j", "enter")
	if v := screen(m); !strings.Contains(v, "› feature/x:") || strings.Contains(v, "feature/x:src") {
		t.Fatalf("root of feature/x: want the crumb ending in feature/x:\n%s", v)
	}
	m, _ = step(t, m, "j")
	m = press(t, m, "l")
	if v := screen(m); !strings.Contains(v, "› feature/x:src") || !strings.Contains(v, "x.go") {
		t.Errorf("l on src/: want the crumb feature/x:src and its entries from feature/x:\n%s", v)
	}
}

func TestBrowsedBranchPreviewsItsOwnFile(t *testing.T) {
	f := branchFixture(t)
	f.SetTreeAt(homelab, "feature/x", "", []domain.TreeEntry{refFile(f, "feature/x", "new.txt", "fresh\n")})
	m := press(t, openBranchesTab(t, f), "j", "enter")
	if v := screen(m); !strings.Contains(v, "fresh") {
		t.Errorf("enter on feature/x: want the preview of new.txt from feature/x:\n%s", v)
	}
}

func TestDefaultBranchCrumbOnOpenAndAfterPick(t *testing.T) {
	tests := []struct {
		name string
		open func(t *testing.T) Model
	}{
		{"first open", func(t *testing.T) Model { return fxOn(t, branchFixture(t)) }},
		{"picked main row", func(t *testing.T) Model { return press(t, openBranchesTab(t, branchFixture(t)), "enter") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.open(t)
			if v := screen(m); !strings.Contains(v, "› main:") {
				t.Fatalf("root: want the crumb ending in main::\n%s", v)
			}
			m = press(t, m, "l")
			if v := screen(m); !strings.Contains(v, "› main:cmd") {
				t.Errorf("l on cmd/: want the crumb main:cmd:\n%s", v)
			}
		})
	}
}

func TestFirstOpenListsDefaultBranchRoot(t *testing.T) {
	v := screen(fxOn(t, branchFixture(t)))
	if !strings.Contains(v, "docs/") || !strings.Contains(v, "› main:") {
		t.Errorf("first open: want the default branch's root under its crumb:\n%s", v)
	}
	if strings.Contains(v, "new.txt") {
		t.Errorf("first open lists the feature/x-only entry new.txt:\n%s", v)
	}
}

func TestPickingAgainStartsAtRootWithFirstEntry(t *testing.T) {
	tests := []struct {
		name, crumb, list string
		keys              []string
	}{
		{"same branch", "› feature/x:", "new.txt", nil},
		{"other branch", "› a&b#c/d:", "odd.txt", []string{"j"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := press(t, openBranchesTab(t, branchFixture(t)), "j", "enter", "l", "]")
			m = press(t, m, append(tt.keys, "enter")...)
			v := screen(m)
			if !strings.Contains(v, tt.crumb) || strings.Contains(v, tt.crumb+"cmd") || !strings.Contains(v, tt.list) {
				t.Errorf("picked again: want %s at the root listing %s:\n%s", tt.crumb, tt.list, v)
			}
			if m.details.filesDir != "" || m.details.filesCur != 0 || m.details.filesFocus {
				t.Errorf("picked again: filesDir %q, filesCur %d, filesFocus %v; want the root, cursor 0 and list focus",
					m.details.filesDir, m.details.filesCur, m.details.filesFocus)
			}
		})
	}
}

func TestBrowsedBranchSurvivesTabsBoxesAndRefresh(t *testing.T) {
	m := press(t, openBranchesTab(t, branchFixture(t)), "j", "enter", "j")
	steps := []struct {
		name string
		keys []string
	}{
		{"switching tabs and back", []string{"]", "["}},
		{"switching boxes and back to [6]", []string{"h", "5", "6", "l", "]"}},
		{"stepping out to the boxes and back in", []string{"h", "l"}},
		{"refresh", []string{"r"}},
	}
	for _, s := range steps {
		m = press(t, m, s.keys...)
		if v := screen(m); !strings.Contains(v, "› feature/x:") || !strings.Contains(v, "new.txt") {
			t.Errorf("after %s: want feature/x's root under its crumb:\n%s", s.name, v)
		}
	}
}

func TestRepoSwitchResetsBrowsedBranch(t *testing.T) {
	f := branchFixture(t)
	f.AddRepo(domain.Repo{RepoRef: fxInfra, WebURL: "https://f.test/home/infra", Access: domain.AccessWrite})
	m := press(t, openBranchesTab(t, f), "j", "enter")
	m = press(t, m, "h", "h", "j")
	if m.details.filesRef != "" || m.details.filesDir != "" {
		t.Errorf("selecting infra: filesRef %q, filesDir %q; want the default branch at the root", m.details.filesRef, m.details.filesDir)
	}
	m = press(t, m, "k", "6", "l", "]")
	if v := screen(m); !strings.Contains(v, "› main:") || strings.Contains(v, "new.txt") {
		t.Errorf("back on homelab: want the default branch's root under its crumb:\n%s", v)
	}
}

func TestRepickingBranchRefetchesCachedRoot(t *testing.T) {
	f := branchFixture(t)
	m := press(t, openBranchesTab(t, f), "j", "enter")
	f.SetTreeAt(homelab, "feature/x", "", []domain.TreeEntry{refFile(f, "feature/x", "pushed.txt", "later\n")})
	m = press(t, m, "]", "enter")
	if v := screen(m); !strings.Contains(v, "pushed.txt") {
		t.Errorf("re-picking feature/x: want the refetched root with pushed.txt:\n%s", v)
	}
}

func TestOpenUsesEntryOnBrowsedBranch(t *testing.T) {
	const mainURL = "https://f.test/home/homelab/src/branch/main/notes.txt"
	const featureURL = "https://f.test/home/homelab/src/branch/feature/x/notes.txt"
	f := homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme})
	f.AddBranch(homelab, domain.Branch{Name: "main", Default: true, WebURL: "https://f.test/home/homelab/src/branch/main"})
	f.AddBranch(homelab, domain.Branch{Name: "feature/x", WebURL: "https://f.test/home/homelab/src/branch/feature/x"})
	f.SetTree(homelab, "", []domain.TreeEntry{{Name: "notes.txt", Path: "notes.txt", Type: domain.EntryFile, Size: 5, WebURL: mainURL}})
	f.SetFile(homelab, "notes.txt", []byte("main\n"))
	f.SetTreeAt(homelab, "feature/x", "", []domain.TreeEntry{{Name: "notes.txt", Path: "notes.txt", Type: domain.EntryFile, Size: 8, WebURL: featureURL}})
	f.SetFileAt(homelab, "feature/x", "notes.txt", []byte("feature\n"))

	tests := []struct {
		name string
		keys []string
		want string
	}{
		{"default branch", []string{"enter"}, mainURL},
		{"feature/x", []string{"j", "enter"}, featureURL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := press(t, openBranchesTab(t, f), tt.keys...)
			var opened []string
			m.openURL = func(u string) error {
				opened = append(opened, u)
				return nil
			}
			press(t, m, "o")
			if !slices.Equal(opened, []string{tt.want}) {
				t.Errorf("o opened %v, want [%s]", opened, tt.want)
			}
		})
	}
}

func TestStaleBranchResultsChangeNothing(t *testing.T) {
	m := press(t, openBranchesTab(t, branchFixture(t)), "j", "enter", "]", "j", "enter")
	if v := screen(m); !strings.Contains(v, "› a&b#c/d:") || !strings.Contains(v, "odd") {
		t.Fatalf("before the stale results: want a&b#c/d's root with odd.txt previewed:\n%s", v)
	}
	before := m.status
	m = run(t, m, treeLoadedMsg{
		key:     core.Key{Kind: core.KindTree, Repo: homelab, Ref: "feature/x"},
		entries: []domain.TreeEntry{fxEntry("stale.txt", domain.EntryFile, 1)},
	})
	m = run(t, m, treeLoadedMsg{
		key: core.Key{Kind: core.KindTree, Repo: homelab, Ref: "feature/x"},
		err: errors.New("old branch exploded"),
	})
	m = run(t, m, previewLoadedMsg{
		key:     core.Key{Kind: core.KindPreview, Repo: homelab, Ref: "feature/x", Path: "odd.txt"},
		preview: domain.FilePreview{Text: "STALE\n"},
	})
	v := screen(m)
	for _, bad := range []string{"STALE", "stale.txt", "couldn't load"} {
		if strings.Contains(v, bad) {
			t.Errorf("a result for feature/x changed a&b#c/d's root: found %q:\n%s", bad, v)
		}
	}
	if m.status != before {
		t.Errorf("status %q after stale results, want %q", m.status, before)
	}
}

func TestCrumbIsPathOnlyWithoutBranchList(t *testing.T) {
	m := fxOn(t, fxFixture(t))
	if v := screen(m); strings.Contains(v, "Repo ›") {
		t.Errorf("root with no branch list: want the path alone, no branch segment:\n%s", v)
	}
	m = press(t, m, "l")
	if v := screen(m); !strings.Contains(v, "[6] Repo › cmd") || strings.Contains(v, ":cmd") {
		t.Errorf("inside cmd/ with no branch list: want the crumb [6] Repo › cmd with no branch segment:\n%s", v)
	}
}

func TestBranchNameIsSanitizedInCrumb(t *testing.T) {
	f := branchFixture(t)
	f.AddBranch(homelab, domain.Branch{
		Name: "bad\x1bname", WebURL: "https://f.test/home/homelab/src/branch/bad",
		Commit: branchOn("5555555555555555555555555555555555555555", "Escape", "Ken", branchDay0.AddDate(0, 0, -10)),
	})
	f.SetTreeAt(homelab, "bad\x1bname", "", []domain.TreeEntry{refFile(f, "bad\x1bname", "esc.txt", "esc\n")})
	m := press(t, openBranchesTab(t, f), "G", "enter")
	if v := screen(m); !strings.Contains(v, "› bad�name:") {
		t.Errorf("crumb: want the branch name with the ESC shown as �:\n%s", v)
	}
	if raw := m.View().Content; strings.Contains(raw, "bad\x1bname") {
		t.Errorf("a raw ESC from the branch name reached View()")
	}
}
