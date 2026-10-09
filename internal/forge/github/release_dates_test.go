package github_test

import (
	"net/http"
	"testing"
	"time"
)

func TestReleasePublishedAtFallsBackToCreatedAt(t *testing.T) {
	f := stub(t, "GET /api/v3/repos/cli/cli/releases", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, []obj{
			{"tag_name": "draft", "published_at": nil, "created_at": "2026-08-01T10:00:00Z"},
			{"tag_name": "v1", "published_at": "2026-09-12T19:15:59Z", "created_at": "2026-09-01T00:00:00Z"},
			{"tag_name": "bare"},
		})
	})
	rels, err := f.ListReleases(t.Context(), cli)
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
