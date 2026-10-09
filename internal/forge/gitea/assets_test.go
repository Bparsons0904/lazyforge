package gitea_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitea"
)

var _ forge.AssetReader = (*gitea.Forge)(nil)

// requestLog counts the requests a server receives and records each one's Authorization values by path.
type requestLog struct {
	mu   sync.Mutex
	n    int
	auth map[string][]string
}

func (l *requestLog) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		l.n++
		if l.auth == nil {
			l.auth = map[string][]string{}
		}
		l.auth[r.URL.Path] = r.Header.Values("Authorization")
		l.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (l *requestLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n, l.auth = 0, nil
}

func (l *requestLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n
}

func (l *requestLog) authFor(path string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.auth[path]
}

// assetHost is a stub Gitea that connects like the real one and then serves the routes a test supplies.
type assetHost struct {
	*httptest.Server
	log requestLog
	f   *gitea.Forge
}

// newAssetHost starts the host and connects a gitea.Forge to it; the request log is cleared after connect.
func newAssetHost(t *testing.T, routes map[string]http.HandlerFunc) *assetHost {
	t.Helper()
	h := &assetHost{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, obj{"version": "16.0.5+gitea-1.22.0"})
	})
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, _ *http.Request) {
		writeRaw(w, 200, readFixture(t, "user.json"))
	})
	for pattern, fn := range routes {
		mux.HandleFunc(pattern, fn)
	}
	h.Server = httptest.NewServer(h.log.wrap(mux))
	t.Cleanup(h.Close)
	f, err := gitea.New(t.Context(), h.URL, testToken, h.Client())
	if err != nil {
		t.Fatal(err)
	}
	h.f = f
	h.log.reset()
	return h
}

// newOtherServer is a second origin the token must never reach; it answers every path with status and body.
func newOtherServer(t *testing.T, status int, body []byte) (*httptest.Server, *requestLog) {
	t.Helper()
	reqs := &requestLog{}
	srv := httptest.NewServer(reqs.wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})))
	t.Cleanup(srv.Close)
	return srv, reqs
}

// serveWithToken answers with body only when the request carries exactly the test token; anything else is a 401.
func serveWithToken(body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token "+testToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body)
	}
}

// readAsset opens raw through f and reads the whole body.
func readAsset(ctx context.Context, f forge.AssetReader, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	rc, err := f.OpenAsset(ctx, u)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func TestOpenAssetSameOriginSendsToken(t *testing.T) {
	h := newAssetHost(t, map[string]http.HandlerFunc{
		"GET /attachments/ok": serveWithToken(forgetest.DemoPNG),
	})
	got, err := readAsset(t.Context(), h.f, h.URL+"/attachments/ok")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, forgetest.DemoPNG) {
		t.Errorf("body is %d bytes, want the %d served bytes unchanged", len(got), len(forgetest.DemoPNG))
	}
	if want := []string{"token " + testToken}; !slices.Equal(h.log.authFor("/attachments/ok"), want) {
		t.Errorf("Authorization = %q, want %q", h.log.authFor("/attachments/ok"), want)
	}
}

func TestOpenAssetRefusesOtherOriginWithoutRequest(t *testing.T) {
	tests := []struct {
		name string
		raw  func(h *assetHost, other *httptest.Server) string
	}{
		{"other hostname", func(h *assetHost, _ *httptest.Server) string {
			return strings.Replace(h.URL, "127.0.0.1", "localhost", 1) + "/attachments/ok"
		}},
		{"other port", func(_ *assetHost, other *httptest.Server) string {
			return other.URL + "/attachments/ok"
		}},
		{"other scheme", func(h *assetHost, _ *httptest.Server) string {
			return "https://" + strings.TrimPrefix(h.URL, "http://") + "/attachments/ok"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			other, otherReqs := newOtherServer(t, http.StatusOK, forgetest.DemoPNG)
			h := newAssetHost(t, map[string]http.HandlerFunc{
				"GET /attachments/ok": serveWithToken(forgetest.DemoPNG),
			})
			_, err := readAsset(t.Context(), h.f, tt.raw(h, other))
			if !errors.Is(err, forge.ErrUnsupported) {
				t.Errorf("got %v, want ErrUnsupported", err)
			}
			if n := h.log.count(); n != 0 {
				t.Errorf("host received %d requests, want 0", n)
			}
			if n := otherReqs.count(); n != 0 {
				t.Errorf("other server received %d requests, want 0", n)
			}
		})
	}
}

func TestOpenAssetRedirectOffOriginStripsToken(t *testing.T) {
	other, otherReqs := newOtherServer(t, http.StatusOK, forgetest.DemoPNG)
	h := newAssetHost(t, map[string]http.HandlerFunc{
		"GET /attachments/redirect-off": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, other.URL+"/bytes", http.StatusFound)
		},
	})
	got, err := readAsset(t.Context(), h.f, h.URL+"/attachments/redirect-off")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, forgetest.DemoPNG) {
		t.Error("body is not the second server's bytes")
	}
	if vals := otherReqs.authFor("/bytes"); len(vals) != 0 {
		t.Errorf("second origin received Authorization %q, want none", vals)
	}
}

