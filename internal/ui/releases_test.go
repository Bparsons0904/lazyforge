package ui

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// unitOffForge answers ListReleases the way Forgejo does when the repo's releases unit is disabled.
type unitOffForge struct{ forge.Forge }

func (unitOffForge) ListReleases(context.Context, domain.RepoRef) ([]domain.Release, error) {
	return nil, fmt.Errorf("releases of homelab: %w", forge.ErrNotFound)
}

// releasesFake returns homelab's Forgejo fake with rels seeded in the given order.
func releasesFake(rels ...domain.Release) *forgetest.Fake {
	f := homelabFake(nil)
	for _, r := range rels {
		f.AddRelease(homelab, r)
	}
	return f
}

// releasesOn returns a 120×40 model over f with homelab selected and the Releases box focused.
func releasesOn(t *testing.T, f forge.Forge) Model {
	t.Helper()
	return press(t, newModel(t, f), "j", "5")
}

// lineContaining returns the first screen line that contains sub, or "" when none does.
func lineContaining(m Model, sub string) string {
	for _, l := range lines(m) {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

func TestReleasesBoxSitsBetweenActionsAndRepo(t *testing.T) {
	now := time.Now()
	m := press(t, newModel(t, releasesFake(
		domain.Release{Tag: "rc-one", PublishedAt: now.Add(-time.Hour)},
		domain.Release{Tag: "rc-two", PublishedAt: now},
	)), "j")
	v := screen(m)
	actions, releases, repo := strings.Index(v, "[3] Actions"), strings.Index(v, "[5] Releases"), strings.Index(v, "[6] Repo")
	if actions < 0 || releases < 0 || repo < 0 {
		t.Fatalf("box titles missing; Actions %d, Releases %d, Repo %d:\n%s", actions, releases, repo, v)
	}
	if actions >= releases || releases >= repo {
		t.Errorf("box order: Actions at %d, Releases at %d, Repo at %d; want Actions, Releases, Repo:\n%s", actions, releases, repo, v)
	}
	if !strings.Contains(v, "Releases 2") {
		t.Errorf("Releases title lacks its count once loaded:\n%s", v)
	}
}

func TestReleasesRowsNewestFirstWithUndatedLast(t *testing.T) {
	now := time.Now()
	m := releasesOn(t, releasesFake(
		domain.Release{Tag: "rc-mid", PublishedAt: now.Add(-48 * time.Hour)},
		domain.Release{Tag: "rc-undated"},
		domain.Release{Tag: "rc-new", PublishedAt: now.Add(-time.Hour)},
		domain.Release{Tag: "rc-old", PublishedAt: now.Add(-96 * time.Hour)},
	))
	v := screen(m)
	prev := -1
	for _, tag := range []string{"rc-new", "rc-mid", "rc-old", "rc-undated"} {
		i := strings.Index(v, tag)
		if i <= prev {
			t.Fatalf("%s at %d after %d; want newest first, undated last:\n%s", tag, i, prev, v)
		}
		prev = i
	}
}

func TestReleaseOverviewShowsNotesAsMarkdown(t *testing.T) {
	m := press(t, releasesOn(t, releasesFake(domain.Release{
		Tag: "rc-two", Name: "Second", Notes: "Fixes the **login** flow\n\n- one item",
		PublishedAt: time.Now().Add(-48 * time.Hour),
	})), "l")
	v := screen(m)
	for _, want := range []string{"Overview", "Second", "home/homelab · rc-two", "Fixes the login flow", "one item"} {
		if !strings.Contains(v, want) {
			t.Errorf("overview missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "**login**") {
		t.Errorf("notes shown as raw markdown:\n%s", v)
	}
}

func TestReleaseMovingCursorShowsItsNotesFromTheTop(t *testing.T) {
	// Both notes overflow the pane, so a scroll left over from rc-two would hide rc-one's first line.
	var long, other strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&long, "line %02d\n\n", i)
		fmt.Fprintf(&other, "alpha %02d\n\n", i)
	}
	m := releasesOn(t, releasesFake(
		domain.Release{Tag: "rc-two", Notes: long.String(), PublishedAt: time.Now().Add(-time.Hour)},
		domain.Release{Tag: "rc-one", Notes: other.String(), PublishedAt: time.Now().Add(-48 * time.Hour)},
	))
	m = press(t, m, "l", "G")
	if v := screen(m); !strings.Contains(v, "line 40") {
		t.Fatalf("G did not scroll the long notes to their end:\n%s", v)
	}
	m = press(t, m, "h", "j", "l")
	if v := screen(m); !strings.Contains(v, "alpha 01") || strings.Contains(v, "line 40") {
		t.Errorf("after moving to rc-one, want its notes from the top and no rc-two notes:\n%s", v)
	}
}

func TestReleaseWithoutNameOrNotesFallsBack(t *testing.T) {
	m := press(t, releasesOn(t, releasesFake(domain.Release{Tag: "rc-bare", PublishedAt: time.Now().Add(-time.Hour)})), "l")
	v := screen(m)
	if !strings.Contains(v, "No release notes") {
		t.Errorf("blank notes missing the faint line:\n%s", v)
	}
	if !strings.Contains(v, "home/homelab · rc-bare") {
		t.Errorf("meta line missing:\n%s", v)
	}
	// The breadcrumb and the row also carry the tag, so only the overview's first line shows the heading.
	plain := func(_ domain.RepoRef, body string) string { return body }
	if head, _, _ := strings.Cut(strip(overview(domain.Release{Tag: "rc-bare"}, homelab, time.Now(), plain)), "\n"); head != "rc-bare" {
		t.Errorf("heading = %q, want the tag when the name is empty", head)
	}
}

// The row marker is matched after the tag with spaces only, so the Overview meta line ("tag · draft") can't satisfy it.
func TestReleaseDraftAndPrereleaseAreMarked(t *testing.T) {
	now := time.Now()
	m := releasesOn(t, releasesFake(
		domain.Release{Tag: "rc-one", Draft: true, PublishedAt: now.Add(-2 * time.Hour)},
		domain.Release{Tag: "rc-two", Prerelease: true, PublishedAt: now.Add(-3 * time.Hour)},
	))
	for _, tt := range []struct{ tag, mark string }{{"rc-one", "draft"}, {"rc-two", "pre-release"}} {
		row := regexp.MustCompile(tt.tag + ` +` + tt.mark + `\b`)
		if !row.MatchString(screen(m)) {
			t.Errorf("row for %s not marked %q:\n%s", tt.tag, tt.mark, screen(m))
		}
	}

	m = press(t, m, "l")
	if meta := lineContaining(m, "home/homelab · rc-one"); !strings.Contains(meta, "home/homelab · rc-one · draft") {
		t.Errorf("draft meta line = %q, want it marked draft", meta)
	}
	m = press(t, m, "h", "j", "l")
	if meta := lineContaining(m, "home/homelab · rc-two"); !strings.Contains(meta, "home/homelab · rc-two · pre-release") {
		t.Errorf("prerelease meta line = %q, want it marked pre-release", meta)
	}
}

// A single release keeps the row and the details pane on lines that no other release can share.
func TestUndatedReleaseShowsNoAge(t *testing.T) {
	m := releasesOn(t, releasesFake(domain.Release{Tag: "rc-zero"}))
	if row := lineContaining(m, "rc-zero"); row == "" || strings.Contains(row, "ago") {
		t.Errorf("undated row = %q, want no age", row)
	}

	m = press(t, m, "l")
	if meta := lineContaining(m, "home/homelab · rc-zero"); meta == "" || strings.Contains(meta, "ago") {
		t.Errorf("undated meta line = %q, want no age", meta)
	}
}

func TestReleaseOpensItsWebURL(t *testing.T) {
	var opened []string
	const url = "https://f.test/home/homelab/releases/tag/rc-one"
	m := releasesOn(t, releasesFake(domain.Release{Tag: "rc-one", WebURL: url, PublishedAt: time.Now()}))
	m.openURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	m = press(t, m, "o")
	if len(opened) != 1 || opened[0] != url {
		t.Fatalf("o on the box opened %v, want [%s]", opened, url)
	}
	press(t, m, "l", "o")
	if len(opened) != 2 || opened[1] != url {
		t.Errorf("o in the details pane opened %v, want the release's URL", opened)
	}
}

func TestReleaseWithoutWebURLDoesNotOpen(t *testing.T) {
	var opened []string
	m := releasesOn(t, releasesFake(domain.Release{Tag: "rc-none"}))
	m.openURL = func(u string) error {
		opened = append(opened, u)
		return nil
	}
	press(t, m, "o", "l", "o")
	if len(opened) != 0 {
		t.Errorf("o opened %v for a release with no web URL", opened)
	}
}

func TestReleaseControlCharactersAreReplaced(t *testing.T) {
	// The plain release is newest, so the overview shows it and the control-character release appears only as a list row.
	m := releasesOn(t, releasesFake(
		domain.Release{Tag: "rc-plain", PublishedAt: time.Now()},
		domain.Release{Tag: "rc\x1b9", Name: "a\x00b\tc", Notes: "safe \x1b]0;evil\x07 text", PublishedAt: time.Now().Add(-time.Hour)},
	))
	if row := lineContaining(m, "rc�9"); !strings.Contains(row, "a�b    c") {
		t.Errorf("row = %q, want the tag and name with control runes replaced", row)
	}
	for _, bad := range []string{"\x1b9", "\x00", "\t"} {
		if strings.Contains(m.View().Content, bad) {
			t.Errorf("screen carries raw %q", bad)
		}
	}

	m = press(t, m, "j", "l")
	raw := m.View().Content
	if strings.Contains(raw, "\x00") {
		t.Errorf("overview carries a raw NUL")
	}
	if !strings.Contains(strip(raw), "a�b    c") {
		t.Errorf("heading missing the sanitised name:\n%s", strip(raw))
	}
	if strings.Contains(raw, "\x1b]") || strings.Contains(raw, "\x07") {
		t.Errorf("notes carried an OSC sequence into the output: %q", raw)
	}
}

func TestReleasesLoadedMsgStaleAndErrors(t *testing.T) {
	m := releasesOn(t, releasesFake(domain.Release{Tag: "rc-one", PublishedAt: time.Now()}))
	before := len(m.boxes.releases)
	if before != 1 {
		t.Fatalf("setup: %d releases", before)
	}
	other := core.Key{Kind: core.KindReleases, Repo: domain.RepoRef{Owner: "home", Name: "infra"}}
	m = run(t, m, releasesLoadedMsg{key: other, items: []domain.Release{{Tag: "stale"}}})
	if len(m.boxes.releases) != before || m.boxes.releases[0].Tag == "stale" {
		t.Fatalf("stale message replaced the box: %+v", m.boxes.releases)
	}

	key := core.Key{Kind: core.KindReleases, Repo: homelab}
	m = run(t, m, releasesLoadedMsg{key: key, err: context.Canceled})
	if m.status != "" || len(m.boxes.releases) != before {
		t.Fatalf("canceled: status %q releases %d", m.status, len(m.boxes.releases))
	}
	m = run(t, m, releasesLoadedMsg{key: key, err: errors.New("forge exploded")})
	if !strings.Contains(m.status, "forge exploded") || len(m.boxes.releases) != before {
		t.Fatalf("error after a load: status %q releases %d", m.status, len(m.boxes.releases))
	}
	if !strings.Contains(screen(m), "Releases 1") {
		t.Errorf("failed refresh lost the previous list:\n%s", screen(m))
	}
}

func TestReleasesShowLoadingUntilTheListArrives(t *testing.T) {
	m := newModel(t, releasesFake(domain.Release{Tag: "rc-one", PublishedAt: time.Now()}))
	m, msgs := step(t, m, "j")
	for _, msg := range msgs {
		if _, ok := msg.(releasesLoadedMsg); !ok {
			m = run(t, m, msg)
		}
	}
	m = press(t, m, "5")
	if v := screen(m); !strings.Contains(v, "Loading…") {
		t.Fatalf("Releases before its load arrives, want Loading…:\n%s", v)
	}

	key := core.Key{Kind: core.KindReleases, Repo: homelab}
	m = run(t, m, releasesLoadedMsg{key: key, err: errors.New("boom")})
	if !strings.Contains(m.status, "boom") || !strings.Contains(screen(m), "Loading…") {
		t.Errorf("failed first load: status %q\n%s", m.status, screen(m))
	}
	m = run(t, m, releasesLoadedMsg{key: key, items: []domain.Release{{Tag: "rc-one"}}})
	if v := screen(m); !strings.Contains(v, "Releases 1") {
		t.Errorf("loaded list missing its count:\n%s", v)
	}
}

func TestReleasesNotFoundShowsNone(t *testing.T) {
	m := releasesOn(t, unitOffForge{homelabFake(nil)})
	if v := screen(m); !strings.Contains(v, "Releases 0") || !strings.Contains(v, "— none —") || m.status != "" {
		t.Errorf("releases unit off: status %q, want none and no status error:\n%s", m.status, v)
	}
}

func TestReleasesEmptyRepoShowsNoneAndSaysEmptyOnL(t *testing.T) {
	m := releasesOn(t, homelabFake(nil))
	if v := screen(m); !strings.Contains(v, "Releases 0") || !strings.Contains(v, "— none —") {
		t.Errorf("empty Releases box:\n%s", v)
	}
	m = press(t, m, "l")
	if m.level != levelBoxes || m.status != "This box is empty" {
		t.Fatalf("l on an empty Releases box: level %v status %q", m.level, m.status)
	}
}

func TestReleaseKeysJumpAndTabPassThrough(t *testing.T) {
	m := press(t, newModel(t, releasesFake(domain.Release{Tag: "rc-one"})), "j", "5")
	if m.level != levelBoxes || m.boxes.focus != boxReleases {
		t.Fatalf("5 from the repo list: level %v focus %v", m.level, m.boxes.focus)
	}
	m = press(t, m, "tab")
	if m.boxes.focus != boxRepo {
		t.Fatalf("tab from [5]: focus %v, want the Repo box", m.boxes.focus)
	}
	m = press(t, m, "shift+tab")
	if m.boxes.focus != boxReleases {
		t.Fatalf("shift+tab from [6]: focus %v, want the Releases box", m.boxes.focus)
	}
	m = press(t, m, "4")
	if m.status != "No box [4] here" || m.boxes.focus != boxReleases {
		t.Fatalf("4: status %q focus %v", m.status, m.boxes.focus)
	}
	m = press(t, m, "l", "5")
	if m.level != levelBoxes || m.boxes.focus != boxReleases {
		t.Fatalf("5 from the details pane: level %v focus %v", m.level, m.boxes.focus)
	}
}
