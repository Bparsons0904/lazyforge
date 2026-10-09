// Package forge defines the Forge interface and its optional capabilities; it holds no implementations.
package forge

import (
	"context"
	"errors"
	"io"
	"net/url"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// Kind identifies a forge family.
type Kind string

// Forge kinds.
const (
	KindForgejo Kind = "forgejo"
	KindGitea   Kind = "gitea"
	KindGitHub  Kind = "github"
	KindGitLab  Kind = "gitlab"
)

// HostInfo describes the connected host.
type HostInfo struct {
	Kind              Kind
	URL, Version      string
	User              string
	ChangeRequestTerm string // "PR" or "MR"
}

// ItemKind says whether an ItemRef targets an issue or a change request.
type ItemKind int

// Item kinds.
const (
	ItemIssue ItemKind = iota
	ItemChangeRequest
)

// ItemRef targets an issue or change request; GitLab routes the two differently.
type ItemRef struct {
	Repo   domain.RepoRef
	Kind   ItemKind
	Number int
}

// Filter selects items by state.
type Filter struct{ State domain.State }

// RunFilter narrows run listings; empty fields match everything.
type RunFilter struct{ Branch, HeadSHA string }

// MergeOpts configures Merge.
type MergeOpts struct {
	HeadSHA string // required; a mismatch returns ErrHeadChanged
	Method  string // "" means the repo default
}

// Sentinel errors every adapter translates into.
var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrRateLimited  = errors.New("rate limited")
	ErrUnsupported  = errors.New("unsupported")
	ErrHeadChanged  = errors.New("head changed since confirmation")
	ErrRefused      = errors.New("refused by the forge") // the forge declined: conflicts, branch protection, required checks
)

// Forge is the core interface every adapter implements; list methods return every page.
type Forge interface {
	Info() HostInfo
	ListRepos(ctx context.Context) ([]domain.Repo, error)

	ListChangeRequests(ctx context.Context, r domain.RepoRef, f Filter) ([]domain.ChangeRequest, error)
	GetChangeRequest(ctx context.Context, r domain.RepoRef, n int) (domain.ChangeRequest, error)
	Merge(ctx context.Context, r domain.RepoRef, n int, opts MergeOpts) error

	ListIssues(ctx context.Context, r domain.RepoRef, f Filter) ([]domain.Issue, error)
	EditIssueBody(ctx context.Context, r domain.RepoRef, n int, body string) error
	ListComments(ctx context.Context, item ItemRef) ([]domain.Comment, error)
	Comment(ctx context.Context, item ItemRef, body string) error
	Close(ctx context.Context, item ItemRef) error

	ListReleases(ctx context.Context, r domain.RepoRef) ([]domain.Release, error)

	// Gate returns nil when the server allows a, else the user-facing reason; it does no I/O.
	Gate(a Action) error
}

// Approver is implemented by adapters that can approve a change request.
type Approver interface {
	Approve(ctx context.Context, r domain.RepoRef, n int) error
}

// RunLister is implemented by adapters that expose CI runs and their jobs.
type RunLister interface {
	ListRuns(ctx context.Context, r domain.RepoRef, f RunFilter) ([]domain.Run, error)
	ListJobs(ctx context.Context, r domain.RepoRef, runID int64) ([]domain.Job, error)
}

// LogReader is implemented by adapters that can stream a job's log.
type LogReader interface {
	JobLog(ctx context.Context, r domain.RepoRef, jobID int64) (io.ReadCloser, error)
}

// Labeler is implemented by adapters that can read and replace item labels.
type Labeler interface {
	ListLabels(context.Context, domain.RepoRef) ([]domain.Label, error)
	ItemLabels(context.Context, ItemRef) ([]domain.Label, error)
	SetLabels(context.Context, ItemRef, []int64) ([]domain.Label, error)
}

// AssetReader is implemented by adapters that can open a file the host serves for a body, such as an attachment.
type AssetReader interface {
	// OpenAsset returns the body of u, which must be on the session's host; the caller closes it.
	OpenAsset(ctx context.Context, u *url.URL) (io.ReadCloser, error)
}

// ReadmeReader is implemented by adapters that can read a repo's README.
type ReadmeReader interface {
	// GetReadme returns the README on the default branch, or an error matching ErrNotFound when the repo has none.
	GetReadme(ctx context.Context, r domain.RepoRef) (domain.Readme, error)
}