func TestOpenAssetRedirectSameOriginKeepsToken(t *testing.T) {
	h := newAssetHost(t, map[string]http.HandlerFunc{
		"GET /attachments/start": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/attachments/next", http.StatusFound)
		},
		"GET /attachments/next": serveWithToken(forgetest.DemoPNG),
	})
	got, err := readAsset(t.Context(), h.f, h.URL+"/attachments/start")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, forgetest.DemoPNG) {
		t.Error("body after the same-origin redirect is not the served bytes")
	}
	if want := []string{"token " + testToken}; !slices.Equal(h.log.authFor("/attachments/next"), want) {
		t.Errorf("second hop Authorization = %q, want %q", h.log.authFor("/attachments/next"), want)
	}
}

func TestOpenAssetStatusMapping(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{"401 is unauthorized", http.StatusUnauthorized, forge.ErrUnauthorized},
		{"403 is unauthorized", http.StatusForbidden, forge.ErrUnauthorized},
		{"404 is not found", http.StatusNotFound, forge.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := fmt.Sprintf("/attachments/%d", tt.status)
			h := newAssetHost(t, map[string]http.HandlerFunc{
				"GET " + path: func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "refused", tt.status)
				},
			})
			_, err := readAsset(t.Context(), h.f, h.URL+path)
			if !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), testToken) {
				t.Errorf("error %q contains the token", err)
			}
		})
	}
}

func TestOpenAssetNonFollowedStatusIsAnError(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"304 is not followed", http.StatusNotModified},
		{"300 is not followed", http.StatusMultipleChoices},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newAssetHost(t, map[string]http.HandlerFunc{
				"GET /attachments/status": func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(tt.status)
				},
			})
			if _, err := readAsset(t.Context(), h.f, h.URL+"/attachments/status"); err == nil {
				t.Errorf("status %d returned no error", tt.status)
			}
		})
	}
}

func TestOpenAssetRedirectLoopIsAnErrorWithoutQuery(t *testing.T) {
	h := newAssetHost(t, map[string]http.HandlerFunc{
		"GET /attachments/loop": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/attachments/loop?sig=SECRET123", http.StatusFound)
		},
	})
	_, err := readAsset(t.Context(), h.f, h.URL+"/attachments/loop")
	if err == nil {
		t.Fatal("a redirect loop returned no error")
	}
	if strings.Contains(err.Error(), "SECRET123") || strings.Contains(err.Error(), testToken) {
		t.Errorf("error %q leaks the redirect query or the token", err)
	}
	// The original request plus at most 10 followed redirects.
	const maxRequests = 11
	if n := h.log.count(); n > maxRequests {
		t.Errorf("loop made %d requests, want at most %d", n, maxRequests)
	}
}

func TestOpenAssetErrorsOmitRedirectQuery(t *testing.T) {
	t.Run("status error from another origin", func(t *testing.T) {
		other, _ := newOtherServer(t, http.StatusServiceUnavailable, []byte("unavailable"))
		h := newAssetHost(t, map[string]http.HandlerFunc{
			"GET /attachments/signed": func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, other.URL+"/obj?sig=SIGNED456", http.StatusFound)
			},
		})
		_, err := readAsset(t.Context(), h.f, h.URL+"/attachments/signed")
		if err == nil {
			t.Fatal("a 503 from the redirect target returned no error")
		}
		if strings.Contains(err.Error(), "SIGNED456") {
			t.Errorf("error %q contains the signed query", err)
		}
	})

	t.Run("transport error on the redirect target", func(t *testing.T) {
		dead := httptest.NewServer(http.NotFoundHandler())
		deadURL := dead.URL
		dead.Close()
		h := newAssetHost(t, map[string]http.HandlerFunc{
			"GET /attachments/gone": func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, deadURL+"/obj?sig=SIGNED789", http.StatusFound)
			},
		})
		_, err := readAsset(t.Context(), h.f, h.URL+"/attachments/gone")
		if err == nil {
			t.Fatal("a refused redirect target returned no error")
		}
		if strings.Contains(err.Error(), "SIGNED789") {
			t.Errorf("error %q contains the signed query", err)
		}
	})
}

func TestOpenAssetContractPath(t *testing.T) {
	h := newAssetHost(t, map[string]http.HandlerFunc{
		"GET /attachments/contract": serveWithToken(forgetest.DemoPNG),
	})
	t.Run("on-host path decodes as an image", func(t *testing.T) {
		got, err := readAsset(t.Context(), h.f, h.URL+"/attachments/contract")
		if err != nil {
			t.Fatal(err)
		}
		if _, format, err := image.DecodeConfig(bytes.NewReader(got)); err != nil || format != "png" {
			t.Errorf("served body decodes as %q, %v; want png", format, err)
		}
	})
	t.Run("unseeded on-host path is not found", func(t *testing.T) {
		_, err := readAsset(t.Context(), h.f, h.URL+"/attachments/unseeded")
		if !errors.Is(err, forge.ErrNotFound) {
			t.Errorf("got %v, want ErrNotFound", err)
		}
	})
}
