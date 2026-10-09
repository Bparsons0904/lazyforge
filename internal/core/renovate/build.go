package renovate

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// Member is one Renovate PR with the repo it lives in.
type Member struct {
	Repo domain.RepoRef
	CR   domain.ChangeRequest // CR.Renovate is nil when the body was rejected
}

// GroupKey identifies a cross-repo update group; From is left out because it differs per repo.
type GroupKey struct{ Ecosystem, Package, To string }

// Group is one row of the updates-by-dependency box.
type Group struct {
	Key        GroupKey // zero for batched and unknown-ecosystem rows
	Batched    bool     // the single member carries more than one update
	Label      string   // "package → to", or the PR title when Batched
	UpdateType string   // highest-ranked update type among members
	Froms      []string // distinct, sorted; nil when Batched
	Members    []Member
}

// RepoSummary counts a repo's Renovate PRs by their biggest bump; Repo is zero for host totals.
type RepoSummary struct {
	Repo                            domain.RepoRef
	PRs, Major, Minor, Patch, Other int
	Impact                          float64
}

// Dashboard is a repo's parsed Dependency Dashboard issue.
type Dashboard struct {
	Repo    domain.RepoRef
	Issue   domain.Issue
	Entries []Entry
}

// RepoScan is what one repo contributes to a view.
type RepoScan struct {
	Repo       domain.Repo
	PRs        []domain.ChangeRequest // Renovate PRs only, Renovate field filled
	Dashboards []Dashboard
}

// View is the derived content of the five ★ Renovate boxes.
type View struct {
	Totals     RepoSummary
	ByRepo     []RepoSummary // [1]
	Groups     []Group       // [2]
	PRs        []Member      // [3]
	Dashboards []Dashboard   // [4]
	CI         []Member      // [5]
}

// Impact scores a PR that has been open since opened: 1 plus log2 of one plus its age in days; a zero or future opened scores 1.
func Impact(opened, now time.Time) float64 {
	days := math.Max(now.Sub(opened).Hours()/24, 0)
	if opened.IsZero() {
		days = 0
	}
	return 1 + math.Log2(1+days)
}

const (
	rankOther = iota
	rankPatch
	rankMinor
	rankMajor
)

func rank(updateType string) int {
	switch updateType {
	case "major":
		return rankMajor
	case "minor":
		return rankMinor
	case "patch":
		return rankPatch
	}
	return rankOther
}

// topType returns the highest-ranked update type; ties keep the earliest.
func topType(us []domain.RenovateUpdate) string {
	best := ""
	for i, u := range us {
		if i == 0 || rank(u.UpdateType) > rank(best) {
			best = u.UpdateType
		}
	}
	return best
}

func compareMembers(a, b Member) int {
	return cmp.Or(
		cmp.Compare(a.Repo.String(), b.Repo.String()),
		cmp.Compare(a.CR.Number, b.CR.Number),
	)
}

// Build derives the view from scans; the result doesn't depend on the order of scans.
func Build(scans []RepoScan, now time.Time) View {
	var members []Member
	var v View
	for _, s := range scans {
		for _, cr := range s.PRs {
			members = append(members, Member{Repo: s.Repo.RepoRef, CR: cr})
		}
		v.Dashboards = append(v.Dashboards, s.Dashboards...)
	}
	slices.SortStableFunc(members, compareMembers)
	slices.SortStableFunc(v.Dashboards, func(a, b Dashboard) int {
		return cmp.Or(cmp.Compare(a.Repo.String(), b.Repo.String()), cmp.Compare(a.Issue.Number, b.Issue.Number))
	})

	v.ByRepo = summarize(members, now)
	for _, r := range v.ByRepo {
		v.Totals.PRs += r.PRs
		v.Totals.Major += r.Major
		v.Totals.Minor += r.Minor
		v.Totals.Patch += r.Patch
		v.Totals.Other += r.Other
		v.Totals.Impact += r.Impact
	}
	v.Groups = group(members)

	v.PRs = slices.Clone(members)
	slices.SortStableFunc(v.PRs, func(a, b Member) int {
		return cmp.Or(a.CR.CreatedAt.Compare(b.CR.CreatedAt), compareMembers(a, b))
	})
	for _, m := range v.PRs {
		if !m.CR.CI.Green() {
			v.CI = append(v.CI, m)
		}
	}
	return v
}

func summarize(members []Member, now time.Time) []RepoSummary {
	by := map[domain.RepoRef]*RepoSummary{}
	for _, m := range members {
		s := by[m.Repo]
		if s == nil {
			s = &RepoSummary{Repo: m.Repo}
			by[m.Repo] = s
		}
		s.PRs++
		switch rank(topType(m.CR.Renovate)) {
		case rankMajor:
			s.Major++
		case rankMinor:
			s.Minor++
		case rankPatch:
			s.Patch++
		default:
			s.Other++
		}
		s.Impact += Impact(m.CR.CreatedAt, now)
	}
	out := make([]RepoSummary, 0, len(by))
	for _, s := range by {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b RepoSummary) int {
		return cmp.Or(-cmp.Compare(a.Impact, b.Impact), cmp.Compare(a.Repo.String(), b.Repo.String()))
	})
	return out
}

// group expects members already sorted, so each group's members and first-seen order are deterministic.
func group(members []Member) []Group {
	keyed := map[GroupKey]*Group{}
	var rows []Group
	for _, m := range members {
		us := m.CR.Renovate
		switch {
		case len(us) == 0:
			continue
		case len(us) > 1:
			rows = append(rows, Group{Batched: true, Label: m.CR.Title, UpdateType: topType(us), Members: []Member{m}})
		case us[0].Ecosystem == "":
			rows = append(rows, Group{Label: us[0].Package + " → " + us[0].To, UpdateType: us[0].UpdateType, Froms: []string{us[0].From}, Members: []Member{m}})
		default:
			u := us[0]
			k := GroupKey{u.Ecosystem, u.Package, u.To}
			g := keyed[k]
			if g == nil {
				g = &Group{Key: k, Label: u.Package + " → " + u.To, UpdateType: u.UpdateType}
				keyed[k] = g
			}
			if rank(u.UpdateType) > rank(g.UpdateType) {
				g.UpdateType = u.UpdateType
			}
			g.Froms = append(g.Froms, u.From)
			g.Members = append(g.Members, m)
		}
	}
	for _, k := range slices.SortedFunc(maps.Keys(keyed), func(a, b GroupKey) int {
		return cmp.Or(cmp.Compare(a.Ecosystem, b.Ecosystem), cmp.Compare(a.Package, b.Package), cmp.Compare(a.To, b.To))
	}) {
		g := *keyed[k]
		slices.Sort(g.Froms)
		g.Froms = slices.Compact(g.Froms)
		rows = append(rows, g)
	}
	slices.SortStableFunc(rows, func(a, b Group) int {
		return cmp.Or(
			-cmp.Compare(len(a.Members), len(b.Members)),
			cmp.Compare(a.Label, b.Label),
			compareMembers(a.Members[0], b.Members[0]),
		)
	})
	return rows
}
