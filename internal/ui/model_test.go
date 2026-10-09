package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestInitLoadsReposInOrderAndStartsTick(t *testing.T) {
	m := seeded(t)
	msgs := exec(t, m.Init())
	var loaded *reposLoadedMsg
	for _, msg := range msgs {
		if r, ok := msg.(reposLoadedMsg); ok {
			loaded = &r
		}
	}
	if loaded == nil || len(loaded.repos) != 4 {
		t.Fatalf("Init msgs %v: want a reposLoadedMsg with 4 repos", msgs)
	}
	if c := counts(msgs); c["tick"] != 1 || len(msgs) != 2 {
		t.Fatalf("Init gave %v, want the repo load plus the tick", c)
	}
	m = sized(t, 80, 24)
	view := strings.Join(lines(m)[2:], "\n")
	prev := -1
	for _, name := range []string{"★ Renovate", "homelab", "infra", "lazyforge", "dotfiles"} {
		i := strings.Index(view, name)
		if i <= prev {
			t.Fatalf("%q out of order (index %d after %d) in:\n%s", name, i, prev, view)
		}
		prev = i
	}
}

func TestAltScreenAndViewBeforeSize(t *testing.T) {
	m := seeded(t)
	v := m.View()
	if v.Content != "" || !v.AltScreen {
		t.Fatalf("before size: content %q alt %v", v.Content, v.AltScreen)
	}
	m = boot(t, m)
	m = press(t, m, "j", "l", "l", "?", "esc", "r")
	if !m.View().AltScreen {
		t.Fatal("AltScreen false")
	}
}

func TestTinyWindowDoesNotPanic(t *testing.T) {
	m := sized(t, 20, 5)
	m = press(t, m, "j", "l", "2", "l", "]", "G", "?")
	_ = m.View()
}

