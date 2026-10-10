package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

// sampleReadme has a heading, a list, a code fence and the relative image and link a README may carry.
const sampleReadme = "# Home lab\n\nServices and their notes.\n\n- Forgejo\n- Renovate\n\n```sh\nmake check\n```\n\n![x](docs/a.png) [l](docs/b.md)\n"

// noReadmeForge hides the ReadmeReader of the forge it wraps.
type noReadmeForge struct{ forge.Forge }

// homelabFake returns a Forgejo fake whose only repo is homelab, with the README rd when rd is non-nil.
func homelabFake(rd *domain.Readme) *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://f.test", ChangeRequestTerm: "PR", User: "you"})
	f.AddRepo(domain.Repo{RepoRef: homelab, Description: "home lab", WebURL: "https://f.test/home/homelab", Access: domain.AccessWrite})
	if rd != nil {
		f.SetReadme(homelab, *rd)
	}
	return f
}

// newModel returns a 120×40 model over f with the repo list loaded.
func newModel(t *testing.T, f forge.Forge) Model {
	t.Helper()
	m := New(context.Background(), core.New(f, core.Options{}))
	m.tick = func() tea.Cmd { return func() tea.Msg { return tickScheduled{} } }
	m.poll = func() tea.Cmd { return func() tea.Msg { return pollScheduled{} } }
	return sizedWith(t, m, 120, 40)
}

// repoBox returns a 120×40 model over f with homelab selected and the Repo box focused.
func repoBox(t *testing.T, f forge.Forge) Model {
	t.Helper()
	return press(t, newModel(t, f), "j", "6")
}

// readmeTab returns a 120×40 model over f with the README tab of the Repo box open in details.
func readmeTab(t *testing.T, f forge.Forge) Model {
	t.Helper()
	return press(t, repoBox(t, f), "l")
}

func TestRepoBoxIsSixthAndReachableByTab(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "j", "3", "tab")
	if m.boxes.focus != boxReleases {
		t.Fatalf("tab from [3]: focus %v, want the Releases box", m.boxes.focus)
	}
	m = press(t, m, "tab")
	if m.boxes.focus != boxRepo {
		t.Fatalf("tab from [5]: focus %v, want the Repo box", m.boxes.focus)
	}
	m = press(t, m, "tab")
	if m.boxes.focus != boxCRs {
		t.Fatalf("tab from [6]: focus %v, want wrap to [1]", m.boxes.focus)
	}
	m = press(t, m, "shift+tab")
	if m.boxes.focus != boxRepo {
		t.Fatalf("shift+tab from [1]: focus %v, want the Repo box", m.boxes.focus)
	}
	m = press(t, m, "1", "6")
	if m.boxes.focus != boxRepo || m.status != "" {
		t.Fatalf("6: focus %v status %q", m.boxes.focus, m.status)
	}
	if v := screen(m); !strings.Contains(v, "[6] Repo") || !strings.Contains(v, "home/homelab") {
		t.Errorf("Repo box missing its title or row:\n%s", v)
	}
}

func TestRepoBoxKeepsNumberWhenActionsHidden(t *testing.T) {
	f := newDemo()
	f.SetGate(forge.ActRuns, errors.New("no actions"))
	m := press(t, sizedWith(t, seededWith(t, f), 120, 40), "j", "6")
	if m.boxes.focus != boxRepo {
		t.Fatalf("6: focus %v, want the Repo box", m.boxes.focus)
	}
	for _, k := range []string{"3", "4"} {
		before := m.boxes.focus
		m = press(t, m, k)
		if m.status != "No box ["+k+"] here" || m.boxes.focus != before {
			t.Errorf("%s: status %q focus %v, want no box and focus %v", k, m.status, m.boxes.focus, before)
		}
	}
	for _, want := range []boxKind{boxCRs, boxIssues, boxReleases, boxRepo} {
		m = press(t, m, "tab")
		if m.boxes.focus != want {
			t.Fatalf("tab: focus %v, want %v (order [1] [2] [5] [6])", m.boxes.focus, want)
		}
	}
}

func TestNoRepoBoxWithoutReadmeReader(t *testing.T) {
	m := press(t, newModel(t, noReadmeForge{homelabFake(nil)}), "j", "6")
	if m.boxes.showRepo {
		t.Fatal("Repo box shown on a forge without a ReadmeReader")
	}
	if m.status != "No box [6] here" || m.boxes.focus == boxRepo {
		t.Errorf("6: status %q focus %v, want no box", m.status, m.boxes.focus)
	}
	if v := screen(m); strings.Contains(v, "[6] Repo") {
		t.Errorf("Repo box drawn without a ReadmeReader:\n%s", v)
	}
}

