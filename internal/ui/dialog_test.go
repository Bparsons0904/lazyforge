package ui

import (
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func dialogText(d *dialog) string { return strip(strings.Join(d.lines(), "\n")) }

func target(r domain.RepoRef, n int, ci domain.CIState) core.Target {
	return core.Target{Repo: r, CR: domain.ChangeRequest{Number: n, Title: "t", CI: ci, SourceBranch: "renovate/x"}}
}

// Seam: dedup is by (repo, number), keeping the first occurrence in order.
func TestMergeDialogDedupsByRepoAndNumber(t *testing.T) {
	d := mergeDialog([]core.Target{target(repoA, 1, domain.CIPass), target(repoB, 1, domain.CIPass), target(repoA, 1, domain.CIPass), target(repoA, 2, domain.CIPass)}, mergeOpts{})
	if len(d.targets) != 3 {
		t.Fatalf("kept %d targets, want 3", len(d.targets))
	}
	if d.targets[0].Repo != repoA || d.targets[1].Repo != repoB || d.targets[2].CR.Number != 2 {
		t.Errorf("order not preserved: %+v", d.targets)
	}
}

// Seam: strategy line. Empty means the repo's default, several repos mean each repo's own, an explicit one wins.
func TestMergeDialogStrategyLine(t *testing.T) {
	one := []core.Target{target(repoA, 1, domain.CIPass)}
	two := []core.Target{target(repoA, 1, domain.CIPass), target(repoB, 1, domain.CIPass)}
	for _, tc := range []struct {
		name string
		ts   []core.Target
		o    mergeOpts
		want string
	}{
		{"single repo", one, mergeOpts{}, "Strategy: repo default"},
		{"several repos", two, mergeOpts{}, "Strategy: each repo's default"},
		{"explicit", two, mergeOpts{strategy: "squash"}, "Strategy: squash"},
	} {
		if v := dialogText(mergeDialog(tc.ts, tc.o)); !strings.Contains(v, tc.want) {
			t.Errorf("%s: want %q in:\n%s", tc.name, tc.want, v)
		}
	}
}

// Seam: repo prefix. Several repos or a ★ merge prefix each target; a plain single-repo merge doesn't.
func TestMergeDialogRepoPrefix(t *testing.T) {
	one := []core.Target{target(repoA, 1, domain.CIPass)}
	if v := dialogText(mergeDialog(one, mergeOpts{})); strings.Contains(v, "o/a") || !strings.Contains(v, "#1 t") {
		t.Errorf("single-repo dialog changed:\n%s", v)
	}
	if v := dialogText(mergeDialog(one, mergeOpts{star: true})); !strings.Contains(v, "o/a #1 t") {
		t.Errorf("★ dialog lacks the repo prefix:\n%s", v)
	}
	two := mergeDialog([]core.Target{target(repoA, 1, domain.CIPass), target(repoB, 2, domain.CIPass)}, mergeOpts{})
	if v := dialogText(two); !strings.Contains(v, "o/a #1 t") || !strings.Contains(v, "o/b #2 t") {
		t.Errorf("multi-repo dialog lacks prefixes:\n%s", v)
	}
}

// Seam: green-CI predicate is per repo.
func TestMergeDialogGreenOnlyPerRepo(t *testing.T) {
	d := mergeDialog([]core.Target{target(repoA, 1, domain.CIFail), target(repoB, 2, domain.CIFail), target(repoC, 3, domain.CIPass)},
		mergeOpts{greenOnly: func(r domain.RepoRef) bool { return r == repoA }})
	v := dialogText(d)
	if strings.Count(v, "will be refused") != 1 || strings.Count(v, "will merge anyway") != 1 {
		t.Errorf("want one refusal (o/a) and one 'merge anyway' (o/b), none for green o/c:\n%s", v)
	}
}

// Seam: warnings and skipped lines render only when given.
func TestMergeDialogWarningAndSkipped(t *testing.T) {
	ts := []core.Target{target(repoA, 1, domain.CIPass)}
	plain := dialogText(mergeDialog(ts, mergeOpts{}))
	if strings.Contains(plain, "Skipped") || strings.Contains(plain, "not scanned") {
		t.Errorf("unexpected warning lines:\n%s", plain)
	}
	v := dialogText(mergeDialog(ts, mergeOpts{coverage: "3 repositories not scanned", skipped: []string{"o/b #2: no write access", "o/c #3: recheck failed"}}))
	for _, want := range []string{"3 repositories not scanned", "Skipped o/b #2: no write access", "Skipped o/c #3: recheck failed"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q:\n%s", want, v)
		}
	}
}

// Seam: every update a target carries is listed, so a batched PR shows all its packages.
func TestMergeDialogListsEveryUpdate(t *testing.T) {
	tg := target(repoA, 1, domain.CIPass)
	tg.CR.Renovate = []domain.RenovateUpdate{{Package: "x", From: "1", To: "2"}, {Package: "y", From: "3", To: "4"}}
	v := dialogText(mergeDialog([]core.Target{tg}, mergeOpts{}))
	if !strings.Contains(v, "x 1 → 2") || !strings.Contains(v, "y 3 → 4") || strings.Contains(v, "packages unknown") {
		t.Errorf("updates not listed:\n%s", v)
	}
}

// Seam: "packages unknown" for a Renovate target without a parsed table. In ★ it's always a Renovate PR;
// elsewhere renovate_user decides when the branch doesn't.
func TestMergeDialogPackagesUnknownHonoursRenovateUser(t *testing.T) {
	cr := domain.ChangeRequest{Number: 1, Title: "t", Author: "botty", SourceBranch: "feature/x"}
	ts := []core.Target{{Repo: repoA, CR: cr}}
	for _, tc := range []struct {
		name string
		o    mergeOpts
		want bool
	}{
		{"no renovate_user", mergeOpts{}, false},
		{"other renovate_user", mergeOpts{renovateUser: "someone"}, false},
		{"matching renovate_user", mergeOpts{renovateUser: "botty"}, true},
		{"case-insensitive", mergeOpts{renovateUser: "BOTTY"}, true},
		{"★ merge", mergeOpts{star: true}, true},
	} {
		if got := strings.Contains(dialogText(mergeDialog(ts, tc.o)), "packages unknown"); got != tc.want {
			t.Errorf("%s: 'packages unknown' = %v, want %v", tc.name, got, tc.want)
		}
	}
}
