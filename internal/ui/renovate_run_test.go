package ui

import (
	"fmt"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

// confirm sends one key and feeds the messages its command produced back into the model.
func confirm(t *testing.T, m Model, k string) Model {
	t.Helper()
	m, msgs := step(t, m, k)
	for _, msg := range msgs {
		m, _ = update(m, msg)
	}
	return m
}

// wfRepo is the criteria's workflow repo and filesRepo a second repo on the same host.
var (
	wfRepo    = domain.RepoRef{Owner: "deadstyle", Name: "forgejo"}
	filesRepo = domain.RepoRef{Owner: "deadstyle", Name: "files"}
	wfOpts    = core.Options{RenovateWorkflow: core.RenovateWorkflow{Repo: wfRepo, File: "renovate.yml"}}
)

// starOpts points the Renovate workflow at infra, the repo starRunFake holds Renovate PRs on.
var starOpts = core.Options{RenovateWorkflow: core.RenovateWorkflow{Repo: infra, File: "renovate.yml"}}

// wfFake holds wfRepo at access on default branch main, and filesRepo with write access; each has one PR so
// the boxes have a row for l to open details on.
func wfFake(access domain.Access) *forgetest.Fake {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://f.test", ChangeRequestTerm: "PR", User: "you"})
	f.AddRepo(domain.Repo{RepoRef: wfRepo, Access: access, DefaultBranch: "main"})
	f.AddRepo(domain.Repo{RepoRef: filesRepo, Access: domain.AccessWrite, DefaultBranch: "main"})
	for _, r := range []domain.RepoRef{wfRepo, filesRepo} {
		f.AddChangeRequest(r, domain.ChangeRequest{Number: 1, Title: "pr1", HeadSHA: "sha1", CI: domain.CIPass, WebURL: "https://f.test/pulls/1"})
	}
	return f
}

// starRunFake has the Renovate PRs and a dashboard that fill the ★ boxes, with infra as the workflow repo.
func starRunFake() *forgetest.Fake {
	body := "## Awaiting Schedule\n\n- [ ] <!-- unlimit-branch=renovate/a -->Update a\n"
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, URL: "https://f.test", ChangeRequestTerm: "PR", User: "you"})
	f.AddRepo(domain.Repo{RepoRef: infra, Access: domain.AccessWrite, DefaultBranch: "main"})
	f.AddRepo(domain.Repo{RepoRef: homelab, Access: domain.AccessWrite, DefaultBranch: "main"})
	f.AddChangeRequest(infra, rvPostgres(17, domain.CIPass))
	f.AddChangeRequest(homelab, rvPostgres(42, domain.CIFail))
	f.AddIssue(homelab, domain.Issue{Number: 12, Title: "Dependency Dashboard", Body: body})
	return f
}

// nLevels are the places N can be pressed from, with the keys that reach each and the level they land on.
var nLevels = []struct {
	name  string
	keys  []string
	level level
}{
	{"★ row", nil, levelRepos},
	{"repo row", []string{"j"}, levelRepos},
	{"repo boxes", []string{"j", "l"}, levelBoxes},
	{"repo details", []string{"j", "l", "l"}, levelDetails},
	{"★ [1]", []string{"l", "1"}, levelBoxes},
	{"★ [2]", []string{"l", "2"}, levelBoxes},
	{"★ [3]", []string{"l", "3"}, levelBoxes},
	{"★ [4]", []string{"l", "4"}, levelBoxes},
	{"★ [5]", []string{"l", "5"}, levelBoxes},
}

func TestRunRenovateEnabledAtEveryLevel(t *testing.T) {
	for _, lv := range nLevels {
		t.Run(lv.name, func(t *testing.T) {
			m := press(t, rvSized(t, wfFake(domain.AccessWrite), wfOpts, 120, 40), lv.keys...)
			if m.level != lv.level {
				t.Fatalf("level %v, want %v", m.level, lv.level)
			}
			if !m.keys.RunRenovate.Enabled() {
				t.Error("N disabled with a workflow repo the token can write")
			}
		})
	}
}

