package forge

import (
	"fmt"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

// Action is something the user can ask the UI to do.
type Action int

// Actions.
const (
	ActMerge Action = iota
	ActApprove
	ActClose
	ActComment
	ActEditIssue
	ActRuns
	ActLogs
	ActLabels
	ActAssets
	ActReadme
	ActBranches
	ActFiles
	ActUpdateBranch
	ActDispatchWorkflow
)

// Availability says whether an action is allowed and, if not, why.
type Availability struct {
	OK     bool
	Reason string
}

// Can checks capability, then the server gate, then repo permission; the first failure wins.
func Can(f Forge, a Action, r domain.Repo) Availability {
	if !hasCapability(f, a) {
		return Availability{Reason: fmt.Sprintf("%s doesn't support this", f.Info().Kind)}
	}
	if err := f.Gate(a); err != nil {
		return Availability{Reason: err.Error()}
	}
	need := domain.AccessRead
	switch a {
	case ActMerge, ActApprove, ActClose, ActEditIssue, ActLabels, ActUpdateBranch, ActDispatchWorkflow:
		need = domain.AccessWrite
	}
	if r.Access < need {
		level := "read"
		if need == domain.AccessWrite {
			level = "write"
		}
		return Availability{Reason: fmt.Sprintf("you don't have %s access to %s", level, r.RepoRef)}
	}
	return Availability{OK: true}
}

func hasCapability(f Forge, a Action) bool {
	var ok bool
	switch a {
	case ActLabels:
		_, ok = f.(Labeler)
	case ActApprove:
		_, ok = f.(Approver)
	case ActRuns:
		_, ok = f.(RunLister)
	case ActLogs:
		_, ok = f.(LogReader)
	case ActAssets:
		_, ok = f.(AssetReader)
	case ActReadme:
		_, ok = f.(ReadmeReader)
	case ActBranches:
		_, ok = f.(BranchReader)
	case ActFiles:
		_, ok = f.(TreeReader)
	case ActUpdateBranch:
		_, ok = f.(BranchUpdater)
	case ActDispatchWorkflow:
		_, ok = f.(WorkflowDispatcher)
	default:
		ok = true
	}
	return ok
}
