package gitea_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitea"
)

func TestProbe(t *testing.T) {
	serve := func(code int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Error("probe sent Authorization")
			}
			w.WriteHeader(code)
			_, _ = w.Write([]byte(body))
		}))
	}
	tests := []struct {
		name     string
		code     int
		body     string
		kind     forge.Kind
		notFound bool
	}{
		{"forgejo", 200, `{"version":"16.0.5+gitea-1.22.0"}`, forge.KindForgejo, false},
		{"gitea", 200, `{"version":"1.22.0"}`, forge.KindGitea, false},
		{"unauthorized", 401, `{"message":"no"}`, "", false},
		{"404", 404, `{}`, "", true},
		{"html", 200, `<html></html>`, "", true},
		{"no version", 200, `{}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serve(tt.code, tt.body)
			defer srv.Close()
			kind, err := gitea.Probe(context.Background(), srv.URL, nil)
			if tt.notFound != errors.Is(err, forge.ErrNotFound) || (!tt.notFound && err != nil) || kind != tt.kind {
				t.Fatalf("got %q, %v", kind, err)
			}
		})
	}
	t.Run("refused", func(t *testing.T) {
		srv := serve(200, `{}`)
		srv.Close()
		_, err := gitea.Probe(context.Background(), srv.URL, nil)
		var op *net.OpError
		if !errors.As(err, &op) || errors.Is(err, forge.ErrNotFound) {
			t.Fatalf("got %v", err)
		}
	})
}
