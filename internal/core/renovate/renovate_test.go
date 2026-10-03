package renovate

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

const fixtures = "../testdata/renovate"

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestIsRenovate(t *testing.T) {
	cases := []struct {
		author, branch, user string
		want                 bool
	}{
		{"renovate-bot", "feature/x", "renovate-bot", true},
		{"RENOVATE-BOT", "feature/x", "renovate-bot", true},
		{"someone", "renovate/node-24.x", "renovate-bot", true},
		{"someone", "renovate/node-24.x", "", true},
		{"someone", "feature/x", "", false},
		{"", "feature/x", "", false},
	}
	for _, c := range cases {
		if got := IsRenovate(domain.ChangeRequest{Author: c.author, SourceBranch: c.branch}, c.user); got != c.want {
			t.Errorf("IsRenovate(%q, %q, user %q) = %v, want %v", c.author, c.branch, c.user, got, c.want)
		}
	}
}

func TestParseCapturedFixtures(t *testing.T) {
	var prs []struct{ Title, Category, File string }
	if err := json.Unmarshal([]byte(read(t, "prs.json")), &prs); err != nil {
		t.Fatal(err)
	}
	for _, p := range prs {
		us, ok := Parse(p.Title, read(t, p.File))
		if p.Category == "dependency-dashboard" {
			if ok || us != nil {
				t.Errorf("%s: dashboard parsed as PR", p.File)
			}
			continue
		}
		if !ok || len(us) == 0 {
			t.Errorf("%s: rejected", p.File)
		}
	}
}

func TestParseValues(t *testing.T) {
	want := func(file, title string, w ...domain.RenovateUpdate) {
		t.Helper()
		got, ok := Parse(title, read(t, file))
		if !ok || !slices.Equal(got, w) {
			t.Errorf("%s: got %+v ok=%v, want %+v", file, got, ok, w)
		}
	}
	want("multi-manager-major_forgejo_9.md", "chore(deps): update dependency node to v24",
		domain.RenovateUpdate{Ecosystem: "github-actions", Package: "node", DepType: "uses-with", UpdateType: "major", From: "20", To: "24", SourceURL: "https://github.com/actions/node-versions"},
		domain.RenovateUpdate{Ecosystem: "docker", Package: "node", DepType: "final", UpdateType: "major", From: "22-bookworm", To: "24-bookworm", SourceURL: "https://github.com/nodejs/node"})
	want("github-actions-major_home_2.md", "Update actions/checkout action to v7",
		domain.RenovateUpdate{Ecosystem: "github-actions", Package: "actions/checkout", DepType: "action", UpdateType: "major", From: "v4", To: "v7", SourceURL: "https://github.com/actions/checkout"})
	want("docker-compose-major_adventure_2.md", "Update postgis/postgis Docker tag to v17",
		domain.RenovateUpdate{Ecosystem: "docker", Package: "postgis/postgis", UpdateType: "major", From: "16-3.5", To: "17-3.5", SourceURL: "https://github.com/postgis/docker-postgis"})
	want("docker-group-nonmajor_forgejo_13.md", "chore(deps): update codeberg.org/forgejo/forgejo docker tag to v16.0.5",
		domain.RenovateUpdate{Ecosystem: "docker", Package: "codeberg.org/forgejo/forgejo", UpdateType: "patch", From: "16.0.3", To: "16.0.5", SourceURL: "https://codeberg.org/forgejo/forgejo"})

	if us, ok := Parse("Update dependency node to v24", "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [node](https://x) | major | `22` → `24` |\n"); !ok || us[0].Ecosystem != "" {
		t.Errorf("unknown ecosystem: %+v ok=%v", us, ok)
	}
	if us, ok := Parse("chore: update go deps", read(t, "batched-gomod-nonmajor.md")); !ok || len(us) != 3 || us[0].Ecosystem != "go" || us[2].Ecosystem != "go" {
		t.Errorf("batched gomod: %+v ok=%v", us, ok)
	}
	for _, f := range []string{"confidence-columns.md", "digest-docker.md"} {
		if us, ok := Parse("x", read(t, f)); !ok || len(us) != 1 {
			t.Errorf("%s: %+v ok=%v", f, us, ok)
		}
	}
}

func TestParseRejects(t *testing.T) {
	const tbl = "| Package | Update | Change |\n|---|---|---|\n"
	bodies := map[string]string{
		"no intro":       tbl + "| a | major | `1` → `2` |\n",
		"width mismatch": "This PR contains the following updates:\n\n" + tbl + "| a | major |\n",
		"zero rows":      "This PR contains the following updates:\n\n" + tbl,
		"no table":       "This PR contains the following updates:\n\nnothing here\n",
		"missing column": "This PR contains the following updates:\n\n| Package | Update |\n|---|---|\n| a | major |\n",
		"empty from":     "This PR contains the following updates:\n\n" + tbl + "| a | major | `` → `2` |\n",
		"unknown column": read(t, "rejected-unknown-column.md"),
		"no arrow":       read(t, "rejected-no-arrow.md"),
	}
	for name, b := range bodies {
		if us, ok := Parse("x", b); ok || us != nil {
			t.Errorf("%s: accepted: %+v", name, us)
		}
	}
}

func TestDashboard(t *testing.T) {
	got := ParseDashboard(read(t, "dashboard_jellyfin_3.md"))
	want := []Entry{{Section: "Awaiting Schedule", Title: "Update jellyfin/jellyfin Docker tag to v12.1", Branch: "renovate/docker-images-non-major"}}
	if !slices.Equal(got, want) {
		t.Errorf("entries = %+v, want %+v", got, want)
	}
	if got := ParseDashboard("## A\n- [x] done\n## Detected Dependencies\n- [ ] ignored\n"); len(got) != 1 || !got[0].Checked || got[0].Section != "A" {
		t.Errorf("checked/stop: %+v", got)
	}
	if !IsDashboard(domain.Issue{Title: " dependency dashboard "}, "") || IsDashboard(domain.Issue{Title: "Bug", Author: "x"}, "x") {
		t.Error("IsDashboard by title")
	}
	if !IsDashboard(domain.Issue{Title: "Renovate", Author: "Bot", Body: "This issue lists Renovate updates and x"}, "bot") {
		t.Error("IsDashboard by author and body")
	}
}