func TestRunRenovateHiddenEverywhereWhenDisabled(t *testing.T) {
	for _, tc := range []struct {
		name   string
		access domain.Access
		opts   core.Options
	}{
		{"no renovate_workflow", domain.AccessWrite, core.Options{}},
		{"workflow repo read-only", domain.AccessRead, wfOpts},
	} {
		for _, lv := range nLevels {
			t.Run(tc.name+"/"+lv.name, func(t *testing.T) {
				m := press(t, rvSized(t, wfFake(tc.access), tc.opts, 120, 40), lv.keys...)
				if m.keys.RunRenovate.Enabled() {
					t.Error("N enabled")
				}
				if h := strip(m.statusBar()); strings.Contains(h, "run Renovate") {
					t.Errorf("hints show N:\n%s", h)
				}
				next, cmd := update(m, keyMsg("N"))
				if next.dialog != nil || cmd != nil {
					t.Error("N opened a dialog or ran a command")
				}
			})
		}
		t.Run(tc.name+"/help", func(t *testing.T) {
			m := press(t, rvSized(t, wfFake(tc.access), tc.opts, 120, 40), "?")
			if s := screen(m); strings.Contains(s, "run Renovate") {
				t.Errorf("help shows N:\n%s", s)
			}
		})
	}
}

func TestRunRenovateShownInHintsAndHelp(t *testing.T) {
	for _, lv := range nLevels {
		t.Run(lv.name, func(t *testing.T) {
			// Wide enough that the boxes and details hints are not cut off before N.
			m := press(t, rvSized(t, wfFake(domain.AccessWrite), wfOpts, 240, 40), lv.keys...)
			if h := strip(m.statusBar()); !strings.Contains(h, "run Renovate") {
				t.Errorf("hints lack N:\n%s", h)
			}
		})
	}
	// body is the help frame alone; the screen would also match the status bar's N hint.
	m := press(t, rvSized(t, wfFake(domain.AccessWrite), wfOpts, 120, 40), "?")
	if b := strip(m.body()); !strings.Contains(b, "run Renovate") {
		t.Errorf("help overlay lacks N:\n%s", b)
	}
}

// atFiles is the workflow fake with the cursor on filesRepo, the row the dialog tests open N on. The ★ row
// comes first, then wfRepo, then filesRepo.
func atFiles(t *testing.T, f *forgetest.Fake) Model {
	t.Helper()
	return press(t, rvSized(t, f, wfOpts, 120, 40), "j", "j")
}

func TestRunRenovateRepoDialogOffersThisRepoOrAll(t *testing.T) {
	m, cmd := update(atFiles(t, wfFake(domain.AccessWrite)), keyMsg("N"))
	if cmd != nil {
		t.Fatal("N ran a command; the dialog should open without a forge call")
	}
	if m.dialog == nil {
		t.Fatal("N opened no dialog")
	}
	s := screen(m)
	for _, want := range []string{"Run Renovate", "› this repo (deadstyle/files)", "  all repos"} {
		if !strings.Contains(s, want) {
			t.Errorf("dialog lacks %q:\n%s", want, s)
		}
	}
}

func TestRunRenovateRepoDialogActions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		keys   []string
		repo   string
		status string
	}{
		{"enter runs this repo", []string{"N", "enter"}, "deadstyle/files", "Renovate run started for deadstyle/files"},
		{"y runs this repo", []string{"N", "y"}, "deadstyle/files", "Renovate run started for deadstyle/files"},
		{"k stays on this repo", []string{"N", "k", "enter"}, "deadstyle/files", "Renovate run started for deadstyle/files"},
		{"j then enter runs all repos", []string{"N", "j", "enter"}, "", "Renovate run started for all repos"},
		{"j stays on all repos", []string{"N", "j", "j", "enter"}, "", "Renovate run started for all repos"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := wfFake(domain.AccessWrite)
			m := atFiles(t, f)
			m = press(t, m, tc.keys[:len(tc.keys)-1]...)
			m = confirm(t, m, tc.keys[len(tc.keys)-1])
			if m.dialog != nil {
				t.Error("confirm left the dialog open")
			}
			muts := f.Mutations()
			if len(muts) != 1 || muts[0].Op != "dispatch-workflow" || muts[0].Workflow != "renovate.yml" {
				t.Fatalf("mutations %+v, want one dispatch of renovate.yml", muts)
			}
			wantInputs := 0
			if tc.repo != "" {
				wantInputs = 1
			}
			if got := muts[0].Inputs; len(got) != wantInputs || got["repo"] != tc.repo {
				t.Errorf("inputs %v, want repo %q or none", got, tc.repo)
			}
			if m.statusErr || m.status != tc.status {
				t.Errorf("status %q (error %v), want %q", m.status, m.statusErr, tc.status)
			}
		})
	}
}

