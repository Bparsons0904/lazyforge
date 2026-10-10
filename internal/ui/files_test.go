package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// fxOrder is the root listing in the order core returns it: directories first, then files by case-insensitive name.
var fxOrder = []string{"cmd", "docs", "many", "void", "big.iso", "blob.bin", "color.txt", "empty.txt", "latest", "long.txt", "notes.txt", "other.txt", "tabbed.txt", "vendor"}

const fxNotes = "# Notes\n\nhello\n"

// fxInfra is a second repo, so a repo change has somewhere to go.
var fxInfra = domain.RepoRef{Owner: "home", Name: "infra"}

// noFxForge hides the wrapped forge's TreeReader but keeps its README, as noBranchesForge does.
type noFxForge struct {
	forge.Forge
	rd forge.ReadmeReader
}

func (n noFxForge) GetReadme(ctx context.Context, r domain.RepoRef) (domain.Readme, error) {
	return n.rd.GetReadme(ctx, r)
}

// fxLines returns n lines that start with prefix and count up from 00, each newline-terminated.
func fxLines(prefix string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s %02d\n", prefix, i)
	}
	return b.String()
}

func fxEntry(p string, typ domain.EntryType, size int64) domain.TreeEntry {
	return domain.TreeEntry{
		Name:   p[strings.LastIndex(p, "/")+1:],
		Path:   p,
		Type:   typ,
		Size:   size,
		WebURL: "https://forge.test/homelab/" + p,
	}
}

// fxFile seeds body at p and returns its listing entry.
func fxFile(f *forgetest.Fake, p, body string) domain.TreeEntry {
	f.SetFile(homelab, p, []byte(body))
	return fxEntry(p, domain.EntryFile, int64(len(body)))
}

// fxFixture is a homelab with nested directories, one file per preview state, and a long file and a deep directory to scroll.
func fxFixture(t *testing.T) *forgetest.Fake {
	t.Helper()
	f := homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme})
	f.SetTree(homelab, "", []domain.TreeEntry{
		fxEntry("cmd", domain.EntryDir, 0),
		fxEntry("docs", domain.EntryDir, 0),
		fxEntry("many", domain.EntryDir, 0),
		fxEntry("void", domain.EntryDir, 0),
		fxEntry("big.iso", domain.EntryFile, int64(core.MaxPreviewSize)+1),
		fxFile(f, "blob.bin", "PK\x03\x00\x04"),
		fxFile(f, "color.txt", "\x1b[31mred\x1b[0m\n"),
		fxFile(f, "empty.txt", ""),
		fxEntry("latest", domain.EntrySymlink, 3),
		fxFile(f, "long.txt", fxLines("line", 60)),
		fxFile(f, "notes.txt", fxNotes),
		fxFile(f, "other.txt", fxLines("other", 60)),
		fxFile(f, "tabbed.txt", "a\tb\r\nc\r\n"),
		fxEntry("vendor", domain.EntrySubmodule, 0),
	})
	f.SetTree(homelab, "cmd", []domain.TreeEntry{
		fxEntry("cmd/api", domain.EntryDir, 0),
		fxEntry("cmd/ui", domain.EntryDir, 0),
		fxFile(f, "cmd/main.go", "package main\n"),
	})
	f.SetTree(homelab, "cmd/api", []domain.TreeEntry{fxFile(f, "cmd/api/server.go", "package api\n")})
	f.SetTree(homelab, "cmd/ui", []domain.TreeEntry{fxFile(f, "cmd/ui/tabs.go", "package ui\n")})
	f.SetTree(homelab, "docs", []domain.TreeEntry{fxFile(f, "docs/guide.md", "# Guide\n")})
	f.SetTree(homelab, "void", nil)
	many := make([]domain.TreeEntry, 0, 60)
	for i := 0; i < 60; i++ {
		p := fmt.Sprintf("many/f%02d.txt", i)
		many = append(many, fxFile(f, p, fmt.Sprintf("body f%02d.txt\n", i)))
	}
	f.SetTree(homelab, "many", many)
	return f
}

// fxOn returns the Files tab over f with the root listing loaded and the cursor on its first entry.
func fxOn(t *testing.T, f forge.Forge) Model {
	t.Helper()
	return press(t, repoBox(t, f), "l", "]")
}

// fxMoveTo moves the root cursor onto name by going to the top and stepping down.
func fxMoveTo(t *testing.T, m Model, name string) Model {
	t.Helper()
	i := slices.Index(fxOrder, name)
	if i < 0 {
		t.Fatalf("%q is not in the root listing", name)
	}
	keys := []string{"g", "g"}
	for k := 0; k < i; k++ {
		keys = append(keys, "j")
	}
	return press(t, m, keys...)
}

