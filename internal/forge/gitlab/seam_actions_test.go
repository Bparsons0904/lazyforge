package gitlab

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func TestSeamRunToDomain(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	started := created.Add(time.Minute)
	dur := 90.0
	got := runToDomain(pipelineRun{ID: 1, IID: 7, Source: "push", Status: "success", CreatedAt: created, StartedAt: &started, Duration: &dur})
	if got.Workflow != "push" || !got.StartedAt.Equal(started) || got.Duration != 90*time.Second || got.Status != domain.CIPass {
		t.Errorf("name falls back to source, started_at wins, duration in seconds: %+v", got)
	}
	got = runToDomain(pipelineRun{Name: "Build", Source: "push", CreatedAt: created})
	if got.Workflow != "Build" || !got.StartedAt.Equal(created) || got.Duration != 0 {
		t.Errorf("name wins, created_at stands in, no duration is zero: %+v", got)
	}
}

func TestSeamJobLogDropsTokenOffOrigin(t *testing.T) {
	var gotToken string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("PRIVATE-TOKEN")
		_, _ = io.WriteString(w, "log\x1b[0m")
	}))
	defer other.Close()
	var apiToken string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiToken = r.Header.Get("PRIVATE-TOKEN")
		http.Redirect(w, r, other.URL+"/blob", http.StatusFound)
	}))
	defer api.Close()
	f, err := newForge(api.URL, "secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := f.JobLog(context.Background(), domain.RepoRef{Owner: "g", Name: "r"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	b, _ := io.ReadAll(rc)
	if apiToken != "secret" || gotToken != "" || string(b) != "log\x1b[0m" {
		t.Errorf("api token %q, redirected host token %q, body %q", apiToken, gotToken, b)
	}
}
