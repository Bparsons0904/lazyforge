package gitlab

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestSeamLabelColor(t *testing.T) {
	for in, want := range map[string]string{"#428BCA": "428BCA", "#ff0000": "ff0000", "red": "", "": "", "#fff": ""} {
		if got := labelColor(in); got != want {
			t.Errorf("labelColor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSeamSetLabels(t *testing.T) {
	var puts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_, _ = io.WriteString(w, `[{"id":1,"name":"bug","color":"#ff0000"},{"id":2,"name":"ui","color":"blue"}]`)
		case http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			puts = append(puts, string(b))
			_, _ = io.WriteString(w, `{"labels":["bug","ui"]}`)
		}
	}))
	defer srv.Close()
	f, err := newForge(srv.URL, "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	item := forge.ItemRef{Repo: domain.RepoRef{Owner: "g", Name: "r"}, Kind: forge.ItemIssue, Number: 3}
	got, err := f.SetLabels(context.Background(), item, []int64{1, 2})
	if err != nil || len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 || got[1].Color != "" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if len(puts) != 1 || puts[0] != `{"labels":"bug,ui"}` {
		t.Errorf("puts = %v", puts)
	}
	if _, err := f.SetLabels(context.Background(), item, []int64{1, 99}); !errors.Is(err, forge.ErrNotFound) || len(puts) != 1 {
		t.Errorf("an unknown ID must error without a PUT: err %v, puts %v", err, puts)
	}
}
