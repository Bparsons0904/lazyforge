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

const body = "This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [postgres](https://x) | major | `16` → `17` |\n"

func TestRenovateFillAndScan(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, User: "bob", ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
	f.AddChangeRequest(repoA, domain.ChangeRequest{Number: 1, State: domain.StateOpen, Author: "bot", Title: "postgres docker tag", Body: body})
	f.AddChangeRequest(repoA, domain.ChangeRequest{Number: 2, State: domain.StateOpen, Author: "bot", Body: "free text"})
	f.AddChangeRequest(repoA, domain.ChangeRequest{Number: 3, State: domain.StateOpen, Author: "human", Body: body})
	f.AddIssue(repoA, domain.Issue{Number: 9, State: domain.StateOpen, Title: "Dependency Dashboard", Body: "## S\n- [ ] x\n"})
	f.AddIssue(repoA, domain.Issue{Number: 10, State: domain.StateOpen, Title: "Bug"})
	svc := core.New(f, core.Options{RenovateUser: "bot"})
	repo := domain.Repo{RepoRef: repoA}

	if _, ok := svc.PeekRenovateScan(repo); ok {
		t.Fatal("Peek ok before any fetch")
	}
	scan, err := svc.RenovateScan(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.PRs) != 2 || scan.PRs[0].Number != 1 || scan.PRs[1].Number != 2 {
		t.Fatalf("PRs = %+v, want only the bot's #1 and #2", scan.PRs)
	}
	if len(scan.PRs[0].Renovate) != 1 || scan.PRs[0].Renovate[0].Ecosystem != "docker" || scan.PRs[1].Renovate != nil {
		t.Errorf("Renovate fill: %+v / %+v", scan.PRs[0].Renovate, scan.PRs[1].Renovate)
	}
	if len(scan.Dashboards) != 1 || scan.Dashboards[0].Issue.Number != 9 || len(scan.Dashboards[0].Entries) != 1 {
		t.Errorf("dashboards = %+v", scan.Dashboards)
	}
	crs, _, _ := svc.PeekChangeRequests(repoA)
	for _, cr := range crs {
		if cr.Number == 3 && cr.Renovate != nil {
			t.Error("non-Renovate PR got Renovate filled")
		}
		if cr.Number == 1 && cr.Renovate == nil {
			t.Error("Peek lost the filled Renovate")
		}
	}
	if peeked, ok := svc.PeekRenovateScan(repo); !ok || len(peeked.PRs) != 2 {
		t.Errorf("Peek = %+v ok=%v", peeked, ok)
	}

	got := svc.Recheck(context.Background(), []core.Target{{Repo: repoA, CR: domain.ChangeRequest{Number: 1}}})
	if got[0].Err != nil || len(got[0].Target.CR.Renovate) != 1 {
		t.Errorf("recheck = %+v", got[0])
	}
}

func TestRenovateScanErrors(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo, ChangeRequestTerm: "PR"})
	f.AddRepo(domain.Repo{RepoRef: repoA, LastActivity: t0})
	svc := core.New(failRepo{Fake: f, bad: repoA}, core.Options{})
	if _, err := svc.RenovateScan(context.Background(), domain.Repo{RepoRef: repoA}); err == nil || !strings.Contains(err.Error(), repoA.String()) {
		t.Fatalf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := core.New(f, core.Options{}).RenovateScan(ctx, domain.Repo{RepoRef: repoA}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