func TestFilesListShowsRootWithSuffixes(t *testing.T) {
	v := screen(fxOn(t, fxFixture(t)))
	for _, want := range []string{"cmd/", "docs/", "many/", "void/", "latest@", "notes.txt", "vendor"} {
		if !strings.Contains(v, want) {
			t.Errorf("list missing %q:\n%s", want, v)
		}
	}
	if strings.Index(v, "void/") > strings.Index(v, "big.iso") {
		t.Errorf("directories don't come before files:\n%s", v)
	}
}

func TestFilesPreviewFollowsCursor(t *testing.T) {
	m := fxOn(t, fxFixture(t))
	if v := screen(m); !strings.Contains(v, "api/") || !strings.Contains(v, "main.go") {
		t.Errorf("cursor on cmd/: preview should list its entries:\n%s", v)
	}
	m = fxMoveTo(t, m, "notes.txt")
	if v := screen(m); !strings.Contains(v, "# Notes") || !strings.Contains(v, "hello") {
		t.Errorf("cursor on notes.txt: preview missing its text:\n%s", v)
	}
}

func TestFilesPreviewStates(t *testing.T) {
	tests := []struct{ name, want string }{
		{"empty.txt", "— empty —"},
		{"blob.bin", "binary file"},
		{"big.iso", "too large to preview"},
		{"latest", "symlink"},
		{"vendor", "submodule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := fxMoveTo(t, fxOn(t, fxFixture(t)), tt.name)
			if v := screen(m); !strings.Contains(v, tt.want) {
				t.Errorf("preview of %s missing %q:\n%s", tt.name, tt.want, v)
			}
		})
	}
}

func TestFilesMoveIssuesOneLoadPerEntry(t *testing.T) {
	tests := []struct {
		name                 string
		tree, preview, total int
	}{
		{"docs", 1, 0, 1},
		{"many", 1, 0, 1},
		{"void", 1, 0, 1},
		{"big.iso", 0, 1, 1},
		{"blob.bin", 0, 1, 1},
		{"color.txt", 0, 1, 1},
		{"empty.txt", 0, 1, 1},
		{"latest", 0, 0, 0},
		{"long.txt", 0, 1, 1},
		{"notes.txt", 0, 1, 1},
		{"other.txt", 0, 1, 1},
		{"tabbed.txt", 0, 1, 1},
		{"vendor", 0, 0, 0},
	}
	m := fxOn(t, fxFixture(t))
	for _, tt := range tests {
		var msgs []tea.Msg
		m, msgs = step(t, m, "j")
		if c := counts(msgs); c["tree"] != tt.tree || c["preview"] != tt.preview || len(msgs) != tt.total {
			t.Errorf("j onto %s issued %v in %d messages, want tree %d, preview %d, total %d",
				tt.name, c, len(msgs), tt.tree, tt.preview, tt.total)
		}
	}
}

func TestFilesGAndGGReachTheEnds(t *testing.T) {
	m := press(t, fxOn(t, fxFixture(t)), "G")
	if v := screen(m); !strings.Contains(v, "submodule") {
		t.Errorf("G: preview of the last entry missing:\n%s", v)
	}
	m = press(t, m, "g", "g")
	if v := screen(m); !strings.Contains(v, "api/") {
		t.Errorf("gg: preview of the first entry missing:\n%s", v)
	}
}

func TestFilesHalfPageMoves(t *testing.T) {
	m := press(t, fxMoveTo(t, fxOn(t, fxFixture(t)), "many"), "l")
	if v := screen(m); !strings.Contains(v, "body f00.txt") {
		t.Fatalf("entering many/: preview of its first file missing:\n%s", v)
	}
	m = press(t, m, "ctrl+d")
	if v := screen(m); strings.Contains(v, "body f00.txt") || strings.Contains(v, "body f59.txt") {
		t.Errorf("ctrl+d: want the cursor half a page down, not on the first or last file:\n%s", v)
	}
	m = press(t, m, "ctrl+u")
	if v := screen(m); !strings.Contains(v, "body f00.txt") {
		t.Errorf("ctrl+u: want the cursor back on the first file:\n%s", v)
	}
}

