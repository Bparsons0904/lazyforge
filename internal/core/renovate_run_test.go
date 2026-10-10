package core_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var (
	workflowRepo = domain.RepoRef{Owner: "deadstyle", Name: "forgejo"}
	filesRepo    = domain.RepoRef{Owner: "deadstyle", Name: "files"}
	renovateCfg  = core.RenovateWorkflow{Repo: workflowRepo, File: "renovate.yml"}
)

// workflowFake returns a Fake whose workflow repo has access and branch as its default branch.
func workflowFake(access domain.Access, branch string) *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob"})
	f.AddRepo(domain.Repo{RepoRef: workflowRepo, Access: access, DefaultBranch: branch})
	return f
}

// loadedRenovate returns a Service configured with renovateCfg whose repo list is cached.
func loadedRenovate(t *testing.T, f forge.Forge) *core.Service {
	t.Helper()
	s := core.New(f, core.Options{RenovateWorkflow: func() core.RenovateWorkflow { return renovateCfg }})
	if _, err := s.Repos(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCanRunRenovateWithoutConfigIsOff(t *testing.T) {
	f := workflowFake(domain.AccessWrite, "main")
	s := core.New(f, core.Options{})
	if _, err := s.Repos(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := s.CanRunRenovate(); got.OK {
		t.Errorf("CanRunRenovate() = %+v, want not OK without a configured workflow", got)
	}
	err := s.RunRenovate(context.Background(), domain.RepoRef{})
	if !errors.Is(err, forge.ErrUnsupported) {
		t.Fatalf("RunRenovate error = %v, want ErrUnsupported", err)
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("%d mutations recorded, want none", n)
	}
}

func TestCanRunRenovateNeedsRepoListLoaded(t *testing.T) {
	f := workflowFake(domain.AccessWrite, "main")
	s := core.New(f, core.Options{RenovateWorkflow: func() core.RenovateWorkflow { return renovateCfg }})
	if got := s.CanRunRenovate(); got.OK {
		t.Errorf("CanRunRenovate() = %+v, want not OK before the repo list loads", got)
	}
	err := s.RunRenovate(context.Background(), domain.RepoRef{})
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("RunRenovate error = %v, want ErrNotFound", err)
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("%d mutations recorded, want none", n)
	}
}

func TestCanRunRenovateNeedsWriteAccess(t *testing.T) {
	if got := loadedRenovate(t, workflowFake(domain.AccessWrite, "main")).CanRunRenovate(); !got.OK {
		t.Errorf("CanRunRenovate() at write access = %+v, want OK", got)
	}
	got := loadedRenovate(t, workflowFake(domain.AccessRead, "main")).CanRunRenovate()
	if got.OK || got.Reason != "you don't have write access to deadstyle/forgejo" {
		t.Errorf("CanRunRenovate() at read access = %+v, want not OK with the write-access reason", got)
	}
}

func TestCanRunRenovateMissingRepoIsOff(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob"})
	f.AddRepo(domain.Repo{RepoRef: filesRepo, Access: domain.AccessWrite, DefaultBranch: "main"})
	s := loadedRenovate(t, f)
	if got := s.CanRunRenovate(); got.OK {
		t.Errorf("CanRunRenovate() = %+v, want not OK when the workflow repo is missing", got)
	}
	err := s.RunRenovate(context.Background(), filesRepo)
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("RunRenovate error = %v, want ErrNotFound", err)
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("%d mutations recorded, want none", n)
	}
}

func TestCanRunRenovateNeedsDefaultBranch(t *testing.T) {
	f := workflowFake(domain.AccessWrite, "")
	s := loadedRenovate(t, f)
	got := s.CanRunRenovate()
	if got.OK || !strings.Contains(got.Reason, "deadstyle/forgejo") || !strings.Contains(got.Reason, "default branch") {
		t.Errorf("CanRunRenovate() = %+v, want not OK with a reason naming the repo and default branch", got)
	}
	err := s.RunRenovate(context.Background(), filesRepo)
	if err == nil || !strings.Contains(err.Error(), "deadstyle/forgejo") || !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("RunRenovate error = %v, want one naming the repo and default branch", err)
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("%d mutations recorded, want none", n)
	}
}