func TestSelectionShowsBoxes(t *testing.T) {
	m := sized(t, 120, 40)
	m, msgs := step(t, m, "j")
	// The ★ scan at startup already cached the change requests and issues, so only the runs, README, branches and root tree miss.
	// The commits load waits for the branch list, so it isn't issued yet.
	if c := counts(msgs); c["crs"] != 0 || c["issues"] != 0 || c["runs"] != 1 || c["readme"] != 1 || c["branches"] != 1 || c["tree"] != 1 || c["preview"] != 0 || len(msgs) != 4 {
		t.Fatalf("selecting homelab issued %v, want only the uncached runs, README, branches and root tree loads", c)
	}
	for _, msg := range msgs {
		m = run(t, m, msg)
	}
	view := strings.Join(lines(m), "\n")
	for _, want := range []string{"Pull requests 3", "Issues 2", "Actions 2", "#42", "3 PR"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestRunsGateHidesActions(t *testing.T) {
	f := newDemo()
	f.SetGate(forge.ActRuns, errors.New("no actions"))
	m := sizedWith(t, seededWith(t, f), 120, 40)
	m, msgs := step(t, m, "j")
	if c := counts(msgs); c["runs"] != 0 || c["crs"] != 0 || c["issues"] != 0 || c["readme"] != 1 || c["branches"] != 1 || c["tree"] != 1 || len(msgs) != 3 {
		t.Fatalf("gated select issued %v, want only the README, branches and root tree loads", c)
	}
	for _, msg := range msgs {
		m = run(t, m, msg)
	}
	if v := strings.Join(lines(m), "\n"); strings.Contains(v, "Actions") {
		t.Errorf("gated view still shows Actions:\n%s", v)
	}
	m = press(t, m, "3")
	if m.status != "No box [3] here" || m.level != levelRepos {
		t.Fatalf("status %q level %v", m.status, m.level)
	}
}

func TestStaleAndErrorLoads(t *testing.T) {
	m := sized(t, 80, 24)
	m = press(t, m, "j")
	before := len(m.boxes.crs)
	if before != 3 {
		t.Fatalf("setup: %d CRs", before)
	}
	other := core.Key{Kind: core.KindChangeRequests, Repo: domain.RepoRef{Owner: "home", Name: "infra"}}
	m = run(t, m, changeRequestsLoadedMsg{key: other, items: []domain.ChangeRequest{{Number: 99}}})
	if len(m.boxes.crs) != before || m.boxes.crs[0].Number == 99 {
		t.Fatalf("stale message replaced boxes: %+v", m.boxes.crs)
	}
	m = run(t, m, issuesLoadedMsg{key: core.Key{Kind: core.KindIssues, Repo: other.Repo}, err: errors.New("stale boom")})
	if m.status != "" {
		t.Fatalf("stale error set status %q", m.status)
	}

	key := core.Key{Kind: core.KindChangeRequests, Repo: homelab}
	m = run(t, m, changeRequestsLoadedMsg{key: key, err: context.Canceled})
	if m.status != "" || len(m.boxes.crs) != before {
		t.Fatalf("canceled: status %q crs %d", m.status, len(m.boxes.crs))
	}
	m = run(t, m, changeRequestsLoadedMsg{key: key, err: errors.New("forge exploded")})
	if !strings.Contains(m.status, "forge exploded") || len(m.boxes.crs) != before {
		t.Fatalf("error: status %q crs %d", m.status, len(m.boxes.crs))
	}
	if !strings.Contains(strings.Join(lines(m), "\n"), "forge exploded") {
		t.Error("status-bar error not rendered")
	}
}

func TestCachedReselectIssuesNoFetch(t *testing.T) {
	m := sized(t, 80, 24)
	m = press(t, m, "j")
	m, _ = step(t, m, "k")
	m, msgs := step(t, m, "j")
	// Landing on a branch always refetches its commits, and the Files tab reloads the root listing; everything else stays cached.
	if c := counts(msgs); c["commits"] != 1 || c["tree"] != 1 || len(msgs) != 2 {
		t.Fatalf("cached re-select fetched %v, want only the cursor branch's commits and the root tree", c)
	}
	if len(m.boxes.crs) != 3 {
		t.Fatalf("cache not seeded: %d CRs", len(m.boxes.crs))
	}
}

func TestRefreshRefetchesEvenWhenCached(t *testing.T) {
	m := sized(t, 80, 24)
	m = press(t, m, "j")
	_, msgs := step(t, m, "r")
	c := counts(msgs)
	if c["repos"] != 1 || c["crs"] != 1 || c["issues"] != 1 || c["runs"] != 1 || c["readme"] != 1 || c["branches"] != 1 || c["commits"] != 1 || c["tree"] != 2 || len(msgs) != 9 {
		t.Fatalf("r issued %v", c)
	}

	_, cmd := m.Update(refreshTickMsg{})
	c = counts(exec(t, cmd))
	if c["repos"] != 1 || c["crs"] != 1 || c["issues"] != 1 || c["runs"] != 1 || c["readme"] != 1 || c["branches"] != 1 || c["commits"] != 1 || c["tree"] != 2 || c["tick"] != 1 {
		t.Fatalf("tick issued %v, want the loads plus one new tick", c)
	}
}

func TestRenovateRowOpensStarBoxes(t *testing.T) {
	for _, k := range []string{"l", "1", "enter"} {
		m := sized(t, 80, 24)
		m, msgs := step(t, m, k)
		if m.level != levelBoxes || m.star.focus != starByRepo || len(msgs) != 0 {
			t.Errorf("%s: level %v focus %v msgs %d", k, m.level, m.star.focus, len(msgs))
		}
	}
	m := sized(t, 120, 40)
	v := strings.Join(lines(m), "\n")
	for _, want := range []string{"[1] By repo", "[2] Updates by dependency", "[3] Renovate PRs", "[4] Dashboards", "[5] Failing / running CI"} {
		if !strings.Contains(v, want) {
			t.Errorf("right pane missing %q:\n%s", want, v)
		}
	}
}

func TestNavigation(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "j", "l")
	if m.level != levelBoxes || m.boxes.focus != boxCRs {
		t.Fatalf("l: level %v focus %v", m.level, m.boxes.focus)
	}
	want := "forge.home.arpa › home/homelab › [1] Pull requests › #42"
	if got := lines(m)[0]; !strings.Contains(got, want) {
		t.Errorf("header %q lacks %q", got, want)
	}
	m = press(t, m, "j", "j", "j", "j", "j")
	if m.boxes.cursor[boxCRs] != 2 {
		t.Fatalf("j x5: cursor %d, want 2", m.boxes.cursor[boxCRs])
	}
	m = press(t, m, "k", "k", "k", "k")
	if m.boxes.cursor[boxCRs] != 0 {
		t.Fatalf("k x4: cursor %d, want 0", m.boxes.cursor[boxCRs])
	}
	m = press(t, m, "2")
	if m.boxes.focus != boxIssues {
		t.Fatalf("2: focus %v", m.boxes.focus)
	}
	m = press(t, m, "3", "tab")
	if m.boxes.focus != boxRepo {
		t.Fatalf("tab from [3]: focus %v, want the Repo box", m.boxes.focus)
	}
	m = press(t, m, "tab")
	if m.boxes.focus != boxCRs {
		t.Fatalf("tab from last box: focus %v, want wrap to first", m.boxes.focus)
	}
	m = press(t, m, "shift+tab")
	if m.boxes.focus != boxRepo {
		t.Fatalf("shift+tab from first: focus %v, want wrap to the Repo box", m.boxes.focus)
	}
	m = press(t, m, "1", "l")
	if m.level != levelDetails {
		t.Fatalf("l: level %v", m.level)
	}
	m = press(t, m, "h")
	if m.level != levelBoxes {
		t.Fatalf("h: level %v", m.level)
	}
	m = press(t, m, "h")
	if m.level != levelRepos {
		t.Fatalf("h: level %v", m.level)
	}
	if m = press(t, m, "h"); m.level != levelRepos {
		t.Fatalf("h at repos: level %v", m.level)
	}
}

func TestJumpFromReposAndDetails(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "j", "2")
	if m.level != levelBoxes || m.boxes.focus != boxIssues {
		t.Fatalf("2 from repos: level %v focus %v", m.level, m.boxes.focus)
	}
	m = press(t, m, "l", "1")
	if m.level != levelBoxes || m.boxes.focus != boxCRs {
		t.Fatalf("1 from details: level %v focus %v", m.level, m.boxes.focus)
	}
	m = press(t, m, "l", "tab")
	if m.level != levelBoxes || m.boxes.focus != boxIssues {
		t.Fatalf("tab from details: level %v focus %v", m.level, m.boxes.focus)
	}
	m = press(t, m, "l", "shift+tab")
	if m.level != levelBoxes || m.boxes.focus != boxCRs {
		t.Fatalf("shift+tab from details: level %v focus %v", m.level, m.boxes.focus)
	}
	m = press(t, m, "4")
	if m.status != "No box [4] here" || m.boxes.focus != boxCRs {
		t.Fatalf("4: status %q focus %v", m.status, m.boxes.focus)
	}
}

