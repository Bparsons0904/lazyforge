package forgetest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// Fixture tells RunContract which seeded items the forge under test holds.
type Fixture struct {
	Repo       domain.RepoRef // has at least one open change request and one open issue
	OpenCR     int            // an open change request in Repo
	OpenCRHead string         // OpenCR's head SHA
	OpenIssue  int            // an open issue in Repo
	Missing    int            // a number that is neither an issue nor a change request in Repo
}

// RunContract runs the behavior every adapter must share; newForge must return a fresh forge per call.
func RunContract(t *testing.T, newForge func(t *testing.T) (forge.Forge, Fixture)) {
	t.Helper()
	ctx := context.Background()

	tests := []struct {
		name string
		run  func(t *testing.T, f forge.Forge, fx Fixture)
	}{
		{"ListRepos sorted by activity and contains fixture repo", func(t *testing.T, f forge.Forge, fx Fixture) {
			repos, err := f.ListRepos(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(repos, func(r domain.Repo) bool { return r.RepoRef == fx.Repo }) {
				t.Errorf("repos %v do not contain %s", repos, fx.Repo)
			}
			sorted := slices.IsSortedFunc(repos, func(a, b domain.Repo) int { return b.LastActivity.Compare(a.LastActivity) })
			if !sorted {
				t.Error("repos are not sorted by LastActivity descending")
			}
		}},
		{"GetChangeRequest returns the open change request", func(t *testing.T, f forge.Forge, fx Fixture) {
			cr, err := f.GetChangeRequest(ctx, fx.Repo, fx.OpenCR)
			if err != nil {
				t.Fatal(err)
			}
			if cr.HeadSHA != fx.OpenCRHead || cr.State != domain.StateOpen {
				t.Errorf("got head %q state %v, want head %q open", cr.HeadSHA, cr.State, fx.OpenCRHead)
			}
		}},
		{"GetChangeRequest of a missing number is ErrNotFound", func(t *testing.T, f forge.Forge, fx Fixture) {
			_, err := f.GetChangeRequest(ctx, fx.Repo, fx.Missing)
			if !errors.Is(err, forge.ErrNotFound) {
				t.Errorf("got %v, want ErrNotFound", err)
			}
		}},
		{"Merge with a stale head fails and leaves the change request open", func(t *testing.T, f forge.Forge, fx Fixture) {
			err := f.Merge(ctx, fx.Repo, fx.OpenCR, forge.MergeOpts{HeadSHA: fx.OpenCRHead + "-stale"})
			if !errors.Is(err, forge.ErrHeadChanged) {
				t.Fatalf("got %v, want ErrHeadChanged", err)
			}
			cr, err := f.GetChangeRequest(ctx, fx.Repo, fx.OpenCR)
			if err != nil {
				t.Fatal(err)
			}
			if cr.State != domain.StateOpen {
				t.Errorf("state %v, want open", cr.State)
			}
		}},
		{"Merge with the right head merges", func(t *testing.T, f forge.Forge, fx Fixture) {
			if err := f.Merge(ctx, fx.Repo, fx.OpenCR, forge.MergeOpts{HeadSHA: fx.OpenCRHead}); err != nil {
				t.Fatal(err)
			}
			cr, err := f.GetChangeRequest(ctx, fx.Repo, fx.OpenCR)
			if err != nil {
				t.Fatal(err)
			}
			if cr.State != domain.StateMerged {
				t.Errorf("state %v, want merged", cr.State)
			}
		}},
		{"Comment is visible through ListComments", func(t *testing.T, f forge.Forge, fx Fixture) {
			item := forge.ItemRef{Repo: fx.Repo, Kind: forge.ItemIssue, Number: fx.OpenIssue}
			if err := f.Comment(ctx, item, "contract comment"); err != nil {
				t.Fatal(err)
			}
			comments, err := f.ListComments(ctx, item)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(comments, func(c domain.Comment) bool { return c.Body == "contract comment" }) {
				t.Errorf("comments %v do not contain the posted body", comments)
			}
		}},
		{"Close removes an issue from the open list", func(t *testing.T, f forge.Forge, fx Fixture) {
			item := forge.ItemRef{Repo: fx.Repo, Kind: forge.ItemIssue, Number: fx.OpenIssue}
			if err := f.Close(ctx, item); err != nil {
				t.Fatal(err)
			}
			issues, err := f.ListIssues(ctx, fx.Repo, forge.Filter{State: domain.StateOpen})
			if err != nil {
				t.Fatal(err)
			}
			if slices.ContainsFunc(issues, func(i domain.Issue) bool { return i.Number == fx.OpenIssue }) {
				t.Error("closed issue still listed as open")
			}
		}},
		{"ListChangeRequests open contains only open items including OpenCR", func(t *testing.T, f forge.Forge, fx Fixture) {
			crs, err := f.ListChangeRequests(ctx, fx.Repo, forge.Filter{State: domain.StateOpen})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(crs, func(c domain.ChangeRequest) bool { return c.Number == fx.OpenCR }) {
				t.Errorf("open list does not contain #%d", fx.OpenCR)
			}
			for _, c := range crs {
				if c.State != domain.StateOpen {
					t.Errorf("#%d has state %v in the open list", c.Number, c.State)
				}
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, fx := newForge(t)
			tt.run(t, f, fx)
		})
	}
}
