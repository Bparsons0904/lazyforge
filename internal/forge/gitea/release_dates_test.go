package gitea_test

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// serveReleasesOnce answers the first releases page with items and every later page empty.
func serveReleasesOnce(items ...obj) http.HandlerFunc {
	calls := 0
	return func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			writeJSON(w, 200, items)
			return
		}
		writeJSON(w, 200, []obj{})
	}
}

func TestReleasePublishedAtFallsBackToCreatedAt(t *testing.T) {
	f := stubForge(t, map[string]http.HandlerFunc{
		"GET /api/v1/repos/{o}/{r}/releases": serveReleasesOnce(
			obj{"tag_name": "draft", "published_at": nil, "created_at": "2026-08-01T10:00:00Z"},
			obj{"tag_name": "v1", "published_at": "2026-09-12T19:15:59Z", "created_at": "2026-09-01T00:00:00Z"},
			obj{"tag_name": "bare"},
		),
	})
	rels, err := f.ListReleases(context.Background(), lazyforge)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Time{
		"draft": time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
		"v1":    time.Date(2026, 9, 12, 19, 15, 59, 0, time.UTC),
		"bare":  {},
	}
	if len(rels) != len(want) {
		t.Fatalf("got %d releases, want %d: %+v", len(rels), len(want), rels)
	}
	for _, r := range rels {
		if !r.PublishedAt.Equal(want[r.Tag]) {
			t.Errorf("%s: PublishedAt %v, want %v", r.Tag, r.PublishedAt, want[r.Tag])
		}
	}
}
