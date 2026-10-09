package renovate

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

const introLine = "This PR contains the following updates:\n\n"

// body builds a PR body: introLine line, a table with the given header, and rows.
func body(header string, rows ...string) string {
	cols := strings.Count(header, "|") - 1
	sep := "|" + strings.Repeat("---|", cols)
	return introLine + header + "\n" + sep + "\n" + strings.Join(rows, "\n") + "\n\n---\n"
}

func repo(name string) domain.Repo {
	return domain.Repo{RepoRef: domain.RepoRef{Owner: "o", Name: name}}
}

func TestIsRenovateEmptyUserNeverMatchesEmptyAuthor(t *testing.T) {
	// Empty author must not match the empty configured user.
	if IsRenovate(domain.ChangeRequest{Author: "", SourceBranch: "main"}, "") {
		t.Error("empty author matched empty user")
	}
	if !IsRenovate(domain.ChangeRequest{Author: "bot", SourceBranch: "main"}, "bot") {
		t.Error("author match with unrelated branch should be true")
	}
	if IsRenovate(domain.ChangeRequest{Author: "bot", SourceBranch: "renovate"}, "other") {
		t.Error("branch without the renovate/ prefix matched")
	}
}

func TestParseIgnoredColumns(t *testing.T) {
	for _, col := range []string{"Pending", "References", "File", "Age", "Adoption", "Passing", "Confidence"} {
		b := body("| Package | Update | Change | "+col+" |", "| a | minor | `1` → `2` | cell |")
		us, ok := Parse("x", b)
		if !ok || len(us) != 1 || us[0].Package != "a" || us[0].From != "1" || us[0].To != "2" {
			t.Errorf("column %s: got %+v ok=%v", col, us, ok)
		}
	}
}

func TestParseRejectRules(t *testing.T) {
	hdr := "| Package | Update | Change |"
	row := "| a | major | `1` → `2` |"
	cases := map[string]string{
		"missing Package":    body("| Update | Change |", "| major | `1` → `2` |"),
		"missing Update":     body("| Package | Change |", "| a | `1` → `2` |"),
		"missing Change":     body("| Package | Update | Type |", "| a | major | final |"),
		"ascii arrow":        body(hdr, "| a | major | `1` -> `2` |"),
		"from not backtick":  body(hdr, "| a | major | 1 → `2` |"),
		"to not backtick":    body(hdr, "| a | major | `1` → 2 |"),
		"empty to":           body(hdr, "| a | major | `1` → `` |"),
		"extra text":         body(hdr, "| a | major | `1` → `2` later |"),
		"too wide row":       body(hdr, "| a | major | `1` → `2` | extra |"),
		"one bad of two":     body(hdr, row, "| b | major | `1` to `2` |"),
		"unknown with valid": body("| Package | Update | Change | Notes |", "| a | major | `1` → `2` | n |"),
		"empty body":         "",
		"intro only":         introLine,
	}
	for name, b := range cases {
		if us, ok := Parse("Update a to v2", b); ok || us != nil {
			t.Errorf("%s: accepted %+v", name, us)
		}
	}
}

func TestParseStopsAtFirstNonTableLine(t *testing.T) {
	b := introLine + "| Package | Update | Change |\n|---|---|---|\n| a | major | `1` → `2` |\n\nprose\n\n| b | major | `3` → `4` |\n"
	us, ok := Parse("x", b)
	if !ok || len(us) != 1 || us[0].Package != "a" {
		t.Errorf("got %+v ok=%v, want only row a", us, ok)
	}
}

func TestParsePackageCellForms(t *testing.T) {
	cases := []struct {
		cell, pkg, src string
	}{
		{"[a/b](https://pkg.example/a)", "a/b", "https://pkg.example/a"},
		{"[a/b](https://pkg.example/a) ([source](https://src.example/a))", "a/b", "https://src.example/a"},
		{"plain-name", "plain-name", ""},
		{"`ticked`", "ticked", ""},
	}
	for _, c := range cases {
		us, ok := Parse("x", body("| Package | Update | Change |", "| "+c.cell+" | Minor | `1` → `2` |"))
		if !ok || len(us) != 1 {
			t.Fatalf("%q: got %+v ok=%v", c.cell, us, ok)
		}
		if us[0].Package != c.pkg || us[0].SourceURL != c.src {
			t.Errorf("%q: package %q source %q, want %q %q", c.cell, us[0].Package, us[0].SourceURL, c.pkg, c.src)
		}
		if us[0].UpdateType != "minor" {
			t.Errorf("%q: UpdateType %q, want lowercased minor", c.cell, us[0].UpdateType)
		}
	}
}

