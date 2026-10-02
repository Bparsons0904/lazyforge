package forge_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// base implements only the core Forge interface; its Gate is configurable.
type base struct {
	kind forge.Kind
	gate func(forge.Action) error
}

func (b base) Info() forge.HostInfo { return forge.HostInfo{Kind: b.kind} }
func (b base) Gate(a forge.Action) error {
	if b.gate == nil {
		return nil
	}
	return b.gate(a)
}

func (base) ListRepos(context.Context) ([]domain.Repo, error) { return nil, nil }
func (base) ListChangeRequests(context.Context, domain.RepoRef, forge.Filter) ([]domain.ChangeRequest, error) {
	return nil, nil
}

func (base) GetChangeRequest(context.Context, domain.RepoRef, int) (domain.ChangeRequest, error) {
	return domain.ChangeRequest{}, nil
}

func (base) Merge(context.Context, domain.RepoRef, int, forge.MergeOpts) error { return nil }
func (base) ListIssues(context.Context, domain.RepoRef, forge.Filter) ([]domain.Issue, error) {
	return nil, nil
}
func (base) EditIssueBody(context.Context, domain.RepoRef, int, string) error { return nil }
func (base) ListComments(context.Context, forge.ItemRef) ([]domain.Comment, error) {
	return nil, nil
}
func (base) Comment(context.Context, forge.ItemRef, string) error { return nil }
func (base) Close(context.Context, forge.ItemRef) error           { return nil }
func (base) ListReleases(context.Context, domain.RepoRef) ([]domain.Release, error) {
	return nil, nil
}

type withApprover struct{ base }

func (withApprover) Approve(context.Context, domain.RepoRef, int) error { return nil }

type withRuns struct{ base }

func (withRuns) ListRuns(context.Context, domain.RepoRef, forge.RunFilter) ([]domain.Run, error) {
	return nil, nil
}

func (withRuns) ListJobs(context.Context, domain.RepoRef, int64) ([]domain.Job, error) {
	return nil, nil
}

type withLogs struct{ base }

func (withLogs) JobLog(context.Context, domain.RepoRef, int64) (io.ReadCloser, error) {
	return nil, nil
}

type full struct {
	withApprover
	withRuns
	withLogs
	base
}

var allActions = []forge.Action{
	forge.ActMerge, forge.ActApprove, forge.ActClose, forge.ActComment,
	forge.ActEditIssue, forge.ActRuns, forge.ActLogs,
}

func repoWith(a domain.Access) domain.Repo {
	return domain.Repo{RepoRef: domain.RepoRef{Owner: "owner", Name: "name"}, Access: a}
}

func newFull(gate func(forge.Action) error) forge.Forge {
	return full{base: base{kind: forge.KindForgejo, gate: gate}}
}

func TestCanAllowed(t *testing.T) {
	f := newFull(nil)
	for _, a := range allActions {
		got := forge.Can(f, a, repoWith(domain.AccessWrite))
		if !got.OK || got.Reason != "" {
			t.Errorf("action %d with write access: got %+v, want OK with empty reason", a, got)
		}
	}
}

func TestCanCapability(t *testing.T) {
	tests := []struct {
		name string
		f    forge.Forge
		act  forge.Action
	}{
		{"approve without Approver", withRuns{base{kind: forge.KindGitea}}, forge.ActApprove},
		{"runs without RunLister", withApprover{base{kind: forge.KindGitea}}, forge.ActRuns},
		{"logs without LogReader", withRuns{base{kind: forge.KindGitea}}, forge.ActLogs},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := forge.Can(tt.f, tt.act, repoWith(domain.AccessAdmin))
			if got.OK {
				t.Fatalf("got OK, want unavailable")
			}
			if got.Reason == "" || !strings.Contains(strings.ToLower(got.Reason), "gitea") {
				t.Errorf("reason %q should be non-empty and mention the kind", got.Reason)
			}
		})
	}

	t.Run("core actions need no capability", func(t *testing.T) {
		f := base{kind: forge.KindGitea}
		for _, a := range []forge.Action{forge.ActMerge, forge.ActClose, forge.ActComment, forge.ActEditIssue} {
			if got := forge.Can(f, a, repoWith(domain.AccessWrite)); !got.OK {
				t.Errorf("action %d: got %+v, want OK", a, got)
			}
		}
	})
}