func TestStarViewHasNoRepoBox(t *testing.T) {
	m := press(t, sized(t, 120, 40), "l")
	if m.level != levelBoxes || m.star.focus != starByRepo {
		t.Fatalf("l on ★: level %v focus %v", m.level, m.star.focus)
	}
	m = press(t, m, "6")
	if m.status != "No box [6] here" {
		t.Errorf("6 in the ★ view: status %q", m.status)
	}
	if v := screen(m); strings.Contains(v, "[6] Repo") {
		t.Errorf("★ view shows a box [6]:\n%s", v)
	}
}

func TestRepoDetailsTabs(t *testing.T) {
	m := readmeTab(t, homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme}))
	if m.level != levelDetails || m.details.tab != 0 {
		t.Fatalf("l on [6]: level %v tab %d, want details on README", m.level, m.details.tab)
	}
	v := screen(m)
	for _, want := range []string{"README", "Files", "Branches"} {
		if !strings.Contains(v, want) {
			t.Errorf("tabs missing %q:\n%s", want, v)
		}
	}
	m = press(t, m, "]")
	if v := screen(m); m.details.tab != 1 || !strings.Contains(v, "— empty —") || strings.Contains(v, "Coming soon") {
		t.Errorf("]: tab %d, want Files showing an empty listing:\n%s", m.details.tab, v)
	}
	m = press(t, m, "]")
	if v := screen(m); m.details.tab != 2 || !strings.Contains(v, "No branches") || strings.Contains(v, "Coming soon") {
		t.Errorf("]: tab %d, want Branches showing No branches:\n%s", m.details.tab, v)
	}
	m = press(t, m, "]")
	if m.details.tab != 0 {
		t.Errorf("] from Branches: tab %d, want wrap to README", m.details.tab)
	}
	m = press(t, m, "[")
	if m.details.tab != 2 {
		t.Errorf("[ from README: tab %d, want wrap to Branches", m.details.tab)
	}
}

func TestReadmeRendersLikePRBody(t *testing.T) {
	m := readmeTab(t, homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme}))
	v := screen(m)
	for _, want := range []string{"Home lab", "Services and their notes.", "Forgejo", "Renovate", "make check"} {
		if !strings.Contains(v, want) {
			t.Errorf("README missing %q:\n%s", want, v)
		}
	}
	if m.status != "" {
		t.Errorf("status %q after a README with a relative image and link", m.status)
	}

	m = readmeTab(t, homelabFake(&domain.Readme{Name: "README.rst", Body: "Plain text readme."}))
	if v := screen(m); !strings.Contains(v, "Plain text readme.") {
		t.Errorf("non-markdown README not shown:\n%s", v)
	}
}

func TestReadmeImageFallsBackToLink(t *testing.T) {
	body := "intro\n\n![x](docs/a.png)\n\noutro\n"
	m := readmeTab(t, homelabFake(&domain.Readme{Name: "README.md", Body: body}))
	m, loads := settle(t, m, enableShots)
	if loads != 1 {
		t.Fatalf("enabling images started %d loads, want 1 for the README image", loads)
	}
	if m.details.img == nil {
		t.Fatal("images are off after enabling them")
	}
	// The demo forge holds no docs/a.png, so the fetch fails and the image ends as its link.
	key := imageKey(homelab, "docs/a.png")
	m, _ = settle(t, m, imageLoadedMsg{set: m.details.img, key: key, err: forge.ErrNotFound})
	if e := entryAt(m, key); e == nil || e.state != imageFailed {
		t.Fatalf("entry for %q = %+v, want failed", key, e)
	}
	if m.status != "" {
		t.Errorf("status %q after a failed README image, want none", m.status)
	}
	if v := screen(m); !strings.Contains(v, "🖼 x") {
		t.Errorf("failed image shows no link fallback:\n%s", v)
	}
}