func TestParseDepTypeColumn(t *testing.T) {
	with, _ := Parse("x", body("| Package | Type | Update | Change |", "| a | devDependencies | minor | `1` → `2` |"))
	without, _ := Parse("x", body("| Package | Update | Change |", "| a | minor | `1` → `2` |"))
	if len(with) != 1 || with[0].DepType != "devDependencies" {
		t.Errorf("DepType with Type column: %+v", with)
	}
	if len(without) != 1 || without[0].DepType != "" {
		t.Errorf("DepType without Type column: %+v", without)
	}
}

func TestParseEcosystemFromTypeCell(t *testing.T) {
	want := map[string]string{
		"action": "github-actions", "uses-with": "github-actions",
		"final": "docker", "stage": "docker",
		"require": "go", "indirect": "go", "toolchain": "go",
		"dependencies": "npm", "devDependencies": "npm", "peerDependencies": "npm", "optionalDependencies": "npm",
	}
	for typ, eco := range want {
		us, ok := Parse("x", body("| Package | Type | Update | Change |", "| a | "+typ+" | minor | `1` → `2` |"))
		if !ok || len(us) != 1 || us[0].Ecosystem != eco {
			t.Errorf("Type %q: got %+v ok=%v, want ecosystem %s", typ, us, ok, eco)
		}
	}
}

func TestParseEcosystemFromTitleNoun(t *testing.T) {
	single := body("| Package | Update | Change |", "| a | minor | `1` → `2` |")
	cases := []struct{ title, eco string }{
		{"Update a docker tag to v2", "docker"},
		{"Update Docker Tag a to v2", "docker"},
		{"Update a docker digest", "docker"},
		{"Update a docker image to v2", "docker"},
		{"Update a action to v2", "github-actions"},
		{"Update a module to v2", "go"},
		{"Update a helm release to v2", "helm"},
		{"Update dependency a to v2", ""},
		{"Update reactions-lib to v2", ""},
	}
	for _, c := range cases {
		us, ok := Parse(c.title, single)
		if !ok || len(us) != 1 || us[0].Ecosystem != c.eco {
			t.Errorf("%q: got %+v ok=%v, want ecosystem %q", c.title, us, ok, c.eco)
		}
	}
}

func TestParseTitleNounIgnoredForMultipleRows(t *testing.T) {
	b := body("| Package | Update | Change |", "| a | minor | `1` → `2` |", "| b | minor | `3` → `4` |")
	us, ok := Parse("Update docker tag", b)
	if !ok || len(us) != 2 {
		t.Fatalf("got %+v ok=%v", us, ok)
	}
	for _, u := range us {
		if u.Ecosystem != "" {
			t.Errorf("%s: ecosystem %q, want unknown for multi-row body without Type", u.Package, u.Ecosystem)
		}
	}
}

func TestParseTypeCellWinsOverTitleNoun(t *testing.T) {
	b := body("| Package | Type | Update | Change |", "| a | require | minor | `1` → `2` |")
	us, ok := Parse("Update a docker tag", b)
	if !ok || len(us) != 1 || us[0].Ecosystem != "go" {
		t.Errorf("got %+v ok=%v, want go from Type cell", us, ok)
	}
}

func TestParseUnusableTypeFallsBackToTitleNoun(t *testing.T) {
	b := body("| Package | Type | Update | Change |", "| a | somethingElse | minor | `1` → `2` |")
	us, ok := Parse("Update a docker tag to v2", b)
	if !ok || len(us) != 1 || us[0].Ecosystem != "docker" {
		t.Errorf("got %+v ok=%v, want docker via title noun", us, ok)
	}
}