func TestEmptyBox(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "G", "l")
	if m.level != levelBoxes {
		t.Fatalf("level %v", m.level)
	}
	if v := strings.Join(lines(m), "\n"); !strings.Contains(v, "— none —") || !strings.Contains(v, "Nothing here.") {
		t.Errorf("empty box view:\n%s", v)
	}
	m = press(t, m, "l")
	if m.level != levelBoxes || m.status != "This box is empty" {
		t.Fatalf("level %v status %q", m.level, m.status)
	}
	if m = press(t, m, "j"); m.status != "" {
		t.Fatalf("status survived a key press: %q", m.status)
	}
}

func TestDetailsOverviews(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "j", "l", "l")
	v := strings.Join(lines(m), "\n")
	for _, want := range []string{"Overview", "CI passing", "home/homelab #42"} {
		if !strings.Contains(v, want) {
			t.Errorf("PR overview missing %q:\n%s", want, v)
		}
	}
	// A single tab makes [ and ] no-ops.
	m = press(t, m, "]", "]", "[")
	if m.details.tab != 0 || !strings.Contains(strings.Join(lines(m), "\n"), "CI passing") {
		t.Fatalf("tab %d after ] [", m.details.tab)
	}
	for _, k := range []string{"Files", "Comments", "Checks", "Not available yet."} {
		if strings.Contains(strings.Join(lines(m), "\n"), k) {
			t.Errorf("view shows placeholder tab %q", k)
		}
	}

	m = press(t, m, "h", "3", "l")
	v = strings.Join(lines(m), "\n")
	for _, want := range []string{"deploy", "push", "main @ a1c9e2f", "took 1m12s"} {
		if !strings.Contains(v, want) {
			t.Errorf("run overview missing %q:\n%s", want, v)
		}
	}
	m = press(t, m, "h", "2", "l")
	v = strings.Join(lines(m), "\n")
	if !strings.Contains(v, "home/homelab #12") {
		t.Errorf("issue overview missing ref:\n%s", v)
	}
}