func TestReadmeScrolls(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line %d\n\n", i)
	}
	m := readmeTab(t, homelabFake(&domain.Readme{Name: "README.md", Body: b.String()}))
	if off := m.details.vp.YOffset(); off != 0 {
		t.Fatalf("opened README at offset %d, want 0", off)
	}
	m = press(t, m, "j")
	if off := m.details.vp.YOffset(); off != 1 {
		t.Fatalf("j: offset %d, want 1", off)
	}
	m = press(t, m, "ctrl+d")
	half := m.details.vp.YOffset()
	if half <= 1 {
		t.Fatalf("ctrl+d: offset %d, want more than one line down", half)
	}
	m = press(t, m, "G")
	if bottom := m.details.vp.YOffset(); bottom <= half {
		t.Fatalf("G: offset %d, want past the half page at %d", bottom, half)
	}
	m = press(t, m, "g", "g")
	if off := m.details.vp.YOffset(); off != 0 {
		t.Fatalf("gg: offset %d, want 0", off)
	}

	m = press(t, m, "G")
	kept := m.details.vp.YOffset()
	m = press(t, m, "h", "r", "l")
	if off := m.details.vp.YOffset(); off != kept {
		t.Errorf("r on the Repo box: offset %d, want the scroll kept at %d", off, kept)
	}

	m = press(t, m, "]")
	if off := m.details.vp.YOffset(); off != 0 {
		t.Errorf("switching tab: offset %d, want 0", off)
	}
}

func TestReadmeStates(t *testing.T) {
	t.Run("no README", func(t *testing.T) {
		m := readmeTab(t, homelabFake(nil))
		if v := screen(m); !strings.Contains(v, "No README") {
			t.Errorf("repo without a README:\n%s", v)
		}
	})

	t.Run("loading", func(t *testing.T) {
		m := newModel(t, homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme}))
		m, _ = step(t, m, "j")
		m, _ = step(t, m, "6")
		m, _ = step(t, m, "l")
		if v := screen(m); !strings.Contains(v, "Loading…") || strings.Contains(v, "No README") {
			t.Errorf("before the load lands:\n%s", v)
		}
	})

	t.Run("empty body is not missing", func(t *testing.T) {
		m := readmeTab(t, homelabFake(&domain.Readme{Name: "README", Body: ""}))
		if v := screen(m); strings.Contains(v, "No README") || strings.Contains(v, "Loading…") {
			t.Errorf("empty README shows a placeholder:\n%s", v)
		}
	})
}

func TestReadmeCachedAcrossReselection(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "j")
	m, _ = step(t, m, "k")
	m, msgs := step(t, m, "j")
	if c := counts(msgs); c["readme"] != 0 {
		t.Errorf("reselecting homelab issued %v, want no README load", c)
	}
	if !m.boxes.readme.ok {
		t.Error("reselected repo's README not seeded from the cache")
	}
}

func TestStaleReadmeIsDropped(t *testing.T) {
	m := newModel(t, homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme}))
	m, _ = step(t, m, "j")
	m = run(t, m, readmeLoadedMsg{key: core.Key{Kind: core.KindReadme, Repo: infra}, readme: domain.Readme{Name: "README.md", Body: "stale"}})
	if m.boxes.readme.ok {
		t.Fatalf("README of another repo was accepted: %+v", m.boxes.readme.r)
	}
	m = run(t, m, readmeLoadedMsg{key: core.Key{Kind: core.KindReadme, Repo: homelab}, readme: domain.Readme{Name: "README.md", Body: sampleReadme}})
	if !m.boxes.readme.ok || m.boxes.readme.r.Name != "README.md" {
		t.Errorf("README of the selected repo dropped: %+v", m.boxes.readme)
	}
}

func TestReadmeLoadErrorSetsStatus(t *testing.T) {
	m := newModel(t, homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme}))
	m, _ = step(t, m, "j")
	m = run(t, m, readmeLoadedMsg{key: core.Key{Kind: core.KindReadme, Repo: homelab}, err: errors.New("forge is down")})
	if !strings.Contains(m.status, "forge is down") {
		t.Errorf("status %q, want the load error", m.status)
	}
	if m.boxes.readme.ok {
		t.Error("failed load marked the README loaded")
	}

	before := m.status
	m = run(t, m, readmeLoadedMsg{key: core.Key{Kind: core.KindReadme, Repo: homelab}, err: context.Canceled})
	if m.status != before {
		t.Errorf("cancelled load changed status to %q", m.status)
	}
}

