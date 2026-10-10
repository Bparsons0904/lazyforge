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

type withReadme struct{ base }

func (withReadme) GetReadme(context.Context, domain.RepoRef) (domain.Readme, error) {
	return domain.Readme{}, nil
}

type withBranches struct{ base }

func (withBranches) ListBranches(context.Context, domain.RepoRef) ([]domain.Branch, error) {
	return nil, nil
}

func (withBranches) ListCommits(context.Context, domain.RepoRef, string) ([]domain.Commit, error) {
	return nil, nil
}

type withTree struct{ base }

func (withTree) ListTree(context.Context, domain.RepoRef, string, string) ([]domain.TreeEntry, error) {
	return nil, nil
}

func (withTree) ReadFile(context.Context, domain.RepoRef, string, string) ([]byte, error) {
	return nil, nil
}

type withUpdater struct{ base }

func (withUpdater) UpdateStyles() []forge.UpdateStyle { return nil }

func (withUpdater) UpdateBranch(context.Context, domain.RepoRef, int, forge.UpdateStyle) error {
	return nil
}

type withDispatcher struct{ base }

func (withDispatcher) DispatchWorkflow(context.Context, domain.RepoRef, string, string, map[string]string) error {
	return nil
}

type full struct {
	withApprover
	withRuns
	withLogs
	withReadme
	withBranches
	withTree
	withUpdater
	withDispatcher
	base
}

var allActions = []forge.Action{
	forge.ActMerge, forge.ActApprove, forge.ActClose, forge.ActComment,
	forge.ActEditIssue, forge.ActRuns, forge.ActLogs, forge.ActBranches, forge.ActFiles,
	forge.ActUpdateBranch, forge.ActDispatchWorkflow,
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
		{"readme without ReadmeReader", withRuns{base{kind: forge.KindGitea}}, forge.ActReadme},
		{"branches without BranchReader", withRuns{base{kind: forge.KindGitea}}, forge.ActBranches},
		{"files without TreeReader", withRuns{base{kind: forge.KindGitea}}, forge.ActFiles},
		{"update branch without BranchUpdater", withRuns{base{kind: forge.KindGitea}}, forge.ActUpdateBranch},
		{"dispatch without WorkflowDispatcher", withRuns{base{kind: forge.KindGitea}}, forge.ActDispatchWorkflow},
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

func TestCanDispatchNeedsWriteAccess(t *testing.T) {
	got := forge.Can(newFull(nil), forge.ActDispatchWorkflow, repoWith(domain.AccessRead))
	if want := "you don't have write access to owner/name"; got.OK || got.Reason != want {
		t.Errorf("read-only dispatch: got %+v, want {false, %q}", got, want)
	}
}

func TestCanDispatchWithoutCapabilitySaysKind(t *testing.T) {
	got := forge.Can(withRuns{base{kind: forge.KindGitea}}, forge.ActDispatchWorkflow, repoWith(domain.AccessWrite))
	if want := "gitea doesn't support this"; got.OK || got.Reason != want {
		t.Errorf("got %+v, want {false, %q}", got, want)
	}
}

func TestCanDispatchGateRefusalIsReason(t *testing.T) {
	f := newFull(func(a forge.Action) error {
		if a == forge.ActDispatchWorkflow {
			return errors.New("server disables Actions")
		}
		return nil
	})
	got := forge.Can(f, forge.ActDispatchWorkflow, repoWith(domain.AccessAdmin))
	if got.OK || got.Reason != "server disables Actions" {
		t.Errorf("gated dispatch: got %+v, want {false, \"server disables Actions\"}", got)
	}
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
	gated := newFull(func(a forge.Action) error {
		if a == forge.ActUpdateBranch {
			return errors.New("branch protected")
		}
		return nil
	})
	if got := forge.Can(gated, forge.ActUpdateBranch, repoWith(domain.AccessAdmin)); got.OK || got.Reason != "branch protected" {
		t.Errorf("gated update branch: got %+v, want {false, \"branch protected\"}", got)
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
		{forge.ActReadme, domain.AccessNone, false},
		{forge.ActReadme, domain.AccessRead, true},
		{forge.ActBranches, domain.AccessNone, false},
		{forge.ActBranches, domain.AccessRead, true},
		{forge.ActFiles, domain.AccessNone, false},
		{forge.ActFiles, domain.AccessRead, true},
		{forge.ActUpdateBranch, domain.AccessRead, false},
		{forge.ActUpdateBranch, domain.AccessWrite, true},
		{forge.ActUpdateBranch, domain.AccessAdmin, true},
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

type withLabels struct{ base }

func (withLabels) ListLabels(context.Context, domain.RepoRef) ([]domain.Label, error) {
	return nil, nil
}

func (withLabels) ItemLabels(context.Context, forge.ItemRef) ([]domain.Label, error) { return nil, nil }

func (withLabels) SetLabels(context.Context, forge.ItemRef, []int64) ([]domain.Label, error) {
	return nil, nil
}

func TestLabelCapabilityAndPermission(t *testing.T) {
	if forge.Can(base{}, forge.ActLabels, repoWith(domain.AccessWrite)).OK {
		t.Fatal("unsupported labels enabled")
	}
	f := withLabels{}
	if forge.Can(f, forge.ActLabels, repoWith(domain.AccessRead)).OK {
		t.Fatal("label writes enabled with read access")
	}
	if !forge.Can(f, forge.ActLabels, repoWith(domain.AccessWrite)).OK {
		t.Fatal("label writes disabled with write access")
	}
}

func TestUpdateBranchReasons(t *testing.T) {
	t.Run("read access names the repo", func(t *testing.T) {
		got := forge.Can(newFull(nil), forge.ActUpdateBranch, repoWith(domain.AccessRead))
		want := "you don't have write access to owner/name"
		if got.OK || got.Reason != want {
			t.Errorf("got %+v, want {false, %q}", got, want)
		}
	})

	t.Run("forge without BranchUpdater names its kind", func(t *testing.T) {
		got := forge.Can(withRuns{base{kind: forge.KindGitea}}, forge.ActUpdateBranch, repoWith(domain.AccessAdmin))
		want := "gitea doesn't support this"
		if got.OK || got.Reason != want {
			t.Errorf("got %+v, want {false, %q}", got, want)
		}
	})
}

func TestActionValuesUnchanged(t *testing.T) {
	// Actions are appended, never inserted, so the numbers of the existing ones stay put.
	existing := []forge.Action{
		forge.ActMerge, forge.ActApprove, forge.ActClose, forge.ActComment,
		forge.ActEditIssue, forge.ActRuns, forge.ActLogs, forge.ActLabels,
		forge.ActAssets, forge.ActReadme, forge.ActBranches, forge.ActFiles,
	}
	for i, a := range existing {
		if int(a) != i {
			t.Errorf("action at position %d has value %d, want %d", i, int(a), i)
		}
	}
	if forge.ActUpdateBranch != forge.ActFiles+1 {
		t.Errorf("ActUpdateBranch = %d, want ActFiles+1 = %d", forge.ActUpdateBranch, forge.ActFiles+1)
	}
}