func TestDetailsScrollResetsOnItemChange(t *testing.T) {
	m := sized(t, 80, 10)
	m = press(t, m, "j", "l", "l", "G")
	if m.details.vp.YOffset() == 0 {
		t.Fatal("setup: PR overview doesn't scroll at this size")
	}
	m = press(t, m, "g", "g")
	if m.details.vp.YOffset() != 0 {
		t.Fatalf("gg: offset %d", m.details.vp.YOffset())
	}
	m = press(t, m, "G", "h", "j", "l")
	if m.details.vp.YOffset() != 0 {
		t.Fatalf("new item kept offset %d", m.details.vp.YOffset())
	}
}

func TestPendingGSemantics(t *testing.T) {
	m := sized(t, 80, 24)
	m = press(t, m, "j", "l", "j")
	if m = press(t, m, "g", "j"); m.boxes.cursor[boxCRs] != 2 {
		t.Fatalf("g j: cursor %d, want 2 (g must not act alone)", m.boxes.cursor[boxCRs])
	}
	if m = press(t, m, "g", "g"); m.boxes.cursor[boxCRs] != 0 {
		t.Fatalf("gg: cursor %d", m.boxes.cursor[boxCRs])
	}
	if m = press(t, m, "G"); m.boxes.cursor[boxCRs] != 2 {
		t.Fatalf("G: cursor %d", m.boxes.cursor[boxCRs])
	}
}

func TestReposGgAndG(t *testing.T) {
	m := sized(t, 80, 24)
	m = press(t, m, "G")
	if r, ok := m.repos.selected(); !ok || r.Name != "dotfiles" {
		t.Fatalf("G: selected %v", r)
	}
	m = press(t, m, "g", "g")
	if _, ok := m.repos.selected(); ok || m.repos.cursor != 0 {
		t.Fatalf("gg: cursor %d", m.repos.cursor)
	}
}

func TestDetailsViewportKeys(t *testing.T) {
	m := sized(t, 80, 10)
	m = press(t, m, "j", "l", "l")
	if m.details.vp.TotalLineCount() <= m.details.vp.Height()+1 {
		t.Fatal("setup: PR overview doesn't scroll at this size")
	}
	m = press(t, m, "j")
	if m.details.vp.YOffset() != 1 {
		t.Fatalf("j: offset %d, want 1", m.details.vp.YOffset())
	}
	m = press(t, m, "k")
	if m.details.vp.YOffset() != 0 {
		t.Fatalf("k: offset %d", m.details.vp.YOffset())
	}
	m = press(t, m, "ctrl+d")
	if m.details.vp.YOffset() == 0 {
		t.Fatal("ctrl+d did not scroll")
	}
	m = press(t, m, "ctrl+u")
	if m.details.vp.YOffset() != 0 {
		t.Fatalf("ctrl+u: offset %d", m.details.vp.YOffset())
	}
}