func TestFilesEnterDirectoryAndCrumb(t *testing.T) {
	m := press(t, fxMoveTo(t, fxOn(t, fxFixture(t)), "cmd"), "l")
	if v := screen(m); !strings.Contains(v, "[6] Repo › cmd") || !strings.Contains(v, "server.go") {
		t.Fatalf("l on cmd/: want the crumb and the first entry's preview:\n%s", v)
	}
	m = press(t, m, "j", "l")
	if v := screen(m); !strings.Contains(v, "[6] Repo › cmd/ui") || !strings.Contains(v, "package ui") {
		t.Fatalf("l on cmd/ui/: want the crumb and tabs.go previewed:\n%s", v)
	}
	m = press(t, m, "h")
	if v := screen(m); !strings.Contains(v, "[6] Repo › cmd") || strings.Contains(v, "[6] Repo › cmd/ui") || !strings.Contains(v, "tabs.go") {
		t.Errorf("h from cmd/ui/: want the crumb back on cmd/ with the cursor on ui/:\n%s", v)
	}
	m = press(t, m, "h")
	if v := screen(m); strings.Contains(v, "[6] Repo › cmd") {
		t.Errorf("h from cmd/: want the crumb back to [6] Repo:\n%s", v)
	}
	m = press(t, m, "h")
	if v := screen(m); !strings.Contains(v, "BOXES") {
		t.Errorf("h at the root: want the BOXES level:\n%s", v)
	}
}

func TestFilesEnterKeyAlsoEntersDirectory(t *testing.T) {
	m := press(t, fxMoveTo(t, fxOn(t, fxFixture(t)), "cmd"), "enter")
	if v := screen(m); !strings.Contains(v, "[6] Repo › cmd") {
		t.Errorf("enter on cmd/: want the crumb [6] Repo › cmd:\n%s", v)
	}
}

func TestFilesEnterOnNonDirectoryDoesNotDescend(t *testing.T) {
	for _, name := range []string{"big.iso", "blob.bin", "empty.txt", "latest", "notes.txt", "vendor"} {
		t.Run(name, func(t *testing.T) {
			m := press(t, fxMoveTo(t, fxOn(t, fxFixture(t)), name), "l")
			if v := screen(m); strings.Contains(v, "Repo ›") {
				t.Errorf("l on %s: want the crumb to stay at the root:\n%s", name, v)
			}
		})
	}
}

func TestFilesEnterWhilePreviewLoadingChangesNothing(t *testing.T) {
	m := press(t, fxOn(t, fxFixture(t)), "g", "g")
	for i := 0; i < slices.Index(fxOrder, "notes.txt"); i++ {
		m, _ = update(m, keyMsg("j"))
	}
	if v := screen(m); !strings.Contains(v, "Loading…") {
		t.Fatalf("notes.txt: preview should still be loading:\n%s", v)
	}
	m = press(t, m, "l", "k")
	if v := screen(m); !strings.Contains(v, "line 00") {
		t.Errorf("l on a loading file then k: want the cursor on long.txt:\n%s", v)
	}
}

func TestFilesPreviewFocusScrollsAndClamps(t *testing.T) {
	m := press(t, fxMoveTo(t, fxOn(t, fxFixture(t)), "long.txt"), "l")
	if !strings.Contains(screen(m), "line 00") {
		t.Fatalf("l on long.txt: preview missing its first line:\n%s", screen(m))
	}
	m = press(t, m, "ctrl+d")
	if strings.Contains(screen(m), "line 00") {
		t.Errorf("ctrl+d: preview didn't scroll:\n%s", screen(m))
	}
	m = press(t, m, "ctrl+u")
	if !strings.Contains(screen(m), "line 00") {
		t.Errorf("ctrl+u: preview didn't scroll back to the top:\n%s", screen(m))
	}
	m = press(t, m, "j")
	if v := screen(m); !strings.Contains(v, "line 01") || strings.Contains(v, "line 00") {
		t.Errorf("j: want the preview scrolled one line:\n%s", v)
	}
	m = press(t, m, "k")
	if !strings.Contains(screen(m), "line 00") {
		t.Errorf("k: want the preview back on its first line:\n%s", screen(m))
	}
	m = press(t, m, "G")
	if v := screen(m); !strings.Contains(v, "line 59") || strings.Contains(v, "line 00") {
		t.Errorf("G: want the last line at the bottom of the column:\n%s", v)
	}
	rows := strings.Split(screen(m), "\n")
	last := slices.IndexFunc(rows, func(r string) bool { return strings.Contains(r, "line 59") })
	// The file ends in "\n"; that must not add a blank row under the last line, so the pane border follows it.
	if last < 0 || last+1 >= len(rows) || !strings.Contains(rows[last+1], "╰") {
		t.Errorf("G: want the pane border directly under line 59:\n%s", screen(m))
	}
	bottom := screen(m)
	m = press(t, m, "ctrl+d", "ctrl+d")
	if screen(m) != bottom {
		t.Errorf("ctrl+d past the end changed the view:\n%s", screen(m))
	}
	m = press(t, m, "g", "g")
	if !strings.Contains(screen(m), "line 00") {
		t.Errorf("gg: want the preview at the top:\n%s", screen(m))
	}
}

