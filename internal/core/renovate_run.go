package core

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// RenovateWorkflow names the workflow that runs Renovate for the host: File in Repo's workflows directory.
type RenovateWorkflow struct {
	Repo domain.RepoRef
	File string
}

// CanRunRenovate reports whether RunRenovate can work: a workflow is configured, the forge can dispatch it,
// and the cached repo list holds its repo with write access. It does no I/O.
func (s *Service) CanRunRenovate() forge.Availability {
	w := s.workflow()
	if w == (RenovateWorkflow{}) {
		return forge.Availability{Reason: "no renovate_workflow is set for this host"}
	}
	if _, ok := s.f.(forge.WorkflowDispatcher); !ok {
		return forge.Availability{Reason: fmt.Sprintf("%s doesn't support this", s.f.Info().Kind)}
	}
	repo, ok := s.cachedWorkflowRepo(w.Repo)
	if !ok {
		return forge.Availability{Reason: fmt.Sprintf("%s isn't in the repo list", w.Repo)}
	}
	return s.workflowAvailability(repo)
}

// RunRenovate dispatches the Renovate workflow on its repo's default branch; a zero only runs it for every repo;
// otherwise the dispatch sends only as the workflow's repo input. It makes no forge call when the run can't work.
func (s *Service) RunRenovate(ctx context.Context, only domain.RepoRef) error {
	what := "run Renovate for all repos"
	var inputs map[string]string
	if only != (domain.RepoRef{}) {
		what = "run Renovate for " + only.String()
		inputs = map[string]string{"repo": only.String()}
	}
	w := s.workflow()
	d, ok := s.f.(forge.WorkflowDispatcher)
	if !ok || w == (RenovateWorkflow{}) {
		return fmt.Errorf("%s: %w", what, forge.ErrUnsupported)
	}
	repo, ok := s.cachedWorkflowRepo(w.Repo)
	if !ok {
		return fmt.Errorf("%s: %s is not in the repo list: %w", what, w.Repo, forge.ErrNotFound)
	}
	if a := s.workflowAvailability(repo); !a.OK {
		return fmt.Errorf("%s: %s", what, a.Reason)
	}
	return s.do(ctx, what, func(ctx context.Context) error {
		return d.DispatchWorkflow(ctx, repo.RepoRef, w.File, repo.DefaultBranch, inputs)
	})
}

func (s *Service) workflow() RenovateWorkflow {
	if s.renovWorkflow == nil {
		return RenovateWorkflow{}
	}
	return s.renovWorkflow()
}

// cachedWorkflowRepo matches owner and name without case.
func (s *Service) cachedWorkflowRepo(want domain.RepoRef) (domain.Repo, bool) {
	repos, _, ok := s.PeekRepos()
	if !ok {
		return domain.Repo{}, false
	}
	i := slices.IndexFunc(repos, func(r domain.Repo) bool {
		return strings.EqualFold(r.Owner, want.Owner) && strings.EqualFold(r.Name, want.Name)
	})
	if i < 0 {
		return domain.Repo{}, false
	}
	return repos[i], true
}

// workflowAvailability reports whether dispatch on the cached repo r can run: the forge allows it, and r has a
// default branch to dispatch on.
func (s *Service) workflowAvailability(r domain.Repo) forge.Availability {
	if a := forge.Can(s.f, forge.ActDispatchWorkflow, r); !a.OK {
		return a
	}
	if r.DefaultBranch == "" {
		return forge.Availability{Reason: fmt.Sprintf("%s has no default branch", r.RepoRef)}
	}
	return forge.Availability{OK: true}
}