func TestHelpOverlay(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "j", "l")
	cur := m.boxes.cursor
	m = press(t, m, "?")
	v := strings.Join(lines(m), "\n")
	for _, grp := range m.keys.fullHelp() {
		for _, b := range grp {
			if !b.Enabled() {
				continue // gated action keys are hidden on purpose; TestActionKeysGated covers them
			}
			if h := b.Help(); !strings.Contains(v, h.Key) || !strings.Contains(v, h.Desc) {
				t.Errorf("help missing %q %q:\n%s", h.Key, h.Desc, v)
			}
		}
	}
	m = press(t, m, "j", "l", "2")
	if m.boxes.cursor != cur || m.level != levelBoxes || m.boxes.focus != boxCRs || !m.showHelp {
		t.Fatalf("keys acted under help: %+v level %v", m.boxes.cursor, m.level)
	}
	if m = press(t, m, "esc"); m.showHelp {
		t.Fatal("esc did not close help")
	}
	for _, k := range []string{"?", "q"} {
		m = press(t, press(t, m, "?"), k)
		if m.showHelp {
			t.Fatalf("%s did not close help", k)
		}
	}
}

func TestStatusBarHintsPerLevel(t *testing.T) {
	want := map[level][]string{
		levelRepos:   {"REPOS", "j/k repo · l enter · 1-6 jump to box · ? help"},
		levelBoxes:   {"BOXES", "j/k move · 1-6/tab box · l details · h back"},
		levelDetails: {"DETAILS", "j/k scroll · [ ] tabs · h back"},
	}
	m := sized(t, 120, 40)
	m = press(t, m, "j")
	for _, lvl := range []level{levelRepos, levelBoxes, levelDetails} {
		bar := lines(m)[39]
		for _, w := range want[lvl] {
			if !strings.Contains(bar, w) {
				t.Errorf("level %v: bar %q lacks %q", lvl, bar, w)
			}
		}
		m = press(t, m, "l")
	}
}

func TestQuit(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		_, cmd := sized(t, 80, 24).Update(keyMsg(k))
		if cmd == nil {
			t.Fatalf("%s returned no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s did not quit", k)
		}
	}
	m := press(t, sized(t, 80, 24), "?")
	next, cmd := m.Update(keyMsg("q"))
	m = next.(Model)
	if cmd != nil || m.showHelp {
		t.Fatalf("q with help open quit or left help open (showHelp %v)", m.showHelp)
	}
}

func TestRepoListScrollsToCursor(t *testing.T) {
	m := sized(t, 80, 5)
	m = press(t, m, "G")
	if row := lines(m)[2]; !strings.Contains(row, "│ dotfiles") {
		t.Errorf("cursor row not visible: %q", row)
	}
}

func TestRepoListWithoutRenovateRow(t *testing.T) {
	m := seeded(t)
	repos := []domain.Repo{{RepoRef: domain.RepoRef{Owner: "o", Name: "a"}}, {RepoRef: domain.RepoRef{Owner: "o", Name: "b"}}}
	l := repoList{noStar: true}
	l.replace(repos)
	if r, ok := l.selected(); !ok || r.Name != "a" {
		t.Fatalf("first row: %v %v", r.Name, ok)
	}
	l.setCursor(99)
	if r, ok := l.selected(); !ok || r.Name != "b" || l.cursor != 1 {
		t.Fatalf("last row: %v %v cursor %d", r.Name, ok, l.cursor)
	}
	if out := l.view(40, 6, m.svc, "PR", time.Now()); strings.Contains(out, renovateRow) || !strings.Contains(out, "a") {
		t.Fatalf("hidden row rendered or repos missing:\n%s", out)
	}
}
