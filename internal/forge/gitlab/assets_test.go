package gitlab_test

import (
	"context"
	"errors"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/gitlab"
)

const (
	secret      = "abc123"
	uploadPath  = "/uploads/" + secret + "/shot.png"
	apiUpload   = "/api/v4/projects/group%2Fsub%2Frepo/uploads/" + secret + "/shot.png"
	apiUploadID = "/api/v4/projects/11/uploads/" + secret + "/shot.png"
)

func newForgeAt(t *testing.T, prefix string) (*gitlab.Forge, *server) {
	t.Helper()
	return newForgeVia(t, prefix, nil)
}

// newForgeVia is newForgeAt with hc as the forge's client; nil means the server's own.
func newForgeVia(t *testing.T, prefix string, hc *http.Client) (*gitlab.Forge, *server) {
	t.Helper()
	s := newServerAt(t, prefix)
	if hc == nil {
		hc = s.Client()
	}
	f, err := gitlab.New(t.Context(), s.webURL(), testToken, hc)
	if err != nil {
		t.Fatal(err)
	}
	s.reset()
	return f, s
}

func resolverOf(t *testing.T, f *gitlab.Forge) forge.AssetResolver {
	t.Helper()
	r, ok := any(f).(forge.AssetResolver)
	if !ok {
		t.Fatal("gitlab.Forge has no ResolveAsset")
	}
	return r
}

func assetReaderOf(t *testing.T, f *gitlab.Forge) forge.AssetReader {
	t.Helper()
	ar, ok := any(f).(forge.AssetReader)
	if !ok {
		t.Fatal("gitlab.Forge is not a forge.AssetReader")
	}
	return ar
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func openAsset(t *testing.T, f *gitlab.Forge, raw string) (image.Config, error) {
	t.Helper()
	rc, err := assetReaderOf(t, f).OpenAsset(t.Context(), mustParse(t, raw))
	if err != nil {
		return image.Config{}, err
	}
	defer func() { _ = rc.Close() }()
	cfg, _, err := image.DecodeConfig(rc)
	return cfg, err
}

func TestResolveAssetMapsUploadLinksToTheProject(t *testing.T) {
	tests := []struct {
		name, prefix string
		repo         domain.RepoRef
		ref, want    string
	}{
		{"subgroup project", "", sub, uploadPath, "/group/sub/repo" + uploadPath},
		{"top-level project", "", domain.RepoRef{Owner: "solo", Name: "old"}, uploadPath, "/solo/old" + uploadPath},
		{"relative URL root", "/gitlab", sub, uploadPath, "/gitlab/group/sub/repo" + uploadPath},
		{"project-id form", "", sub, "/-/project/11" + uploadPath, "/-/project/11" + uploadPath},
		{"project-id form under a relative URL root", "/gitlab", sub, "/-/project/11" + uploadPath, "/gitlab/-/project/11" + uploadPath},
		{"escaped filename keeps its escaping", "", sub, "/uploads/" + secret + "/a%20b.png", "/group/sub/repo/uploads/" + secret + "/a%20b.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := newForgeAt(t, tt.prefix)
			got, ok := resolverOf(t, f).ResolveAsset(tt.repo, mustParse(t, tt.ref))
			if !ok {
				t.Fatalf("ok=false for %s, want true", tt.ref)
			}
			if want := s.URL + tt.want; got.String() != want {
				t.Errorf("got %s, want %s", got, want)
			}
		})
	}
}

func TestResolveAssetLeavesOtherLinksToTheCaller(t *testing.T) {
	f, s := newForgeAt(t, "")
	for _, ref := range []string{s.URL + uploadPath, "", "img/x.png", "/attachments/x", "uploads/" + secret} {
		if u, ok := resolverOf(t, f).ResolveAsset(sub, mustParse(t, ref)); ok {
			t.Errorf("ResolveAsset(%q) = %s, ok=true; want ok=false", ref, u)
		}
	}
}

func TestResolveAssetSeparatesProjects(t *testing.T) {
	f, _ := newForgeAt(t, "")
	a, okA := resolverOf(t, f).ResolveAsset(sub, mustParse(t, uploadPath))
	b, okB := resolverOf(t, f).ResolveAsset(other, mustParse(t, uploadPath))
	if !okA || !okB || a.String() == b.String() {
		t.Fatalf("same upload link in two projects resolved to %v (%v) and %v (%v), want different URLs", a, okA, b, okB)
	}
}

