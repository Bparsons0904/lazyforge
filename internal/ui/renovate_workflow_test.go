package ui

import (
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestRenovateWorkflowParsesByHostType(t *testing.T) {
	tests := []struct {
		name, hostType, in string
		want               core.RenovateWorkflow
	}{
		{"gitlab subgroup path", "gitlab", "group/sub/runner", core.RenovateWorkflow{Repo: domain.RepoRef{Owner: "group/sub", Name: "runner"}}},
		{"gitlab two-segment path", "gitlab", "group/runner", core.RenovateWorkflow{Repo: domain.RepoRef{Owner: "group", Name: "runner"}}},
		{"gitlab refuses a workflow file", "gitlab", "a/b/renovate.yml", core.RenovateWorkflow{}},
		{"gitlab unset", "gitlab", "", core.RenovateWorkflow{}},
		{
			"forgejo owner/repo/file", "forgejo", "deadstyle/forgejo/renovate.yml",
			core.RenovateWorkflow{Repo: domain.RepoRef{Owner: "deadstyle", Name: "forgejo"}, File: "renovate.yml"},
		},
		{"forgejo refuses a project path", "forgejo", "group/runner", core.RenovateWorkflow{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renovateWorkflow(tt.hostType, tt.in); got != tt.want {
				t.Errorf("renovateWorkflow(%q, %q) = %+v, want %+v", tt.hostType, tt.in, got, tt.want)
			}
		})
	}
}

func TestWorkflowCopyByForgeKind(t *testing.T) {
	if got, want := workflowFormatMsg(forge.KindGitLab), "Use a project path, for example group/renovate-runner"; got != want {
		t.Errorf("GitLab hint %q, want %q", got, want)
	}
	if got, want := workflowPlaceholder(forge.KindGitLab), "group/renovate-runner"; got != want {
		t.Errorf("GitLab placeholder %q, want %q", got, want)
	}
	for _, k := range []forge.Kind{forge.KindForgejo, forge.KindGitea, forge.KindGitHub} {
		if got, want := workflowFormatMsg(k), "Use owner/repo/file, for example deadstyle/forgejo/renovate.yml"; got != want {
			t.Errorf("%s hint %q, want %q", k, got, want)
		}
		if got, want := workflowPlaceholder(k), "deadstyle/forgejo/renovate.yml"; got != want {
			t.Errorf("%s placeholder %q, want %q", k, got, want)
		}
	}
}
