package ui

import (
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
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
