package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

var backspace = tea.KeyPressMsg{Code: tea.KeyBackspace}

func rowIndex(t *testing.T, v settingsView, kind settingsRowKind, host string) int {
	t.Helper()
	for i, r := range settingsRows(v) {
		if r.kind == kind && r.host == host {
			return i
		}
	}
	t.Fatalf("no row %v for host %q", kind, host)
	return 0
}

// editRow opens the row's editor on a fresh settings screen with the cursor on it.
func editRow(t *testing.T, v settingsView, kind settingsRowKind, host string) settings {
	t.Helper()
	s := settings{cursor: rowIndex(t, v, kind, host)}
	s, _ = s.update(keyMsg("enter"), v, defaultKeys())
	return s
}

func typeInto(s settings, v settingsView, text string) (settings, settingsIntent) {
	in := settingsIntent{}
	for _, r := range text {
		s, in = s.update(keyMsg(string(r)), v, defaultKeys())
	}
	return s, in
}

func TestSettingsRenovateRowsShowValuesAndUnsetHints(t *testing.T) {
	v := settingsFixture()
	h := v.cfg.Hosts["a"]
	h.RenovateUser, h.RenovateWorkflow = "renovate-bot", "deadstyle/forgejo/renovate.yml"
	v.cfg.Hosts["a"] = h

	out := strip((settings{}).view(100, 40, v))
	for _, want := range []string{
		"Renovate", "a: Renovate bot", "renovate-bot", "a: Renovate workflow (N)", "deadstyle/forgejo/renovate.yml",
		"b: Renovate bot", "not set", "b: Renovate workflow (N)", "not set (N hidden)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
}

func TestSettingsEditWorkflowSavesTrimmedValue(t *testing.T) {
	v := settingsFixture()
	s := editRow(t, v, rowHostWorkflow, "b")
	if s.mode != modeInput {
		t.Fatalf("enter on the workflow row left mode %v, want the input", s.mode)
	}
	s, _ = typeInto(s, v, " deadstyle/forgejo/renovate.yml ")
	s, in := s.update(keyMsg("enter"), v, defaultKeys())
	if in.kind != intentChange || in.cfg.Hosts["b"].RenovateWorkflow != "deadstyle/forgejo/renovate.yml" {
		t.Fatalf("intent %+v, want the trimmed workflow on host b", in)
	}
	if s.mode != modeList {
		t.Fatalf("mode %v after saving, want the list", s.mode)
	}
	if v.cfg.Hosts["b"].RenovateWorkflow != "" {
		t.Fatal("input config mutated")
	}
}

func TestSettingsMalformedWorkflowStaysInTheFieldWithTheHint(t *testing.T) {
	v := settingsFixture()
	s := editRow(t, v, rowHostWorkflow, "b")
	s, _ = typeInto(s, v, "deadstyle/forgejo")
	s, in := s.update(keyMsg("enter"), v, defaultKeys())
	if in.kind != intentNone {
		t.Fatalf("intent %+v, want none for a malformed workflow", in)
	}
	if s.mode != modeInput || s.msg != workflowFormatMsg {
		t.Fatalf("mode %v msg %q, want the field open with %q", s.mode, s.msg, workflowFormatMsg)
	}
}

func TestSettingsClearingWorkflowStoresEmpty(t *testing.T) {
	v := settingsFixture()
	h := v.cfg.Hosts["a"]
	h.RenovateWorkflow = "deadstyle/forgejo/renovate.yml"
	v.cfg.Hosts["a"] = h

	s := editRow(t, v, rowHostWorkflow, "a")
	for range len(h.RenovateWorkflow) {
		s, _ = s.update(backspace, v, defaultKeys())
	}
	_, in := s.update(keyMsg("enter"), v, defaultKeys())
	if in.kind != intentChange || in.cfg.Hosts["a"].RenovateWorkflow != "" {
		t.Fatalf("intent %+v, want the workflow cleared", in)
	}
}

func TestSettingsEscCancelsAnEdit(t *testing.T) {
	v := settingsFixture()
	s := editRow(t, v, rowHostWorkflow, "b")
	s, _ = typeInto(s, v, "a/b/c.yml")
	s, in := s.update(keyMsg("esc"), v, defaultKeys())
	if in.kind != intentNone || s.mode != modeList {
		t.Fatalf("mode %v intent %+v, want the list and no change", s.mode, in)
	}
}

func TestSettingsTypingKeysAreNotShortcutsInTheField(t *testing.T) {
	v := settingsFixture()
	s := editRow(t, v, rowHostWorkflow, "b")
	s, in := typeInto(s, v, "jkqS")
	if in.kind != intentNone || s.mode != modeInput || s.input.Value() != "jkqS" {
		t.Fatalf("mode %v intent %+v value %q, want the keys typed into the field", s.mode, in, s.input.Value())
	}
}

func TestSettingsSpaceDoesNotOpenAnEditor(t *testing.T) {
	v := settingsFixture()
	s := settings{cursor: rowIndex(t, v, rowHostWorkflow, "b")}
	s, _ = s.update(keyMsg("space"), v, defaultKeys())
	if s.mode != modeList {
		t.Fatalf("space left mode %v, want the list", s.mode)
	}
}

func TestSettingsEditBotSavesAndNotesReconnectOnTheActiveHost(t *testing.T) {
	v := settingsFixture()
	s := editRow(t, v, rowHostRenovate, "a")
	s, _ = typeInto(s, v, "renovate-bot")
	s, in := s.update(keyMsg("enter"), v, defaultKeys())
	if in.kind != intentChange || in.cfg.Hosts["a"].RenovateUser != "renovate-bot" {
		t.Fatalf("intent %+v, want the bot saved on host a", in)
	}
	if !strings.Contains(s.msg, "next time you connect") {
		t.Errorf("msg %q, want the reconnect note for the connected host", s.msg)
	}

	s = editRow(t, v, rowHostRenovate, "b")
	s, _ = typeInto(s, v, "renovate-bot")
	s, _ = s.update(keyMsg("enter"), v, defaultKeys())
	if s.msg != "" {
		t.Errorf("msg %q for a host that isn't connected, want none", s.msg)
	}
}

func TestSettingsEditorFitsAnEightyColumnScreen(t *testing.T) {
	v := settingsFixture()
	s := editRow(t, v, rowHostWorkflow, "b")
	s, _ = typeInto(s, v, "a-long-owner-name/a-long-repo-name/renovate-weekly-workflow.yml")
	for i, l := range strings.Split(s.view(80, 30, v), "\n") {
		if w := lipgloss.Width(l); w > 80 {
			t.Errorf("line %d is %d columns wide:\n%s", i, w, l)
		}
	}
}

func TestSettingsHintsInTheEditor(t *testing.T) {
	v := settingsFixture()
	s := editRow(t, v, rowHostWorkflow, "b")
	var got []string
	for _, b := range s.hints(defaultKeys()) {
		got = append(got, b.Help().Desc)
	}
	if strings.Join(got, ",") != "save,cancel" {
		t.Fatalf("hints %v, want save and cancel", got)
	}
}

func TestSettingsWorkflowEditEnablesNLive(t *testing.T) {
	cfg := config.Config{Update: config.Update{Check: true}, Hosts: map[string]config.Host{
		"h": {Type: "forgejo", URL: "https://f.test", Token: "t"},
	}}
	a, _ := testApp(t, Deps{Config: cfg, Host: "h", Forge: wfFake(domain.AccessWrite)})
	a = appRun(t, a, appExec(a.Init())...)
	if a.session.keys.RunRenovate.Enabled() {
		t.Fatal("N enabled before any workflow is set")
	}

	a = appPress(t, a, "S")
	for a.settings.cursor < rowIndex(t, a.settingsView(), rowHostWorkflow, "h") {
		a = appPress(t, a, "j")
	}
	a = appPress(t, a, "enter")
	for _, r := range "deadstyle/forgejo/renovate.yml" {
		a = appPress(t, a, string(r))
	}
	a = appPress(t, a, "enter")

	if !a.session.keys.RunRenovate.Enabled() {
		t.Fatal("N not enabled after saving the workflow, want it live without reconnecting")
	}
	saved, err := config.Load(a.deps.ConfigPath)
	if err != nil || saved.Hosts["h"].RenovateWorkflow != "deadstyle/forgejo/renovate.yml" {
		t.Fatalf("saved workflow %q: %v", saved.Hosts["h"].RenovateWorkflow, err)
	}

	a = appPress(t, a, "enter")
	for range len("deadstyle/forgejo/renovate.yml") {
		a = appRun(t, a, backspace)
	}
	a = appPress(t, a, "enter")
	if a.session.keys.RunRenovate.Enabled() {
		t.Fatal("N still enabled after clearing the workflow")
	}
}

func TestSettingsFieldTakesPaste(t *testing.T) {
	cfg := config.Config{Hosts: map[string]config.Host{"h": {Type: "forgejo", URL: "https://f.test", Token: "t"}}}
	a, _ := testApp(t, Deps{Config: cfg, Host: "h", Forge: wfFake(domain.AccessWrite)})
	a = appPress(t, a, "S")
	for a.settings.cursor < rowIndex(t, a.settingsView(), rowHostWorkflow, "h") {
		a = appPress(t, a, "j")
	}
	a = appPress(t, a, "enter")
	a = appRun(t, a, tea.PasteMsg{Content: "deadstyle/forgejo/renovate.yml"})
	if got := a.settings.input.Value(); got != "deadstyle/forgejo/renovate.yml" {
		t.Fatalf("field holds %q after a paste", got)
	}
}
