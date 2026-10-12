package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func TestSeamResolveAsset(t *testing.T) {
	tests := []struct {
		web, owner, ref string
		want            string // "" means ok=false
	}{
		{"https://h", "group/sub", "/uploads/abc/x.png", "https://h/group/sub/proj/uploads/abc/x.png"},
		{"https://h/gitlab", "group", "/uploads/abc/a%20b.png", "https://h/gitlab/group/proj/uploads/abc/a%20b.png"},
		{"https://h/gitlab", "group", "/-/project/42/uploads/abc/x.png", "https://h/gitlab/-/project/42/uploads/abc/x.png"},
		{"https://h", "group", "https://h/uploads/abc/x.png", ""},
		{"https://h", "group", "", ""},
		{"https://h", "group", "img/x.png", ""},
		{"https://h", "group", "uploads/abc/x.png", ""},
		{"https://h", "group", "/attachments/x", ""},
		{"https://h", "group", "/-/project/abc/uploads/abc/x.png", ""},
	}
	for _, tt := range tests {
		f, err := newForge(tt.web, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		ref, err := url.Parse(tt.ref)
		if err != nil {
			t.Fatal(err)
		}
		u, ok := f.ResolveAsset(domain.RepoRef{Owner: tt.owner, Name: "proj"}, ref)
		switch {
		case tt.want == "" && ok:
			t.Errorf("%s under %s: got %s, want ok=false", tt.ref, tt.web, u)
		case tt.want != "" && (!ok || u.String() != tt.want):
			t.Errorf("%s under %s: got %v ok=%v, want %s", tt.ref, tt.web, u, ok, tt.want)
		}
	}
}

func TestSeamUploadAPIURL(t *testing.T) {
	tests := []struct {
		web, u string
		want   string // "" means not an upload
	}{
		{"https://h", "https://h/group/sub/proj/uploads/abc/x.png", "https://h/api/v4/projects/group%2Fsub%2Fproj/uploads/abc/x.png"},
		{"https://h", "https://h/group/proj/-/uploads/abc/x.png", "https://h/api/v4/projects/group%2Fproj/uploads/abc/x.png"},
		{"https://h", "https://h/-/project/42/uploads/abc/x.png", "https://h/api/v4/projects/42/uploads/abc/x.png"},
		{"https://h/gitlab", "https://h/gitlab/group/proj/uploads/abc/x.png", "https://h/gitlab/api/v4/projects/group%2Fproj/uploads/abc/x.png"},
		{"https://h", "https://h/group/proj/uploads/abc/a%20b.png?x=1", "https://h/api/v4/projects/group%2Fproj/uploads/abc/a%20b.png"},
		{"https://h", "https://h/group%2Fsub/proj/uploads/abc/x.png", "https://h/api/v4/projects/group%2Fsub%2Fproj/uploads/abc/x.png"},
		{"https://h", "https://h/attachments/x.png", ""},
		{"https://h", "https://h/uploads/abc/x.png", ""},
		{"https://h/gitlab", "https://h/group/proj/uploads/abc/x.png", ""},
	}
	for _, tt := range tests {
		web, _ := url.Parse(tt.web)
		u, _ := url.Parse(tt.u)
		got, ok := uploadAPIURL(web, u)
		if (tt.want == "") == ok || got != tt.want {
			t.Errorf("uploadAPIURL(%s, %s) = %q ok=%v, want %q", tt.web, tt.u, got, ok, tt.want)
		}
	}
}

func TestSeamTokenOnlyOnOrigin(t *testing.T) {
	var offToken, onToken string
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		offToken = r.Header.Get("PRIVATE-TOKEN") + r.Header.Get("Authorization")
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/first":
			http.Redirect(w, r, "/api/v4/second", http.StatusFound)
		case "/api/v4/second":
			onToken = r.Header.Get("PRIVATE-TOKEN")
			http.Redirect(w, r, other.URL+"/blob", http.StatusFound)
		}
	}))
	defer srv.Close()
	f, err := newForge(srv.URL, "secret", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	req, _, err := f.request(context.Background(), http.MethodGet, f.url("/first", nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := f.hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if onToken != "secret" || offToken != "" {
		t.Errorf("same-origin hop token %q, off-origin hop tokens %q", onToken, offToken)
	}
}