func TestFilesHGivesFocusBackToTheList(t *testing.T) {
	m := press(t, fxMoveTo(t, fxOn(t, fxFixture(t)), "long.txt"), "l", "h", "j")
	if v := screen(m); !strings.Contains(v, "# Notes") {
		t.Errorf("h then j: want the cursor on notes.txt:\n%s", v)
	}
}

func TestFilesMoveResetsPreviewOffset(t *testing.T) {
	m := press(t, fxMoveTo(t, fxOn(t, fxFixture(t)), "long.txt"), "l", "G", "h", "j", "j")
	if v := screen(m); !strings.Contains(v, "other 00") || strings.Contains(v, "other 59") {
		t.Errorf("other.txt: preview should start at its first line:\n%s", v)
	}
}

func TestFilesOpenCursorEntryURL(t *testing.T) {
	var opened []string
	m := fxMoveTo(t, fxOn(t, fxFixture(t)), "notes.txt")
	m.openURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	m = press(t, m, "o")
	if want := []string{"https://forge.test/homelab/notes.txt"}; !slices.Equal(opened, want) {
		t.Errorf("o opened %q, want %q", opened, want)
	}
}

func TestFilesOpenIsDisabledInEmptyDirectory(t *testing.T) {
	var opened []string
	m := press(t, fxMoveTo(t, fxOn(t, fxFixture(t)), "void"), "l")
	m.openURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	m = press(t, m, "o")
	if len(opened) != 0 {
		t.Errorf("o in an empty directory opened %q, want nothing", opened)
	}
}

func TestFilesStaleMessagesChangeNothing(t *testing.T) {
	m := fxMoveTo(t, fxOn(t, fxFixture(t)), "notes.txt")
	m = run(t, m, treeLoadedMsg{
		key:     core.Key{Kind: core.KindTree, Repo: fxInfra},
		entries: []domain.TreeEntry{fxEntry("stale.txt", domain.EntryFile, 1)},
	})
	m = run(t, m, previewLoadedMsg{
		key:     core.Key{Kind: core.KindPreview, Repo: fxInfra, Path: "notes.txt"},
		preview: domain.FilePreview{Text: "STALE\n"},
	})
	if v := screen(m); strings.Contains(v, "STALE") || strings.Contains(v, "stale.txt") || !strings.Contains(v, "# Notes") {
		t.Errorf("messages for another repo changed the Files tab:\n%s", v)
	}
}

func TestFilesCancelledAndFailedLoads(t *testing.T) {
	key := core.Key{Kind: core.KindTree, Repo: homelab, Path: "docs"}
	m := fxOn(t, fxFixture(t))
	m, _ = step(t, m, "j")

	m = run(t, m, treeLoadedMsg{key: key, err: context.Canceled})
	if m.status != "" || strings.Contains(screen(m), "couldn't load") {
		t.Errorf("cancelled load: status %q, want no status and no error text:\n%s", m.status, screen(m))
	}

	m = run(t, m, treeLoadedMsg{key: key, err: errors.New("forge exploded")})
	if !strings.Contains(m.status, "forge exploded") {
		t.Errorf("status %q, want the load error", m.status)
	}
	if v := screen(m); !strings.Contains(v, "couldn't load") {
		t.Errorf("docs/: want couldn't load in the preview:\n%s", v)
	}
}

func TestFilesEmptyRootDoesNotPanic(t *testing.T) {
	f := homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme})
	f.SetTree(homelab, "", nil)
	m := press(t, repoBox(t, f), "l", "]")
	if v := screen(m); !strings.Contains(v, "— empty —") {
		t.Fatalf("empty root: want — empty — in the list:\n%s", v)
	}
	m = press(t, m, "l", "j")
	if v := screen(m); !strings.Contains(v, "— empty —") {
		t.Errorf("l and j on an empty root: want — empty — still shown:\n%s", v)
	}
}

func TestFilesUnavailableWithoutTreeReader(t *testing.T) {
	h := homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme})
	f := noFxForge{Forge: h, rd: h}
	m := press(t, repoBox(t, f), "l", "]")
	if v := screen(m); !strings.Contains(v, "Files aren't available on this host") {
		t.Errorf("forge without TreeReader: want the unavailable message on the tab:\n%s", v)
	}
}

