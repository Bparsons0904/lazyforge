package ui

import (
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestOnboardWorkflowFieldTakesTabFocus(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter", "enter")
	if o.step != stepRenovate {
		t.Fatalf("not on the Renovate step: %d", o.step)
	}
	o = obType(o, "x")
	o, _ = obKeys(o, "tab")
	o = obType(o, "y")
	if o.renovate.Value() != "x" {
		t.Errorf("username %q, want typing before tab", o.renovate.Value())
	}
	if o.workflow.Value() != "y" {
		t.Errorf("workflow %q, want typing after tab", o.workflow.Value())
	}
}

func TestOnboardRenovateStepExplainsTheRepoInput(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter", "enter")
	out := strip(o.view(100, 30))
	for _, want := range []string{"optional repo input", "README: Run Renovate now", "N (run Renovate now)"} {
		if !strings.Contains(out, want) {
			t.Errorf("Renovate step lacks %q:\n%s", want, out)
		}
	}
}

func TestOnboardWorkflowRejectsBadFormat(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter", "enter", "tab")
	o = obType(o, "not-a-workflow")
	o, _ = obKeys(o, "enter")
	if o.step != stepRenovate {
		t.Fatalf("bad workflow moved on: step %d", o.step)
	}
	if want := "Use owner/repo/file, for example deadstyle/forgejo/renovate.yml"; o.err != want {
		t.Errorf("err %q, want %q", o.err, want)
	}
}

func TestOnboardWorkflowBlankIsAccepted(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter", "enter", "enter")
	if o.step != stepName {
		t.Errorf("blank workflow not accepted: step %d err %q", o.step, o.err)
	}
}

func TestOnboardWorkflowSavesToHost(t *testing.T) {
	e := newObEnv()
	a, _ := testApp(t, Deps{Fresh: true, Probe: e.probe})
	a = appPress(t, a, "enter", "enter")
	for _, r := range "git.example.com" {
		a = appPress(t, a, string(r))
	}
	a = appPress(t, a, "enter", "t", "o", "k", "enter", "enter", "tab")
	for _, r := range "deadstyle/forgejo/renovate.yml" {
		a = appPress(t, a, string(r))
	}
	a = appPress(t, a, "enter", "enter", "enter")
	if a.screen != screenSession {
		t.Fatalf("screen %v onboarding err %q", a.screen, a.onboard.err)
	}
	c, err := config.Load(a.deps.ConfigPath)
	if err != nil || c.Hosts["example"].RenovateWorkflow != "deadstyle/forgejo/renovate.yml" {
		t.Fatalf("saved workflow %q: %v", c.Hosts["example"].RenovateWorkflow, err)
	}
}

func TestOnboardWorkflowTabReturnsToUsername(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter", "enter")
	o, _ = obKeys(o, "tab")
	o = obType(o, "w")
	o, _ = obKeys(o, "tab")
	o = obType(o, "me")
	if o.renovate.Value() != "me" || o.workflow.Value() != "w" {
		t.Errorf("username %q workflow %q, want the second tab to return typing to the username", o.renovate.Value(), o.workflow.Value())
	}
}

func TestOnboardWorkflowTypingKeepsLateSuggestion(t *testing.T) {
	e := newObEnv()
	e.f.AddChangeRequest(domain.RepoRef{Owner: "bob", Name: "one"}, domain.ChangeRequest{Number: 1, Author: "renovate-bot", SourceBranch: "renovate/go"})
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter")
	o, cmd := o.update(keyMsg("enter"), defaultKeys())
	late := appExec(cmd)
	o, _ = obKeys(o, "tab")
	o = obType(o, "y")
	o, _ = obRun(o, late...)
	if o.renovate.Value() != "renovate-bot" {
		t.Errorf("username %q, want the suggestion to fill it while only the workflow was typed in", o.renovate.Value())
	}
	if o.workflow.Value() != "y" {
		t.Errorf("workflow %q, want the typed value kept", o.workflow.Value())
	}
}

// editToRenovate walks an edit of host to the Renovate step, the same way TestOnboardEdit walks it to the name step.
func editToRenovate(t *testing.T, host config.Host) onboarding {
	t.Helper()
	e := newObEnv()
	o := e.start(onboardStart{edit: "home", host: host})
	o, _ = obKeys(o, "enter", "enter", "enter")
	if o.step != stepRenovate {
		t.Fatalf("edit not on the Renovate step: %d err %q", o.step, o.err)
	}
	return o
}

func TestOnboardEditPrefillsAndSavesWorkflow(t *testing.T) {
	host := config.Host{Type: "forgejo", URL: "https://h.example", Token: "tok", RenovateWorkflow: "deadstyle/forgejo/renovate.yml"}
	o := editToRenovate(t, host)
	if o.workflow.Value() != host.RenovateWorkflow {
		t.Fatalf("workflow prefill %q, want %q", o.workflow.Value(), host.RenovateWorkflow)
	}
	_, out := obKeys(o, "enter", "enter")
	var done onboardDoneMsg
	for _, m := range out {
		if d, ok := m.(onboardDoneMsg); ok {
			done = d
		}
	}
	if done.f == nil || done.host.RenovateWorkflow != host.RenovateWorkflow {
		t.Errorf("saved workflow %q, want %q unchanged", done.host.RenovateWorkflow, host.RenovateWorkflow)
	}
}

func TestOnboardEditClearedWorkflowSavesEmpty(t *testing.T) {
	host := config.Host{Type: "forgejo", URL: "https://h.example", Token: "tok", RenovateWorkflow: "deadstyle/forgejo/renovate.yml"}
	o := editToRenovate(t, host)
	o, _ = obKeys(o, "tab")
	o.workflow.SetValue("")
	_, out := obKeys(o, "enter", "enter")
	var done onboardDoneMsg
	for _, m := range out {
		if d, ok := m.(onboardDoneMsg); ok {
			done = d
		}
	}
	if done.f == nil || done.host.RenovateWorkflow != "" {
		t.Errorf("saved workflow %q, want it cleared", done.host.RenovateWorkflow)
	}
}

// gitlabRenovateStep walks an edit of a GitLab host to the Renovate step.
func gitlabRenovateStep(t *testing.T) onboarding {
	t.Helper()
	e := newObEnv()
	e.probeKind = forge.KindGitLab
	o := e.start(onboardStart{edit: "home", host: config.Host{Type: "gitlab", URL: "https://gl.example", Token: "tok"}})
	o, _ = obKeys(o, "enter", "enter", "enter")
	if o.step != stepRenovate {
		t.Fatalf("not on the Renovate step: %d err %q", o.step, o.err)
	}
	return o
}

func TestOnboardGitLabWorkflowRejectsANonPath(t *testing.T) {
	o := gitlabRenovateStep(t)
	o, _ = obKeys(o, "tab")
	o = obType(o, "renovate-runner")
	o, _ = obKeys(o, "enter")
	if o.step != stepRenovate {
		t.Fatalf("a non-path moved on: step %d", o.step)
	}
	if want := "Use a project path, for example group/renovate-runner"; o.err != want {
		t.Errorf("err %q, want %q", o.err, want)
	}
}

func TestOnboardGitLabWorkflowRejectsTheForgejoForm(t *testing.T) {
	o := gitlabRenovateStep(t)
	o, _ = obKeys(o, "tab")
	o = obType(o, "deadstyle/forgejo/renovate.yml")
	o, _ = obKeys(o, "enter")
	if o.step != stepRenovate || o.err != "Use a project path, for example group/renovate-runner" {
		t.Errorf("step %d err %q, want the Renovate step with the project-path hint", o.step, o.err)
	}
}

func TestOnboardGitLabWorkflowAcceptsASubgroupPath(t *testing.T) {
	o := gitlabRenovateStep(t)
	o, _ = obKeys(o, "tab")
	o = obType(o, "group/sub/renovate-runner")
	o, out := obKeys(o, "enter", "enter")
	if o.err != "" {
		t.Fatalf("err %q, want none for a project path", o.err)
	}
	var done onboardDoneMsg
	for _, m := range out {
		if d, ok := m.(onboardDoneMsg); ok {
			done = d
		}
	}
	if done.f == nil || done.host.RenovateWorkflow != "group/sub/renovate-runner" {
		t.Errorf("saved workflow %q, want group/sub/renovate-runner", done.host.RenovateWorkflow)
	}
}

func TestOnboardGitLabRenovateStepDescribesTheProjectPath(t *testing.T) {
	o := gitlabRenovateStep(t)
	if got := o.workflow.Placeholder; got != "group/renovate-runner" {
		t.Errorf("placeholder %q, want group/renovate-runner", got)
	}
	out := strings.ToLower(strip(o.view(100, 30)))
	for _, want := range []string{"project", "pipeline"} {
		if !strings.Contains(out, want) {
			t.Errorf("GitLab Renovate step lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "optional repo input") {
		t.Errorf("GitLab Renovate step still describes a workflow file input:\n%s", out)
	}
}

func TestOnboardForgejoRenovateStepKeepsItsPlaceholder(t *testing.T) {
	e := newObEnv()
	o, _ := obKeys(obType(e.toSignIn(t), "tok"), "enter", "enter")
	if got := o.workflow.Placeholder; got != "deadstyle/forgejo/renovate.yml" {
		t.Errorf("placeholder %q, want deadstyle/forgejo/renovate.yml", got)
	}
}
