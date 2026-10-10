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

func TestJobsAndLog(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo})
	r := domain.RepoRef{Owner: "o", Name: "r"}
	f.AddRun(r, domain.Run{ID: 7}, []domain.Job{{ID: 70, RunID: 7, Name: "build", Status: domain.CIFail}}, map[int64]string{70: "step 1\nboom\n"})
	svc := core.New(f, core.Options{})
	ctx := context.Background()

	js, err := svc.Jobs(ctx, r, 7)
	if err != nil || len(js) != 1 || js[0].Name != "build" {
		t.Fatalf("Jobs = %v, %v", js, err)
	}
	if log, err := svc.JobLog(ctx, r, 70); err != nil || log != "step 1\nboom\n" {
		t.Fatalf("JobLog = %q, %v", log, err)
	}
	if _, err := svc.JobLog(ctx, r, 99); !errors.Is(err, forge.ErrNotFound) {
		t.Errorf("JobLog of an unknown job = %v, want ErrNotFound", err)
	}
}

func TestJobLogKeepsTheTail(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo})
	r := domain.RepoRef{Owner: "o", Name: "r"}
	long := strings.Repeat("old line\n", core.LogKeep/9) + "last line\n"
	f.AddRun(r, domain.Run{ID: 1}, []domain.Job{{ID: 10, RunID: 1}}, map[int64]string{10: long})
	got, err := core.New(f, core.Options{}).JobLog(context.Background(), r, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "… earlier output omitted\n") || !strings.HasSuffix(got, "last line\n") {
		t.Errorf("tail = %.40q … %.20q", got, got[len(got)-20:])
	}
	if len(got) > core.LogKeep+64 {
		t.Errorf("kept %d bytes, want at most about %d", len(got), core.LogKeep)
	}
}

func TestFirstProblemJob(t *testing.T) {
	js := func(ss ...domain.CIState) []domain.Job {
		out := make([]domain.Job, len(ss))
		for i, s := range ss {
			out[i].Status = s
		}
		return out
	}
	tests := []struct {
		name string
		in   []domain.Job
		want int
	}{
		{"empty", nil, 0},
		{"failed wins", js(domain.CIPass, domain.CIFail, domain.CIRunning), 1},
		{"running next", js(domain.CIPass, domain.CIRunning, domain.CIPending), 1},
		{"else last", js(domain.CIPass, domain.CIPass), 1},
	}
	for _, tt := range tests {
		if got := core.FirstProblemJob(tt.in); got != tt.want {
			t.Errorf("%s: FirstProblemJob = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestJobLogShowsTheEndOfALogLongerThanTheCap(t *testing.T) {
	f := forgetest.NewFake(forge.HostInfo{Kind: forge.KindForgejo})
	r := domain.RepoRef{Owner: "o", Name: "r"}
	long := strings.Repeat("old line\n", 10*core.LogKeep/9) + "the very end\n"
	f.AddRun(r, domain.Run{ID: 1}, []domain.Job{{ID: 10, RunID: 1}}, map[int64]string{10: long})
	got, err := core.New(f, core.Options{}).JobLog(context.Background(), r, 10)
	if err != nil || !strings.HasSuffix(got, "the very end\n") {
		t.Errorf("the log's end must survive a log far past the cap: err %v, tail %q", err, got[max(len(got)-20, 0):])
	}
}
