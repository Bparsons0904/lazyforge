package gitea_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitea"
)

func TestProbeRequestShape(t *testing.T) {
	var path, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
	}))
	defer srv.Close()
	if _, err := gitea.Probe(context.Background(), srv.URL, srv.Client()); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/version" || auth != "" {
		t.Fatalf("path %q auth %q", path, auth)
	}
}

func TestProbeKindFromVersion(t *testing.T) {
	tests := []struct {
		version string
		want    forge.Kind
	}{
		{"16.0.5+gitea-1.22.0", forge.KindForgejo},
		{"7.0.0+gitea-1.21.0", forge.KindForgejo},
		{"1.22.0", forge.KindGitea},
		{"1.22.0+dev-123-gabcdef", forge.KindGitea},
		{"1.21.11-rc1", forge.KindGitea},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"version":"` + tt.version + `"}`))
			}))
			defer srv.Close()
			got, err := gitea.Probe(context.Background(), srv.URL, nil)
			if err != nil || got != tt.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestProbeBadResponses(t *testing.T) {
	tests := []struct {
		name string
		code int
		body string
	}{
		{"404 with json", 404, `{"message":"not found"}`},
		{"404 html", 404, `<html>nope</html>`},
		{"200 html", 200, `<!doctype html><title>login</title>`},
		{"200 empty body", 200, ``},
		{"200 empty version", 200, `{"version":""}`},
		{"200 json array", 200, `[]`},
		{"200 wrong shape", 200, `{"v":"1.0"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.code)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			kind, err := gitea.Probe(context.Background(), srv.URL, nil)
			if !errors.Is(err, forge.ErrNotFound) || kind != "" {
				t.Fatalf("got %q, %v", kind, err)
			}
		})
	}
}

func TestProbeUnauthorizedIsReachableUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	kind, err := gitea.Probe(context.Background(), srv.URL, nil)
	if err != nil || kind != "" {
		t.Fatalf("got %q, %v", kind, err)
	}
}

func TestProbeCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"1.22.0"}`))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := gitea.Probe(ctx, srv.URL, nil)
	if !errors.Is(err, context.Canceled) || errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