func TestOpenAssetReadsUploadsThroughTheAPI(t *testing.T) {
	tests := []struct {
		name, prefix, path, wantURI string
	}{
		{"project path form", "", "/group/sub/repo" + uploadPath, apiUpload},
		{"project path form under a relative URL root", "/gitlab", "/gitlab/group/sub/repo" + uploadPath, "/gitlab" + apiUpload},
		{"project-id form", "", "/-/project/11" + uploadPath, apiUploadID},
		{"project-id form under a relative URL root", "/gitlab", "/gitlab/-/project/11" + uploadPath, "/gitlab" + apiUploadID},
		{"dash-segment form", "", "/group/sub/repo/-" + uploadPath, apiUpload},
		{"escaped filename keeps its escaping", "", "/group/sub/repo/uploads/" + secret + "/a%20b.png", "/api/v4/projects/group%2Fsub%2Frepo/uploads/" + secret + "/a%20b.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := newForgeAt(t, tt.prefix)
			cfg, err := openAsset(t, f, s.URL+tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Width == 0 {
				t.Error("asset did not decode to a non-empty image")
			}
			reqs := s.requests()
			if len(reqs) != 1 || reqs[0].Method != http.MethodGet || reqs[0].URI != tt.wantURI || reqs[0].Token != testToken {
				t.Fatalf("requests %+v, want one GET %s with PRIVATE-TOKEN %q", reqs, tt.wantURI, testToken)
			}
		})
	}
}

func TestOpenAssetDoesNotCarryTheQueryOntoTheAPIURL(t *testing.T) {
	f, s := newForgeAt(t, "")
	if _, err := openAsset(t, f, s.URL+"/group/sub/repo"+uploadPath+"?x=1"); err != nil {
		t.Fatal(err)
	}
	if reqs := s.requests(); len(reqs) != 1 || reqs[0].URI != apiUpload {
		t.Fatalf("requests %+v, want one GET %s", reqs, apiUpload)
	}
}

func TestOpenAssetFetchesOtherOnOriginURLsUnchanged(t *testing.T) {
	f, s := newForgeAt(t, "")
	_, err := openAsset(t, f, s.URL+"/group/sub/repo/raw/x.png")
	if err == nil {
		t.Fatal("got nil, want an error from the 404 the fake serves outside the API")
	}
	if reqs := s.requests(); len(reqs) != 1 || reqs[0].URI != "/group/sub/repo/raw/x.png" {
		t.Fatalf("requests %+v, want one GET of the URL as given", reqs)
	}
}

func TestOpenAssetMapsStatuses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   error
	}{
		{"404 is not found, as pre-17.4 servers answer", 404, forge.ErrNotFound},
		{"401 is unauthorized", 401, forge.ErrUnauthorized},
		{"403 is unauthorized", 403, forge.ErrUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, s := newForgeAt(t, "")
			s.answer(http.MethodGet, "/uploads/", reply{Status: tt.status, Body: `{"message":"x"}`})
			_, err := openAsset(t, f, s.URL+"/group/sub/repo"+uploadPath)
			if !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestOpenAssetUnseededUploadIsNotFound(t *testing.T) {
	f, s := newForgeAt(t, "")
	_, err := openAsset(t, f, s.URL+"/group/sub/repo/uploads/"+secret+"/nope.png")
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestOpenAssetRefusesOtherOriginsWithoutARequest(t *testing.T) {
	f, s := newForgeAt(t, "")
	base := mustParse(t, s.URL)
	variants := map[string]string{
		"hostname": "http://other.invalid:" + base.Port() + "/group/sub/repo" + uploadPath,
		"port":     "http://" + base.Hostname() + ":1/group/sub/repo" + uploadPath,
		"scheme":   "https://" + base.Host + "/group/sub/repo" + uploadPath,
	}
	for name, raw := range variants {
		t.Run(name, func(t *testing.T) {
			_, err := openAsset(t, f, raw)
			if !errors.Is(err, forge.ErrUnsupported) {
				t.Fatalf("got %v, want ErrUnsupported", err)
			}
			if reqs := s.requests(); len(reqs) != 0 {
				t.Fatalf("requests %+v, want none", reqs)
			}
		})
	}
}

func TestOpenAssetErrorsNeverCarryTheTokenOrQuery(t *testing.T) {
	const leak = "SEKRET-QUERY"
	raw := "/group/sub/repo" + uploadPath + "?private_token=" + leak
	check := func(t *testing.T, err error) {
		t.Helper()
		if err == nil {
			t.Fatal("got nil error")
		}
		if msg := err.Error(); strings.Contains(msg, leak) || strings.Contains(msg, testToken) {
			t.Errorf("error %q carries the query or the token", msg)
		}
	}
	t.Run("server error", func(t *testing.T) {
		f, s := newForgeAt(t, "")
		s.answer(http.MethodGet, "/uploads/", reply{Status: 500, Body: `{"message":"boom"}`})
		_, err := openAsset(t, f, s.URL+raw)
		check(t, err)
	})
	t.Run("transport error on a redirect target", func(t *testing.T) {
		f, s := newForgeAt(t, "")
		hop := newHopServer(t, false)
		target := hop.URL + "/blob?sig=" + leak
		hop.Close()
		s.answer(http.MethodGet, "/uploads/", redirect(target))
		_, err := openAsset(t, f, s.URL+raw)
		check(t, err)
	})
}

// hopServer is a second origin that records the headers of every request it receives.
type hopServer struct {
	*httptest.Server
	mu   sync.Mutex
	seen []http.Header
}

func newHopServer(t *testing.T, tls bool) *hopServer {
	t.Helper()
	h := &hopServer{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.seen = append(h.seen, r.Header.Clone())
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{}")
	})
	if tls {
		h.Server = httptest.NewTLSServer(handler)
	} else {
		h.Server = httptest.NewServer(handler)
	}
	t.Cleanup(h.Close)
	return h
}

func (h *hopServer) headers() []http.Header {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]http.Header(nil), h.seen...)
}