func TestFilesResetOnRepoSelection(t *testing.T) {
	f := fxFixture(t)
	f.AddRepo(domain.Repo{RepoRef: fxInfra, WebURL: "https://f.test/home/infra", Access: domain.AccessWrite})
	m := press(t, fxMoveTo(t, fxOn(t, f), "cmd"), "l")
	// From inside cmd/, three h presses reach the repo list; j moves from homelab to infra.
	m = press(t, m, "h", "h", "h", "j")
	if m.details.filesDir != "" || m.details.filesCur != 0 || m.details.filesFocus {
		t.Errorf("selecting infra: filesDir %q, filesCur %d, filesFocus %v; want the root, cursor 0 and list focus",
			m.details.filesDir, m.details.filesCur, m.details.filesFocus)
	}
	m = press(t, m, "k", "6", "l")
	if v := screen(m); strings.Contains(v, "[6] Repo › cmd") {
		t.Errorf("back on homelab, the Files tab kept the crumb into cmd/:\n%s", v)
	}
}

func TestFilesRefreshKeepsCursorOnPath(t *testing.T) {
	m := press(t, fxMoveTo(t, fxOn(t, fxFixture(t)), "notes.txt"), "r")
	if v := screen(m); !strings.Contains(v, "# Notes") || strings.Contains(v, "api/") {
		t.Errorf("r: want the cursor still on notes.txt:\n%s", v)
	}
}

func TestFilesSanitizesControlRunesInPreview(t *testing.T) {
	m := fxMoveTo(t, fxOn(t, fxFixture(t)), "color.txt")
	if v := screen(m); !strings.Contains(v, "�[31mred") {
		t.Errorf("preview: want the ESC shown as � and the text kept:\n%s", v)
	}
	if raw := m.View().Content; strings.Contains(raw, "\x1b[31mred") {
		t.Errorf("a raw ESC from the file reached View()")
	}
}

func TestFilesSanitizesEntryNames(t *testing.T) {
	f := homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme})
	f.SetTree(homelab, "", []domain.TreeEntry{fxEntry("a\x1bb", domain.EntryDir, 0)})
	f.SetTree(homelab, "a\x1bb", []domain.TreeEntry{fxFile(f, "a\x1bb/x.txt", "ok\n")})
	m := fxOn(t, f)
	if v := screen(m); !strings.Contains(v, "a�b/") {
		t.Errorf("list: want the directory name shown with the ESC as �:\n%s", v)
	}
	m = press(t, m, "l")
	if v := screen(m); !strings.Contains(v, "[6] Repo › a�b") {
		t.Errorf("crumb: want the sanitized name:\n%s", v)
	}
	if raw := m.View().Content; strings.Contains(raw, "a\x1bb") {
		t.Errorf("a raw ESC from the entry name reached View()")
	}
}

func TestFilesExpandsTabsAndDropsCarriageReturns(t *testing.T) {
	m := fxMoveTo(t, fxOn(t, fxFixture(t)), "tabbed.txt")
	if v := screen(m); !strings.Contains(v, "a    b") {
		t.Errorf("preview: want the tab rendered as four spaces:\n%s", v)
	}
	if strings.Contains(strip(m.View().Content), "\r") {
		t.Errorf("a carriage return reached View()")
	}
}

// The narrow case uses width 30, which stands in for a content width under 40: the exact cw is set by the layout.
func TestFilesNarrowPaneShowsOneColumn(t *testing.T) {
	m := run(t, fxOn(t, fxFixture(t)), tea.WindowSizeMsg{Width: 30, Height: 20})
	m = fxMoveTo(t, m, "notes.txt")
	if v := screen(m); !strings.Contains(v, "notes.txt") || strings.Contains(v, "# Notes") {
		t.Errorf("narrow pane with the list focused: want the list only:\n%s", v)
	}
	m = press(t, m, "l")
	if v := screen(m); !strings.Contains(v, "# Notes") || strings.Contains(v, "other.txt") {
		t.Errorf("narrow pane with the preview focused: want the preview only:\n%s", v)
	}
}

// The sweep stands in for a render-safety check: the exact cw and ch that trigger each layout branch are not part of the planned seams.
func TestFilesRenderSweepDoesNotPanicOrOverflow(t *testing.T) {
	for _, w := range []int{1, 10, 39, 40, 120} {
		for _, h := range []int{0, 1, 5} {
			m := run(t, fxOn(t, fxFixture(t)), tea.WindowSizeMsg{Width: w, Height: h})
			m = press(t, m, "l", "j", "G")
			for _, line := range lines(m) {
				if n := utf8.RuneCountInString(line); n > w {
					t.Errorf("%dx%d: line %q is %d cells wide", w, h, line, n)
				}
			}
		}
	}
}
