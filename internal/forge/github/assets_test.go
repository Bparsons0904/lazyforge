package github_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

func assetURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// The web root sees the token on the first hop, but the object store (same hostname, other port) must not.
func TestOpenAssetSendsTokenOnlyOnWebRoot(t *testing.T) {
	f, s := newForge(t)
	rc, err := f.OpenAsset(t.Context(), assetURL(t, s.URL+"/user-attachments/assets/"+assetID))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, forgetest.DemoPNG) {
		t.Fatalf("read %d bytes, want the demo PNG", len(got))
	}
	// wrap answers 401 without the Bearer token, so a 302 here proves the first hop carried it.
	hops := s.requestsTo("GET", "/user-attachments/assets/"+assetID)
	if len(hops) != 1 || hops[0].Status != http.StatusFound {
		t.Fatalf("web root requests %+v, want one 302", hops)
	}
	seen := s.assets.headers()
	if len(seen) != 1 {
		t.Fatalf("asset host saw %d requests, want 1", len(seen))
	}
	if a := seen[0].Get("Authorization"); a != "" {
		t.Fatalf("token sent to the asset host %s: %q", s.assets.URL, a)
	}
}

func TestOpenAssetRefusesOtherOrigins(t *testing.T) {
	f, s := newForge(t)
	for _, raw := range []string{
		s.assets.URL + "/" + assetID + ".png?X-Amz-Signature=signed",
		"https://github.com/user-attachments/assets/" + assetID,
	} {
		rc, err := f.OpenAsset(t.Context(), assetURL(t, raw))
		if err == nil {
			_ = rc.Close()
		}
		if !errors.Is(err, forge.ErrUnsupported) {
			t.Errorf("%s: got %v, want ErrUnsupported", raw, err)
		}
		if err != nil && strings.Contains(err.Error(), "Signature") {
			t.Errorf("refusal carries the query: %v", err)
		}
	}
	if n := len(s.requests()) + len(s.assets.headers()); n != 0 {
		t.Fatalf("%d requests sent for refused URLs", n)
	}
}

func TestOpenAssetMissingIsNotFound(t *testing.T) {
	f, s := newForge(t)
	_, err := f.OpenAsset(t.Context(), assetURL(t, s.URL+"/user-attachments/assets/private?token=secret"))
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error carries the query: %v", err)
	}
}

func TestOpenAssetErrorsOmitSignedURL(t *testing.T) {
	f, s := newForge(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	s.mu.Lock()
	s.assetRedirects["dead"] = deadURL + "/obj.png?X-Amz-Signature=secret"
	s.assetRedirects["forbidden"] = s.assets.URL + "/obj.png?X-Amz-Signature=secret"
	s.mu.Unlock()
	for _, id := range []string{"dead", "forbidden"} {
		rc, err := f.OpenAsset(t.Context(), assetURL(t, s.URL+"/user-attachments/assets/"+id))
		if err == nil {
			_ = rc.Close()
			t.Fatalf("%s: OpenAsset succeeded", id)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: error carries the signed URL: %v", id, err)
		}
	}
}