func TestRunRenovateRepoDialogEscRecordsNothing(t *testing.T) {
	f := wfFake(domain.AccessWrite)
	m := press(t, atFiles(t, f), "N", "esc")
	if m.dialog != nil {
		t.Error("esc left the dialog open")
	}
	if n := len(f.Mutations()); n != 0 {
		t.Errorf("%d mutations after esc, want none", n)
	}
}

func TestRunRenovateFailureShowsForgeReason(t *testing.T) {
	f := wfFake(domain.AccessWrite)
	m := press(t, atFiles(t, f), "N")
	f.FailNext(fmt.Errorf("%w: workflow has no workflow_dispatch", forge.ErrRefused))
	m = confirm(t, m, "enter")
	if !m.statusErr || !strings.Contains(m.status, "workflow has no workflow_dispatch") {
		t.Errorf("status %q (error %v), want the forge's reason in error state", m.status, m.statusErr)
	}
	if s := screen(m); !strings.Contains(s, "workflow has no workflow_dispatch") {
		t.Errorf("status bar lacks the forge's reason:\n%s", s)
	}
}

func TestRunRenovateStarRowIsHostWide(t *testing.T) {
	f := starRunFake()
	m, cmd := update(rvSized(t, f, starOpts, 120, 40), keyMsg("N"))
	if cmd != nil || m.dialog == nil {
		t.Fatal("N on the ★ row should open the dialog without a forge call")
	}
	if s := screen(m); !strings.Contains(s, "Start a Renovate run on all repos?") {
		t.Fatalf("★ dialog lacks its line:\n%s", s)
	}
	m = confirm(t, m, "enter")
	muts := f.Mutations()
	if len(muts) != 1 || len(muts[0].Inputs) != 0 {
		t.Fatalf("mutations %+v, want one all-repos dispatch", muts)
	}
	if m.statusErr || m.status != "Renovate run started for all repos" {
		t.Errorf("status %q (error %v)", m.status, m.statusErr)
	}
}

func TestRunRenovateStarBoxesNameTheirRepo(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
		want string
	}{
		{"[2] group row is host-wide", []string{"l", "2"}, "Start a Renovate run on all repos?"},
		{"[1] by-repo row names its repo", []string{"l", "1"}, "this repo (home/infra)"},
		{"[3] PR row names its repo", []string{"l", "3"}, "this repo (home/infra)"},
		{"[4] dashboard row names its repo", []string{"l", "4"}, "this repo (home/homelab)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := press(t, rvSized(t, starRunFake(), starOpts, 120, 40), tc.keys...)
			m, _ = update(m, keyMsg("N"))
			if m.dialog == nil {
				t.Fatal("N opened no dialog")
			}
			if s := screen(m); !strings.Contains(s, tc.want) {
				t.Errorf("dialog lacks %q:\n%s", tc.want, s)
			}
		})
	}
}

// TestAppRunRenovateFollowsHostWorkflow checks the App wiring: N is off until the repo list loads, then follows
// the host's renovate_workflow, and a missing or malformed value keeps it off.
func TestAppRunRenovateFollowsHostWorkflow(t *testing.T) {
	for _, tc := range []struct {
		name, workflow string
		want           bool
	}{
		{"workflow set", "deadstyle/forgejo/renovate.yml", true},
		{"workflow empty", "", false},
		{"workflow malformed", "not-a-workflow", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := twoHosts()
			h := cfg.Hosts["a"]
			h.RenovateWorkflow = tc.workflow
			cfg.Hosts["a"] = h
			a, _ := testApp(t, Deps{Config: cfg, Host: "a", Forge: wfFake(domain.AccessWrite)})
			if a.session.keys.RunRenovate.Enabled() {
				t.Fatal("N enabled before the repo list loaded")
			}
			a = appRun(t, a, appExec(a.Init())...)
			if got := a.session.keys.RunRenovate.Enabled(); got != tc.want {
				t.Errorf("N enabled %v, want %v", got, tc.want)
			}
		})
	}
}
