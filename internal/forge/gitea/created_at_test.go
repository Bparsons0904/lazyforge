package gitea_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// The expected instants are parsed straight from the recorded JSON, not from the adapter.
func TestCreatedAtMapsFromPullsFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/pulls.json")
	if err != nil {
		t.Fatal(err)
	}
	var pulls []struct {
		Number    int       `json:"number"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(raw, &pulls); err != nil {
		t.Fatal(err)
	}
	if len(pulls) < 2 || pulls[0].CreatedAt.Equal(pulls[0].UpdatedAt) {
		t.Fatalf("fixture must hold several PRs whose created and updated differ, got %d", len(pulls))
	}
	want := map[int]time.Time{}
	for _, p := range pulls {
		want[p.Number] = p.CreatedAt
	}

	f, _ := newForge(t.Context(), t)
	list, err := f.ListChangeRequests(context.Background(), lazyforge, forge.Filter{State: domain.StateOpen})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("no open PRs listed")
	}
	for _, cr := range list {
		if !cr.CreatedAt.Equal(want[cr.Number]) {
			t.Errorf("list #%d CreatedAt = %v, want %v", cr.Number, cr.CreatedAt, want[cr.Number])
		}
		if cr.CreatedAt.Equal(cr.UpdatedAt) {
			t.Errorf("#%d CreatedAt equals UpdatedAt; the two fields are mixed up", cr.Number)
		}
	}
	for _, n := range []int{18, 16} {
		cr, err := f.GetChangeRequest(context.Background(), lazyforge, n)
		if err != nil {
			t.Fatal(err)
		}
		if !cr.CreatedAt.Equal(want[n]) {
			t.Errorf("get #%d CreatedAt = %v, want %v", n, cr.CreatedAt, want[n])
		}
	}
}