func TestImpact(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		opened time.Time
		want   float64
	}{
		{now, 1}, {now.AddDate(0, 0, -1), 2}, {now.AddDate(0, 0, -7), 4}, {time.Time{}, 1}, {now.Add(time.Hour), 1},
	} {
		if got := Impact(c.opened, now); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Impact(%v) = %v, want %v", c.opened, got, c.want)
		}
	}
}

func TestBuild(t *testing.T) {
	now := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	upd := func(eco, pkg, typ, from, to string) domain.RenovateUpdate {
		return domain.RenovateUpdate{Ecosystem: eco, Package: pkg, UpdateType: typ, From: from, To: to}
	}
	pr := func(n int, age int, ci domain.CIState, title string, us ...domain.RenovateUpdate) domain.ChangeRequest {
		return domain.ChangeRequest{Number: n, Title: title, CI: ci, CreatedAt: now.AddDate(0, 0, -age), Renovate: us}
	}
	a, b := domain.Repo{RepoRef: domain.RepoRef{Owner: "o", Name: "a"}}, domain.Repo{RepoRef: domain.RepoRef{Owner: "o", Name: "b"}}
	scans := []RepoScan{
		{Repo: a, PRs: []domain.ChangeRequest{
			pr(1, 1, domain.CIPass, "co", upd("github-actions", "actions/checkout", "major", "v3", "v7")),
			pr(2, 7, domain.CIFail, "node-gha", upd("github-actions", "node", "major", "20", "24")),
			pr(3, 3, domain.CIPass, "rejected"),
		}},
		{Repo: b, PRs: []domain.ChangeRequest{
			pr(1, 1, domain.CIRunning, "co", upd("github-actions", "actions/checkout", "major", "v4", "v7")),
			pr(2, 2, domain.CIPass, "node-docker", upd("docker", "node", "major", "22", "24")),
			pr(3, 5, domain.CIPass, "go batch", upd("go", "x", "patch", "1", "2"), upd("go", "y", "minor", "1", "2")),
			pr(4, 9, domain.CIPass, "mystery", upd("", "foo", "minor", "1", "2")),
		}},
	}
	v := Build(scans, now)
	rev := Build([]RepoScan{scans[1], scans[0]}, now)
	if !slices.EqualFunc(v.Groups, rev.Groups, func(x, y Group) bool { return x.Label == y.Label && x.Key == y.Key && len(x.Members) == len(y.Members) }) ||
		!slices.EqualFunc(v.ByRepo, rev.ByRepo, func(x, y RepoSummary) bool { return x == y }) {
		t.Error("Build depends on scan order")
	}

	var co *Group
	keys := map[GroupKey]int{}
	batched := 0
	for i, g := range v.Groups {
		keys[g.Key]++
		if g.Key.Package == "actions/checkout" {
			co = &v.Groups[i]
		}
		if g.Batched {
			batched++
			if len(g.Members) != 1 || g.Froms != nil {
				t.Errorf("batched row %+v", g)
			}
		}
	}
	if co == nil || len(co.Members) != 2 || !slices.Equal(co.Froms, []string{"v3", "v4"}) {
		t.Errorf("checkout group = %+v", co)
	}
	if keys[GroupKey{"github-actions", "node", "24"}] != 1 || keys[GroupKey{"docker", "node", "24"}] != 1 {
		t.Errorf("node groups = %v", keys)
	}
	if batched != 1 || len(v.Groups) != 5 {
		t.Errorf("batched=%d groups=%d, want 1 and 5", batched, len(v.Groups))
	}
	if v.Groups[0].Key.Package != "actions/checkout" {
		t.Errorf("largest group not first: %+v", v.Groups[0])
	}
	for _, g := range v.Groups {
		for _, m := range g.Members {
			if m.CR.Title == "rejected" {
				t.Error("rejected PR in Groups")
			}
		}
	}

	if len(v.PRs) != 7 {
		t.Errorf("PRs = %d, want 7", len(v.PRs))
	}
	for i := 1; i < len(v.PRs); i++ {
		if v.PRs[i-1].CR.CreatedAt.After(v.PRs[i].CR.CreatedAt) {
			t.Error("PRs not oldest first")
		}
	}
	var ci []string
	for _, m := range v.CI {
		ci = append(ci, m.CR.Title)
	}
	if !slices.Equal(ci, []string{"node-gha", "co"}) {
		t.Errorf("CI = %v", ci)
	}

	sum := RepoSummary{}
	for _, r := range v.ByRepo {
		if r.Major+r.Minor+r.Patch+r.Other != r.PRs {
			t.Errorf("%v buckets don't sum to PRs: %+v", r.Repo, r)
		}
		sum.PRs += r.PRs
		sum.Other += r.Other
		sum.Impact += r.Impact
	}
	if v.Totals.PRs != 7 || v.Totals.PRs != sum.PRs || v.Totals.Other != sum.Other || v.Totals.Impact != sum.Impact || sum.Other != 1 {
		t.Errorf("totals %+v vs summed %+v", v.Totals, sum)
	}
	if len(v.ByRepo) != 2 || v.ByRepo[0].Impact < v.ByRepo[1].Impact {
		t.Errorf("ByRepo order: %+v", v.ByRepo)
	}
}