func TestLoadReadmeReturnsKeyedMessage(t *testing.T) {
	svc := core.New(homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme}), core.Options{})
	msgs := exec(t, loadReadme(context.Background(), svc, homelab))
	if len(msgs) != 1 {
		t.Fatalf("loadReadme gave %d messages, want 1", len(msgs))
	}
	got, ok := msgs[0].(readmeLoadedMsg)
	if !ok || got.key != (core.Key{Kind: core.KindReadme, Repo: homelab}) || got.err != nil || got.readme.Body != sampleReadme {
		t.Errorf("loadReadme message = %#v", msgs[0])
	}
}

func TestActionKeysDoNothingOnRepoBox(t *testing.T) {
	for _, k := range []string{"m", "a", "x", "c", "L", "R", "space"} {
		m := repoBox(t, homelabFake(&domain.Readme{Name: "README.md", Body: sampleReadme}))
		for _, at := range []string{"boxes", "details"} {
			next, msgs := step(t, m, k)
			if len(msgs) != 0 || next.dialog != nil {
				t.Errorf("%s at %s: %d messages, dialog %v; want nothing", k, at, len(msgs), next.dialog != nil)
			}
			m = press(t, m, "l")
		}
	}
}

func TestFourBoxesFitAt80x24(t *testing.T) {
	m := press(t, sized(t, 80, 24), "j", "6")
	assertFits(t, m, "four boxes")
	if v := screen(m); !strings.Contains(v, "[6] Repo") || !strings.Contains(v, "[3]") {
		t.Errorf("box titles missing:\n%s", v)
	}
}

func TestRepoBoxAccentIsDistinct(t *testing.T) {
	if len(style.RepoAccents) < int(boxRepo)+1 {
		t.Fatalf("%d repo accents, want one for [6]", len(style.RepoAccents))
	}
	for i := range 3 {
		if style.RepoAccents[int(boxRepo)] == style.RepoAccents[i] {
			t.Errorf("accent for [6] equals the accent for box %d", i+1)
		}
	}
}

func TestTabsByItemType(t *testing.T) {
	if got := tabs(domain.Repo{}); !slices.Equal(got, []string{"README", "Files", "Branches"}) {
		t.Errorf("tabs(Repo) = %v", got)
	}
	for _, item := range []any{domain.Issue{}, domain.ChangeRequest{}} {
		if got := tabs(item); !slices.Equal(got, []string{"Overview"}) {
			t.Errorf("tabs(%T) = %v, want Overview", item, got)
		}
	}
}

func TestBoxKindsFollowVisibility(t *testing.T) {
	if int(boxRepo) != 5 {
		t.Errorf("boxRepo = %d, want 5 so the box is numbered [6]", int(boxRepo))
	}
	if got, want := (boxes{showRuns: true, showRepo: true}).kinds(), []boxKind{boxCRs, boxIssues, boxRuns, boxReleases, boxRepo}; !slices.Equal(got, want) {
		t.Errorf("all boxes: kinds %v, want %v", got, want)
	}
	if got, want := (boxes{showRepo: true}).kinds(), []boxKind{boxCRs, boxIssues, boxReleases, boxRepo}; !slices.Equal(got, want) {
		t.Errorf("no Actions: kinds %v, want %v", got, want)
	}
	if got, want := (boxes{showRuns: true}).kinds(), []boxKind{boxCRs, boxIssues, boxRuns, boxReleases}; !slices.Equal(got, want) {
		t.Errorf("no Repo box: kinds %v, want %v", got, want)
	}
	if n := (boxes{showRuns: true}).count(); n != 4 {
		t.Errorf("count without Repo box = %d, want 4", n)
	}
}

func TestBoxStepSkipsHiddenBoxes(t *testing.T) {
	b := boxes{showRepo: true, focus: boxIssues}
	if got := b.step(1); got != boxReleases {
		t.Errorf("step(1) from [2] = %v, want [5]", got)
	}
	b.focus = boxReleases
	if got := b.step(1); got != boxRepo {
		t.Errorf("step(1) from [5] = %v, want [6]", got)
	}
	b.focus = boxRepo
	if got := b.step(1); got != boxCRs {
		t.Errorf("step(1) from [6] = %v, want wrap to [1]", got)
	}
	b.focus = boxCRs
	if got := b.step(-1); got != boxRepo {
		t.Errorf("step(-1) from [1] = %v, want wrap to [6]", got)
	}
	b = boxes{showRuns: true, showRepo: true, focus: boxRuns}
	if got := b.step(1); got != boxReleases {
		t.Errorf("step(1) from [3] = %v, want [5]", got)
	}
}