func redirect(to string) reply {
	return reply{Status: http.StatusFound, Header: map[string]string{"Location": to}}
}

func requireNoCredentials(t *testing.T, hops []http.Header) {
	t.Helper()
	if len(hops) == 0 {
		t.Fatal("the redirect was never followed")
	}
	for _, h := range hops {
		if h.Get("PRIVATE-TOKEN") != "" || h.Get("Authorization") != "" {
			t.Errorf("off-origin hop carried credentials: %v", h)
		}
	}
}

func TestOpenAssetDropsCredentialsOnRedirectsOffOrigin(t *testing.T) {
	plain, secure := newHopServer(t, false), newHopServer(t, true)
	plainURL := mustParse(t, plain.URL)
	tests := []struct {
		name string
		to   string
		hop  *hopServer
	}{
		{"other port", plain.URL + "/blob", plain},
		{"other hostname", "http://localhost:" + plainURL.Port() + "/blob", plain},
		{"other scheme", secure.URL + "/blob", secure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// secure.Client trusts the TLS hop's test certificate, so the https hop gets past the handshake.
			f, s := newForgeVia(t, "", secure.Client())
			s.answer(http.MethodGet, "/uploads/", redirect(tt.to))
			_, _ = openAsset(t, f, s.URL+"/group/sub/repo"+uploadPath)
			requireNoCredentials(t, tt.hop.headers())
		})
	}
}

func TestOpenAssetKeepsTheTokenOnSameOriginRedirects(t *testing.T) {
	f, s := newForgeAt(t, "")
	s.answer(http.MethodGet, "/projects/group%2Fsub%2Frepo/uploads/", redirect(s.URL+apiUploadID))
	if _, err := openAsset(t, f, s.URL+"/group/sub/repo"+uploadPath); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 2 || reqs[1].URI != apiUploadID || reqs[1].Token != testToken {
		t.Fatalf("requests %+v, want the redirect followed with PRIVATE-TOKEN %q", reqs, testToken)
	}
}

func TestOpenAssetStopsAfterTenRedirects(t *testing.T) {
	f, s := newForgeAt(t, "")
	loop := make([]reply, 30)
	for i := range loop {
		loop[i] = redirect(s.URL + "/api/v4/projects/group%2Fsub%2Frepo/uploads/" + secret + "/loop.png")
	}
	s.answer(http.MethodGet, "/uploads/", loop...)
	if _, err := openAsset(t, f, s.URL+"/group/sub/repo"+uploadPath); err == nil {
		t.Fatal("got nil, want an error from the redirect loop")
	}
	if n := len(s.requests()); n > 11 {
		t.Fatalf("%d requests, want the loop cut at 10 redirects", n)
	}
}

func TestAPIGetRedirectedOffOriginDropsTheToken(t *testing.T) {
	f, s := newForgeAt(t, "")
	hop := newHopServer(t, false)
	s.answer(http.MethodGet, "/merge_requests/", redirect(hop.URL+"/elsewhere"))
	_, _ = f.GetChangeRequest(context.Background(), sub, openMR)
	requireNoCredentials(t, hop.headers())
}

// dotLinks are upload links whose secret or filename would walk to another path on the host.
var dotLinks = []string{
	"/uploads/../shot.png",
	"/uploads/./shot.png",
	"/uploads/" + secret + "/..",
	"/uploads/" + secret + "/.",
	"/uploads/%2e%2e/shot.png",
	"/uploads/%2E%2e/shot.png",
	"/uploads/.%2e/shot.png",
	"/uploads/" + secret + "/%2e%2E",
	"/uploads/" + secret + "/%2e",
	"/-/project/11/uploads/%2e%2e/shot.png",
	"/-/project/11/uploads/" + secret + "/..",
	"/uploads//shot.png",
	"/uploads/" + secret + "/",
}

func TestResolveAssetRefusesDotAndEmptySegments(t *testing.T) {
	f, _ := newForgeAt(t, "")
	for _, ref := range dotLinks {
		if u, ok := resolverOf(t, f).ResolveAsset(sub, mustParse(t, ref)); ok {
			t.Errorf("ResolveAsset(%q) = %s, ok=true; want ok=false", ref, u)
		}
	}
}

func TestOpenAssetRefusesDotAndEmptySegmentsWithoutARequest(t *testing.T) {
	f, s := newForgeAt(t, "")
	for _, ref := range dotLinks {
		for _, base := range []string{"/group/sub/repo", ""} {
			if base == "" && !strings.HasPrefix(ref, "/-/") {
				continue
			}
			if _, err := openAsset(t, f, s.URL+base+ref); err == nil {
				t.Errorf("OpenAsset(%s%s) succeeded, want an error", base, ref)
			}
			if reqs := s.requests(); len(reqs) != 0 {
				t.Fatalf("OpenAsset(%s%s) made requests %+v, want none", base, ref, reqs)
			}
		}
	}
}