func TestIsDashboard(t *testing.T) {
	cases := []struct {
		name string
		is   domain.Issue
		user string
		want bool
	}{
		{"title exact", domain.Issue{Title: "Dependency Dashboard"}, "", true},
		{"title trimmed and cased", domain.Issue{Title: "  dependency DASHBOARD\n"}, "", true},
		{"author and body", domain.Issue{Title: "Renovate", Author: "Bot", Body: "This issue lists Renovate updates"}, "bot", true},
		{"author mismatch", domain.Issue{Title: "Renovate", Author: "someone", Body: "This issue lists Renovate updates"}, "bot", false},
		{"body mismatch", domain.Issue{Title: "Renovate", Author: "bot", Body: "hello"}, "bot", false},
		{"empty user body only", domain.Issue{Title: "Renovate", Author: "", Body: "This issue lists Renovate updates"}, "", false},
		{"ordinary issue", domain.Issue{Title: "Crash on start", Author: "bot", Body: "boom"}, "bot", false},
		{"title substring", domain.Issue{Title: "Dependency Dashboard v2"}, "", false},
	}
	for _, c := range cases {
		if got := IsDashboard(c.is, c.user); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseDashboardRules(t *testing.T) {
	b := strings.Join([]string{
		"This issue lists Renovate updates.",
		" - [ ] <!-- orphan-branch=renovate/orphan -->Before any heading",
		"## Rate-Limited",
		"  - [x] <!-- approve-branch=renovate/foo -->Update [lib](https://x.example/lib) to v2  ",
		" - [X] Checked uppercase, no branch",
		" - plain bullet is not an entry",
		" * [ ] star bullet is not an entry",
		"## Open",
		" - [ ] <!-- unschedule-branch=renovate/bar -->Update bar <!-- stray -->to v3",
		"## Detected Dependencies",
		" - [ ] <!-- unschedule-branch=renovate/ignored -->never read",
		"## After",
		" - [ ] also never read",
	}, "\n")
	want := []Entry{
		{Section: "", Title: "Before any heading", Branch: "renovate/orphan"},
		{Section: "Rate-Limited", Title: "Update lib to v2", Branch: "renovate/foo", Checked: true},
		{Section: "Rate-Limited", Title: "Checked uppercase, no branch", Checked: true},
		{Section: "Open", Title: "Update bar to v3", Branch: "renovate/bar"},
	}
	got := ParseDashboard(b)
	if !slices.Equal(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseDashboardCapturedFixtures(t *testing.T) {
	adv := ParseDashboard(read(t, "dashboard_adventure_3.md"))
	if len(adv) != 1 || adv[0].Section != "PR Closed (Blocked)" || adv[0].Branch != "renovate/postgis-postgis-17.x" ||
		adv[0].Title != "Update postgis/postgis Docker tag to v17" || adv[0].Checked {
		t.Errorf("adventure: %+v", adv)
	}
	tra := ParseDashboard(read(t, "dashboard_traefik_3.md"))
	if len(tra) != 1 || tra[0].Section != "Awaiting Schedule" || tra[0].Branch != "renovate/docker-images-non-major" {
		t.Errorf("traefik: %+v", tra)
	}
	if got := ParseDashboard("no checkboxes here"); len(got) != 0 {
		t.Errorf("no entries expected, got %+v", got)
	}
}

func TestImpactFormula(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct{ hours, want float64 }{
		{12, 1.584962500721156},
		{24, 2},
		{72, 3},
		{7 * 24, 4},
		{40 * 24, 6.357552004618084},
	} {
		opened := now.Add(-time.Duration(c.hours * float64(time.Hour)))
		if got := Impact(opened, now); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%vh: got %v, want %v", c.hours, got, c.want)
		}
	}
}

func TestBuildEmpty(t *testing.T) {
	v := Build(nil, time.Now())
	if v.Totals.PRs != 0 || len(v.ByRepo) != 0 || len(v.Groups) != 0 || len(v.PRs) != 0 || len(v.CI) != 0 || len(v.Dashboards) != 0 {
		t.Errorf("non-empty view from no scans: %+v", v)
	}
}

func up(eco, pkg, typ, from, to string) domain.RenovateUpdate {
	return domain.RenovateUpdate{Ecosystem: eco, Package: pkg, UpdateType: typ, From: from, To: to}
}

func TestBuildCountingBuckets(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	r := repo("a")
	cr := func(n int, us ...domain.RenovateUpdate) domain.ChangeRequest {
		return domain.ChangeRequest{Number: n, Renovate: us}
	}
	scans := []RepoScan{{Repo: r, PRs: []domain.ChangeRequest{
		cr(1, up("go", "x", "patch", "1", "2"), up("go", "y", "major", "1", "2"), up("go", "z", "minor", "1", "2")),
		cr(2, up("go", "x", "patch", "1", "2"), up("go", "y", "minor", "1", "2")),
		cr(3, up("go", "x", "patch", "1", "2")),
		cr(4, up("docker", "p", "digest", "a", "b")),
		cr(5, up("docker", "p", "pin", "a", "b")),
		cr(6, up("docker", "p", "weird", "a", "b")),
		cr(7),
	}}}
	v := Build(scans, now)
	if len(v.ByRepo) != 1 {
		t.Fatalf("ByRepo = %+v", v.ByRepo)
	}
	got := v.ByRepo[0]
	if got.Repo != r.RepoRef || got.PRs != 7 || got.Major != 1 || got.Minor != 1 || got.Patch != 1 || got.Other != 4 {
		t.Errorf("summary = %+v", got)
	}
	if v.Totals.Repo != (domain.RepoRef{}) || v.Totals.PRs != 7 || v.Totals.Major != 1 || v.Totals.Minor != 1 || v.Totals.Patch != 1 || v.Totals.Other != 4 {
		t.Errorf("totals = %+v", v.Totals)
	}
}

func TestBuildImpactSumsAndSorts(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	mk := func(n, days int) domain.ChangeRequest {
		return domain.ChangeRequest{Number: n, CreatedAt: now.AddDate(0, 0, -days), Renovate: []domain.RenovateUpdate{up("go", "x", "major", "1", "2")}}
	}
	scans := []RepoScan{
		{Repo: repo("young"), PRs: []domain.ChangeRequest{mk(1, 1)}},
		{Repo: repo("old"), PRs: []domain.ChangeRequest{mk(1, 7), mk(2, 3)}},
		{Repo: repo("zero"), PRs: []domain.ChangeRequest{{Number: 1}}},
		{Repo: repo("empty")},
		{Repo: repo("future"), PRs: []domain.ChangeRequest{{Number: 1, CreatedAt: now.Add(48 * time.Hour)}}},
	}
	v := Build(scans, now)
	var names []string
	for _, s := range v.ByRepo {
		names = append(names, s.Repo.Name)
	}
	if !slices.Equal(names, []string{"old", "young", "future", "zero"}) {
		t.Fatalf("ByRepo order = %v (repos without PRs omitted; impact desc then repo asc)", names)
	}
	if want := (1 + math.Log2(8)) + (1 + math.Log2(4)); math.Abs(v.ByRepo[0].Impact-want) > 1e-9 {
		t.Errorf("old impact = %v, want %v", v.ByRepo[0].Impact, want)
	}
	if math.Abs(v.ByRepo[1].Impact-2) > 1e-9 {
		t.Errorf("young impact = %v, want 2", v.ByRepo[1].Impact)
	}
	if v.ByRepo[2].Impact != 1 || v.ByRepo[3].Impact != 1 {
		t.Errorf("zero/future CreatedAt impact = %v, %v, want 1", v.ByRepo[2].Impact, v.ByRepo[3].Impact)
	}
	var sum float64
	for _, s := range v.ByRepo {
		sum += s.Impact
	}
	if math.Abs(v.Totals.Impact-sum) > 1e-9 {
		t.Errorf("Totals.Impact = %v, want %v", v.Totals.Impact, sum)
	}
}

func TestBuildGroupFieldsAndOrder(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	mk := func(n int, title string, us ...domain.RenovateUpdate) domain.ChangeRequest {
		return domain.ChangeRequest{Number: n, Title: title, Renovate: us}
	}
	scans := []RepoScan{
		{Repo: repo("a"), PRs: []domain.ChangeRequest{
			mk(1, "t", up("docker", "pg", "minor", "16", "17")),
			mk(2, "t", up("docker", "zzz", "patch", "1", "2")),
			mk(3, "t", up("", "mystery", "minor", "1", "2")),
		}},
		{Repo: repo("b"), PRs: []domain.ChangeRequest{
			mk(1, "t", up("docker", "pg", "major", "15", "17")),
			mk(2, "t", up("docker", "aaa", "patch", "1", "2")),
			mk(3, "t", up("", "mystery", "minor", "1", "2")),
			mk(4, "batch title", up("go", "x", "patch", "1", "2"), up("go", "y", "patch", "1", "2")),
		}},
		{Repo: repo("c"), PRs: []domain.ChangeRequest{
			mk(1, "t", up("docker", "pg", "patch", "16", "17")),
			mk(2, "t", up("docker", "pg", "patch", "16", "17")),
		}},
	}
	v := Build(scans, now)

	var pg *Group
	for i, g := range v.Groups {
		if g.Key == (GroupKey{"docker", "pg", "17"}) {
			pg = &v.Groups[i]
		}
	}
	if pg == nil {
		t.Fatalf("pg group missing: %+v", v.Groups)
	}
	if len(pg.Members) != 4 || pg.Batched || pg.Label != "pg → 17" || pg.UpdateType != "major" {
		t.Errorf("pg group = %+v", *pg)
	}
	if !slices.Equal(pg.Froms, []string{"15", "16"}) {
		t.Errorf("Froms = %v, want distinct sorted [15 16]", pg.Froms)
	}
	for _, m := range pg.Members {
		if m.Repo.Name == "" {
			t.Errorf("member without repo: %+v", m)
		}
	}

	var labels []string
	for _, g := range v.Groups {
		labels = append(labels, g.Label)
	}
	want := []string{"pg → 17", "aaa → 2", "batch title", "mystery → 2", "mystery → 2", "zzz → 2"}
	if !slices.Equal(labels, want) {
		t.Errorf("labels = %v, want %v", labels, want)
	}

	for _, g := range v.Groups {
		switch g.Label {
		case "mystery → 2":
			if g.Key != (GroupKey{}) || g.Batched || len(g.Members) != 1 {
				t.Errorf("unknown-ecosystem row = %+v", g)
			}
		case "batch title":
			if g.Key != (GroupKey{}) || !g.Batched || g.Froms != nil || len(g.Members) != 1 {
				t.Errorf("batched row = %+v", g)
			}
		}
	}
	if v.Groups[3].Members[0].Repo.Name != "a" || v.Groups[4].Members[0].Repo.Name != "b" {
		t.Errorf("tie on label not broken by repo: %v, %v", v.Groups[3].Members[0].Repo, v.Groups[4].Members[0].Repo)
	}
}

func TestBuildGroupUpdateTypeRanking(t *testing.T) {
	now := time.Now()
	cases := []struct {
		types []string
		want  string
	}{
		{[]string{"patch", "minor"}, "minor"},
		{[]string{"minor", "patch", "major"}, "major"},
		{[]string{"digest", "patch"}, "patch"},
		{[]string{"pin", "minor"}, "minor"},
	}
	for _, c := range cases {
		var prs []domain.ChangeRequest
		for i, typ := range c.types {
			prs = append(prs, domain.ChangeRequest{Number: i + 1, Renovate: []domain.RenovateUpdate{up("docker", "pg", typ, "1", "2")}})
		}
		v := Build([]RepoScan{{Repo: repo("a"), PRs: prs}}, now)
		if len(v.Groups) != 1 || v.Groups[0].UpdateType != c.want {
			t.Errorf("%v: got %+v, want %s", c.types, v.Groups, c.want)
		}
	}
}

func TestBuildFromIsNotPartOfKey(t *testing.T) {
	prs := []domain.ChangeRequest{
		{Number: 1, Renovate: []domain.RenovateUpdate{up("go", "x", "minor", "1", "2")}},
		{Number: 2, Renovate: []domain.RenovateUpdate{up("go", "x", "minor", "1.5", "2")}},
		{Number: 3, Renovate: []domain.RenovateUpdate{up("go", "x", "minor", "1", "3")}},
	}
	v := Build([]RepoScan{{Repo: repo("a"), PRs: prs}}, time.Now())
	if len(v.Groups) != 2 || len(v.Groups[0].Members) != 2 || v.Groups[0].Key.To != "2" {
		t.Errorf("groups = %+v, want To=2 group of 2 then To=3", v.Groups)
	}
}

func TestBuildPRsAndCIOrdering(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	day := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	mk := func(n int, created time.Time, ci domain.CIState) domain.ChangeRequest {
		return domain.ChangeRequest{Number: n, CreatedAt: created, CI: ci, Renovate: []domain.RenovateUpdate{up("go", "x", "patch", "1", "2")}}
	}
	scans := []RepoScan{
		{Repo: repo("b"), PRs: []domain.ChangeRequest{mk(2, day(9), domain.CIFail), mk(1, day(9), domain.CIFail), mk(3, day(1), domain.CIPass)}},
		{Repo: repo("a"), PRs: []domain.ChangeRequest{mk(9, day(5), domain.CIPending), mk(4, day(2), domain.CIRunning)}},
	}
	v := Build(scans, now)
	type key struct {
		r string
		n int
	}
	keys := func(ms []Member) []key {
		var out []key
		for _, m := range ms {
			out = append(out, key{m.Repo.String(), m.CR.Number})
		}
		return out
	}
	wantPRs := []key{{"o/b", 1}, {"o/b", 2}, {"o/a", 9}, {"o/a", 4}, {"o/b", 3}}
	if got := keys(v.PRs); !slices.Equal(got, wantPRs) {
		t.Errorf("PRs = %v, want %v", got, wantPRs)
	}
	wantCI := []key{{"o/b", 1}, {"o/b", 2}, {"o/a", 9}, {"o/a", 4}}
	if got := keys(v.CI); !slices.Equal(got, wantCI) {
		t.Errorf("CI = %v, want %v", got, wantCI)
	}
}

func TestBuildCIBoxIsExactlyNonGreen(t *testing.T) {
	states := map[domain.CIState]bool{
		domain.CINone: false, domain.CIPass: false, domain.CISkipped: false,
		domain.CIPending: true, domain.CIRunning: true, domain.CIFail: true, domain.CICancelled: true,
	}
	for st, inCI := range states {
		cr := domain.ChangeRequest{Number: 1, CI: st, Renovate: []domain.RenovateUpdate{up("go", "x", "patch", "1", "2")}}
		v := Build([]RepoScan{{Repo: repo("a"), PRs: []domain.ChangeRequest{cr}}}, time.Now())
		if got := len(v.CI) == 1; got != inCI {
			t.Errorf("CI state %v: in box = %v, want %v", st, got, inCI)
		}
		if len(v.PRs) != 1 {
			t.Errorf("CI state %v: PRs = %d, want 1", st, len(v.PRs))
		}
	}
}

func TestBuildRejectedBodyKeptOutOfGroupsOnly(t *testing.T) {
	cr := domain.ChangeRequest{Number: 1, Title: "broken", CI: domain.CIFail}
	v := Build([]RepoScan{{Repo: repo("a"), PRs: []domain.ChangeRequest{cr}}}, time.Now())
	if len(v.Groups) != 0 {
		t.Errorf("Groups = %+v, want none", v.Groups)
	}
	if len(v.PRs) != 1 || len(v.CI) != 1 || len(v.ByRepo) != 1 || v.ByRepo[0].Other != 1 || v.ByRepo[0].PRs != 1 {
		t.Errorf("view = %+v", v)
	}
}

func TestBuildDashboardsSortedByRepo(t *testing.T) {
	issue := domain.Issue{Number: 3, Title: "Dependency Dashboard"}
	entries := []Entry{{Section: "S", Title: "t"}}
	scans := []RepoScan{
		{Repo: repo("b"), Dashboards: []Dashboard{{Repo: repo("b").RepoRef, Issue: issue, Entries: entries}}},
		{Repo: repo("a"), Dashboards: []Dashboard{{Repo: repo("a").RepoRef, Issue: issue, Entries: entries}}},
		{Repo: repo("c")},
	}
	v := Build(scans, time.Now())
	if len(v.Dashboards) != 2 || v.Dashboards[0].Repo.Name != "a" || v.Dashboards[1].Repo.Name != "b" {
		t.Fatalf("Dashboards = %+v", v.Dashboards)
	}
	if v.Dashboards[0].Issue.Number != 3 || !slices.Equal(v.Dashboards[0].Entries, entries) {
		t.Errorf("dashboard content lost: %+v", v.Dashboards[0])
	}
}

func TestBuildDeterministicAcrossScanOrder(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	mk := func(n, age int, ci domain.CIState, us ...domain.RenovateUpdate) domain.ChangeRequest {
		return domain.ChangeRequest{Number: n, Title: "t", CI: ci, CreatedAt: now.AddDate(0, 0, -age), Renovate: us}
	}
	scans := []RepoScan{
		{Repo: repo("c"), PRs: []domain.ChangeRequest{mk(1, 2, domain.CIFail, up("docker", "pg", "minor", "16", "17")), mk(2, 2, domain.CIPass)}},
		{Repo: repo("a"), PRs: []domain.ChangeRequest{mk(1, 2, domain.CIPass, up("docker", "pg", "major", "15", "17")), mk(2, 5, domain.CIRunning, up("", "m", "minor", "1", "2"))}},
		{Repo: repo("b"), PRs: []domain.ChangeRequest{mk(1, 2, domain.CIPass, up("docker", "pg", "patch", "14", "17"))}},
	}
	want := Build(scans, now)
	for _, perm := range [][]int{{0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		shuffled := []RepoScan{scans[perm[0]], scans[perm[1]], scans[perm[2]]}
		if got := Build(shuffled, now); !reflect.DeepEqual(got, want) {
			t.Errorf("order %v changed the view:\n got %+v\nwant %+v", perm, got, want)
		}
	}
}
