// Package domain holds lazyforge's own types; it imports nothing from internal/.
package domain

import "time"

// RepoRef addresses a repo; Owner may contain slashes (GitLab subgroups).
type RepoRef struct{ Owner, Name string }

// String returns "owner/name".
func (r RepoRef) String() string { return r.Owner + "/" + r.Name }

// Access is the signed-in user's permission on a repo.
type Access int

// Access levels, ordered so that a higher level includes the lower ones.
const (
	AccessNone Access = iota
	AccessRead
	AccessWrite
	AccessAdmin
)

// Repo is a repository plus the user's access to it.
type Repo struct {
	RepoRef
	Description, WebURL string
	LastActivity        time.Time
	Access              Access
	MergeStyle          string // the repo's default merge style; "" when the forge doesn't report one
}

// State is the lifecycle state of an issue or change request.
type State int

// States.
const (
	StateOpen State = iota
	StateMerged
	StateClosed
)

// CIState is a forge's CI status folded into one set of values.
type CIState int

// CI states.
const (
	CINone CIState = iota
	CIPending
	CIRunning
	CIPass
	CIFail
	CICancelled
	CISkipped
)

// Green reports whether the state passes a require-green-CI gate: nothing to wait on and nothing failed.
func (s CIState) Green() bool { return s == CIPass || s == CINone || s == CISkipped }

// RenovateUpdate is one row of a Renovate PR's update table.
type RenovateUpdate struct {
	Ecosystem, Package, DepType, UpdateType, From, To, SourceURL string
}

// ChangeRequest is a PR on Gitea, Forgejo and GitHub, an MR on GitLab.
type ChangeRequest struct {
	Number              int
	Title, Body, Author string
	State               State
	SourceBranch        string
	TargetBranch        string
	HeadSHA             string
	CI                  CIState
	Labels              []string
	UpdatedAt           time.Time
	WebURL              string
	Renovate            []RenovateUpdate // nil from adapters; core fills it
}

// Issue is a forge issue.
type Issue struct {
	Number              int
	Title, Body, Author string
	State               State
	Labels              []string
	Comments            int
	UpdatedAt           time.Time
	WebURL              string
}

// Comment is one comment on an issue or change request.
type Comment struct {
	ID        int64
	Author    string
	Body      string
	CreatedAt time.Time
}

// Run is one CI run (a pipeline on GitLab).
type Run struct {
	ID                                     int64
	Number                                 int
	Workflow, Title, Branch, Commit, Event string
	Status                                 CIState
	StartedAt                              time.Time
	Duration                               time.Duration
	WebURL                                 string
}

// Job is one job of a run.
type Job struct {
	ID, RunID int64
	Name      string
	Stage     string // set on GitLab only
	Status    CIState
	Attempt   int
}

// Release is a tagged release.
type Release struct {
	Tag, Name, Notes  string
	Draft, Prerelease bool
	PublishedAt       time.Time
	WebURL            string
}