func TestRunRenovateDispatchesOneRepo(t *testing.T) {
	f := workflowFake(domain.AccessWrite, "main")
	if err := loadedRenovate(t, f).RunRenovate(context.Background(), filesRepo); err != nil {
		t.Fatalf("RunRenovate: %v", err)
	}
	ms := f.Mutations()
	if len(ms) != 1 {
		t.Fatalf("%d mutations recorded, want one", len(ms))
	}
	m := ms[0]
	if m.Op != "dispatch-workflow" || m.Item.Repo != workflowRepo || m.Workflow != "renovate.yml" || m.Ref != "main" {
		t.Errorf("mutation = %+v, want dispatch-workflow on deadstyle/forgejo renovate.yml at main", m)
	}
	if len(m.Inputs) != 1 || m.Inputs["repo"] != "deadstyle/files" {
		t.Errorf("Inputs = %v, want map[repo:deadstyle/files]", m.Inputs)
	}
}

func TestRunRenovateDispatchesOnWorkflowRepoDefaultBranch(t *testing.T) {
	f := workflowFake(domain.AccessWrite, "develop")
	if err := loadedRenovate(t, f).RunRenovate(context.Background(), filesRepo); err != nil {
		t.Fatalf("RunRenovate: %v", err)
	}
	if ms := f.Mutations(); len(ms) != 1 || ms[0].Ref != "develop" {
		t.Errorf("mutations = %+v, want one dispatch on develop", ms)
	}
}

func TestRunRenovateAllReposSendsNoInputs(t *testing.T) {
	f := workflowFake(domain.AccessWrite, "main")
	if err := loadedRenovate(t, f).RunRenovate(context.Background(), domain.RepoRef{}); err != nil {
		t.Fatalf("RunRenovate: %v", err)
	}
	ms := f.Mutations()
	if len(ms) != 1 || len(ms[0].Inputs) != 0 {
		t.Errorf("mutations = %+v, want one dispatch with no inputs", ms)
	}
}

func TestRunRenovateMatchesWorkflowRepoCaseInsensitively(t *testing.T) {
	f := workflowFake(domain.AccessWrite, "main")
	cfg := core.RenovateWorkflow{Repo: domain.RepoRef{Owner: "Deadstyle", Name: "ForgeJO"}, File: "renovate.yml"}
	s := core.New(f, core.Options{RenovateWorkflow: func() core.RenovateWorkflow { return cfg }})
	if _, err := s.Repos(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.RunRenovate(context.Background(), domain.RepoRef{}); err != nil {
		t.Fatalf("RunRenovate: %v", err)
	}
	if ms := f.Mutations(); len(ms) != 1 || ms[0].Item.Repo != workflowRepo {
		t.Errorf("mutations = %+v, want one dispatch on the list's deadstyle/forgejo", ms)
	}
}

func TestRunRenovateForgeFailureIsWrapped(t *testing.T) {
	f := workflowFake(domain.AccessWrite, "main")
	s := loadedRenovate(t, f)
	f.FailNext(errors.New("boom"))
	err := s.RunRenovate(context.Background(), filesRepo)
	if err == nil || !strings.HasPrefix(err.Error(), "run Renovate for deadstyle/files: ") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("RunRenovate error = %v, want the run named and the forge's reason", err)
	}
}

func TestRunRenovateAtReadAccessMakesNoCall(t *testing.T) {
	f := workflowFake(domain.AccessRead, "main")
	err := loadedRenovate(t, f).RunRenovate(context.Background(), filesRepo)
	if err == nil || !strings.Contains(err.Error(), "you don't have write access to deadstyle/forgejo") {
		t.Fatalf("RunRenovate error = %v, want the write-access reason", err)
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("%d mutations recorded, want none", n)
	}
}

func TestRunRenovateWithoutCapabilityMakesNoCall(t *testing.T) {
	f := workflowFake(domain.AccessWrite, "main")
	s := loadedRenovate(t, plainForge{f})
	if got := s.CanRunRenovate(); got.OK {
		t.Errorf("CanRunRenovate() = %+v, want not OK on a forge without WorkflowDispatcher", got)
	}
	err := s.RunRenovate(context.Background(), domain.RepoRef{})
	if !errors.Is(err, forge.ErrUnsupported) {
		t.Fatalf("RunRenovate error = %v, want ErrUnsupported", err)
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("%d mutations recorded, want none", n)
	}
}