func TestCanGate(t *testing.T) {
	f := newFull(func(a forge.Action) error {
		if a == forge.ActMerge {
			return errors.New("server is read-only")
		}
		return nil
	})
	got := forge.Can(f, forge.ActMerge, repoWith(domain.AccessAdmin))
	if got.OK || got.Reason != "server is read-only" {
		t.Errorf("gated merge: got %+v, want {false, \"server is read-only\"}", got)
	}
	if got := forge.Can(f, forge.ActClose, repoWith(domain.AccessAdmin)); !got.OK {
		t.Errorf("ungated close: got %+v, want OK", got)
	}
}

func TestCanPermission(t *testing.T) {
	tests := []struct {
		act    forge.Action
		access domain.Access
		ok     bool
	}{
		{forge.ActMerge, domain.AccessRead, false},
		{forge.ActMerge, domain.AccessWrite, true},
		{forge.ActApprove, domain.AccessRead, false},
		{forge.ActApprove, domain.AccessAdmin, true},
		{forge.ActClose, domain.AccessNone, false},
		{forge.ActClose, domain.AccessRead, false},
		{forge.ActClose, domain.AccessWrite, true},
		{forge.ActEditIssue, domain.AccessRead, false},
		{forge.ActEditIssue, domain.AccessWrite, true},
		{forge.ActComment, domain.AccessNone, false},
		{forge.ActComment, domain.AccessRead, true},
		{forge.ActRuns, domain.AccessNone, false},
		{forge.ActRuns, domain.AccessRead, true},
		{forge.ActLogs, domain.AccessNone, false},
		{forge.ActLogs, domain.AccessRead, true},
		{forge.ActLogs, domain.AccessAdmin, true},
	}
	f := newFull(nil)
	for _, tt := range tests {
		got := forge.Can(f, tt.act, repoWith(tt.access))
		if got.OK != tt.ok {
			t.Errorf("action %d access %d: OK = %v, want %v", tt.act, tt.access, got.OK, tt.ok)
		}
		if tt.ok && got.Reason != "" {
			t.Errorf("action %d access %d: allowed but reason %q", tt.act, tt.access, got.Reason)
		}
		if !tt.ok && !strings.Contains(strings.ToLower(got.Reason), "access") {
			t.Errorf("action %d access %d: reason %q should mention access", tt.act, tt.access, got.Reason)
		}
	}
}

func TestCanRuleOrder(t *testing.T) {
	gateErr := errors.New("gated")
	gate := func(forge.Action) error { return gateErr }

	t.Run("capability beats gate", func(t *testing.T) {
		f := withRuns{base{kind: forge.KindGitHub, gate: gate}}
		got := forge.Can(f, forge.ActApprove, repoWith(domain.AccessAdmin))
		if got.OK || got.Reason == "gated" || !strings.Contains(strings.ToLower(got.Reason), "github") {
			t.Errorf("got %+v, want capability reason mentioning the kind", got)
		}
	})

	t.Run("gate beats permission", func(t *testing.T) {
		got := forge.Can(newFull(gate), forge.ActMerge, repoWith(domain.AccessNone))
		if got.OK || got.Reason != "gated" {
			t.Errorf("got %+v, want {false, \"gated\"}", got)
		}
	})

	t.Run("capability beats permission", func(t *testing.T) {
		f := base{kind: forge.KindGitLab}
		got := forge.Can(f, forge.ActApprove, repoWith(domain.AccessNone))
		if got.OK || !strings.Contains(strings.ToLower(got.Reason), "gitlab") {
			t.Errorf("got %+v, want capability reason mentioning the kind", got)
		}
	})
}
