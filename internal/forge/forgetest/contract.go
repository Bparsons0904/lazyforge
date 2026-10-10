package forgetest

import (
	"context"
	"errors"
	"image"
	_ "image/png" // registers the decoder the asset case checks the body with
	"net"
	"net/url"
	"slices"
	"strconv"
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
	Asset      string         // an on-host path from the host root that serves a PNG
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
		{"Merging an already-merged change request is ErrRefused", func(t *testing.T, f forge.Forge, fx Fixture) {
			opts := forge.MergeOpts{HeadSHA: fx.OpenCRHead}
			if err := f.Merge(ctx, fx.Repo, fx.OpenCR, opts); err != nil {
				t.Fatal(err)
			}
			if err := f.Merge(ctx, fx.Repo, fx.OpenCR, opts); !errors.Is(err, forge.ErrRefused) {
				t.Errorf("got %v, want ErrRefused", err)
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
		{"OpenAsset decodes the fixture asset", func(t *testing.T, f forge.Forge, fx Fixture) {
			ar, base := assetReader(t, f)
			rc, err := ar.OpenAsset(ctx, onHost(base, fx.Asset))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rc.Close() }()
			cfg, format, err := image.DecodeConfig(rc)
			if err != nil {
				t.Fatalf("asset does not decode as an image: %v", err)
			}
			if format != "png" || cfg.Width == 0 || cfg.Height == 0 {
				t.Errorf("decoded %q %dx%d, want a non-empty png", format, cfg.Width, cfg.Height)
			}
		}},
		{"OpenAsset off the host's origin is ErrUnsupported", func(t *testing.T, f forge.Forge, fx Fixture) {
			ar, base := assetReader(t, f)
			port := base.Port()
			if port == "" {
				port = map[string]string{"http": "80", "https": "443"}[base.Scheme]
			}
			n, err := strconv.Atoi(port)
			if err != nil {
				t.Fatal(err)
			}
			scheme := "http"
			if base.Scheme == "http" {
				scheme = "https"
			}
			otherHost := "contract-other.test"
			if base.Hostname() == otherHost {
				otherHost = "contract-another.test"
			}
			variants := map[string]func(u *url.URL){
				"hostname": func(u *url.URL) { u.Host = net.JoinHostPort(otherHost, port) },
				"port":     func(u *url.URL) { u.Host = net.JoinHostPort(base.Hostname(), strconv.Itoa(n+1)) },
				"scheme":   func(u *url.URL) { u.Scheme = scheme },
			}
			for name, mutate := range variants {
				u := *onHost(base, fx.Asset)
				mutate(&u)
				rc, err := ar.OpenAsset(ctx, &u)
				if err == nil {
					_ = rc.Close()
				}
				if !errors.Is(err, forge.ErrUnsupported) {
					t.Errorf("%s variant %s: got %v, want ErrUnsupported", name, &u, err)
				}
			}
		}},
		{"OpenAsset of an unseeded on-host path is ErrNotFound", func(t *testing.T, f forge.Forge, fx Fixture) {
			ar, base := assetReader(t, f)
			rc, err := ar.OpenAsset(ctx, onHost(base, fx.Asset+"-missing"))
			if err == nil {
				_ = rc.Close()
			}
			if !errors.Is(err, forge.ErrNotFound) {
				t.Errorf("got %v, want ErrNotFound", err)
			}
		}},
		{"UpdateStyles starts with UpdateMerge", func(t *testing.T, f forge.Forge, _ Fixture) {
			styles := branchUpdater(t, f).UpdateStyles()
			if len(styles) == 0 || styles[0] != forge.UpdateMerge {
				t.Errorf("styles %v, want non-empty with UpdateMerge first", styles)
			}
		}},
		{"UpdateBranch with UpdateMerge on the open change request succeeds", func(t *testing.T, f forge.Forge, fx Fixture) {
			if err := branchUpdater(t, f).UpdateBranch(ctx, fx.Repo, fx.OpenCR, forge.UpdateMerge); err != nil {
				t.Fatal(err)
			}
		}},
		{"UpdateBranch of a missing number is ErrNotFound", func(t *testing.T, f forge.Forge, fx Fixture) {
			err := branchUpdater(t, f).UpdateBranch(ctx, fx.Repo, fx.Missing, forge.UpdateMerge)
			if !errors.Is(err, forge.ErrNotFound) {
				t.Errorf("got %v, want ErrNotFound", err)
			}
		}},
		{"UpdateBranch with a style UpdateStyles omits is ErrUnsupported", func(t *testing.T, f forge.Forge, fx Fixture) {
			bu := branchUpdater(t, f)
			offered := bu.UpdateStyles()
			for _, style := range []forge.UpdateStyle{forge.UpdateMerge, forge.UpdateRebase} {
				if slices.Contains(offered, style) {
					continue
				}
				if err := bu.UpdateBranch(ctx, fx.Repo, fx.OpenCR, style); !errors.Is(err, forge.ErrUnsupported) {
					t.Errorf("style %s: got %v, want ErrUnsupported", style, err)
				}
			}
		}},
		{"DispatchWorkflow on Repo succeeds", func(t *testing.T, f forge.Forge, fx Fixture) {
			wd := workflowDispatcher(t, f)
			if err := wd.DispatchWorkflow(ctx, fx.Repo, "renovate.yml", "main", map[string]string{"repo": "a/b"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"DispatchWorkflow in a missing repo is ErrNotFound", func(t *testing.T, f forge.Forge, fx Fixture) {
			wd := workflowDispatcher(t, f)
			missing := domain.RepoRef{Owner: fx.Repo.Owner, Name: "no-such-repo"}
			if err := wd.DispatchWorkflow(ctx, missing, "renovate.yml", "main", nil); !errors.Is(err, forge.ErrNotFound) {
				t.Errorf("got %v, want ErrNotFound", err)
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

// onHost returns base with its path replaced by p; url.URL.JoinPath drops the leading slash when base has no path.
func onHost(base *url.URL, p string) *url.URL {
	u := *base
	u.Path = p
	return &u
}

// branchUpdater returns f's BranchUpdater, or skips the case for a forge without the capability.
func branchUpdater(t *testing.T, f forge.Forge) forge.BranchUpdater {
	t.Helper()
	bu, ok := f.(forge.BranchUpdater)
	if !ok {
		t.Skip("forge is not a forge.BranchUpdater")
	}
	return bu
}

// workflowDispatcher returns f's WorkflowDispatcher, or skips the case for a forge without the capability.
func workflowDispatcher(t *testing.T, f forge.Forge) forge.WorkflowDispatcher {
	t.Helper()
	wd, ok := f.(forge.WorkflowDispatcher)
	if !ok {
		t.Skip("forge is not a forge.WorkflowDispatcher")
	}
	return wd
}

// assetReader returns f's AssetReader and its host URL, or skips the case for a forge without the capability.
func assetReader(t *testing.T, f forge.Forge) (forge.AssetReader, *url.URL) {
	t.Helper()
	ar, ok := f.(forge.AssetReader)
	if !ok {
		t.Skip("forge is not a forge.AssetReader")
	}
	base, err := url.Parse(f.Info().URL)
	if err != nil || base.Host == "" {
		t.Fatalf("host URL %q is not absolute: %v", f.Info().URL, err)
	}
	return ar, base
}
