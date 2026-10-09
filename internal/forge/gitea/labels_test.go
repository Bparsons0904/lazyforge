package gitea_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestLabelEndpoints(t *testing.T) {
	var ids []int64
	f := stubForge(t, map[string]http.HandlerFunc{
		"GET /api/v1/repos/{o}/{r}/labels": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Total-Count", "2")
			id := 1
			name := "bug"
			if r.URL.Query().Get("page") == "2" {
				id = 2
				name = "feature"
			}
			writeJSON(w, 200, []obj{{"id": id, "name": name, "color": "ff0000"}})
		},
		"GET /api/v1/repos/{o}/{r}/issues/{n}/labels": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 200, []obj{{"id": 1, "name": "bug", "color": "ff0000"}})
		},
		"PUT /api/v1/repos/{o}/{r}/issues/{n}/labels": func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Labels []int64 `json:"labels"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			ids = body.Labels
			if ids == nil {
				t.Error("labels must be an array, including for clear-all")
			}
			writeJSON(w, 200, []obj{})
		},
	})
	choices, err := f.ListLabels(t.Context(), lazyforge)
	if err != nil || len(choices) != 2 || choices[1].ID != 2 || choices[0].Color != "ff0000" {
		t.Fatalf("choices=%v err=%v", choices, err)
	}
	for _, kind := range []forge.ItemKind{forge.ItemIssue, forge.ItemChangeRequest} {
		item := forge.ItemRef{Repo: lazyforge, Kind: kind, Number: 18}
		selected, err := f.ItemLabels(t.Context(), item)
		if err != nil || !slices.Equal(selected, []domain.Label{{ID: 1, Name: "bug", Color: "ff0000"}}) {
			t.Fatalf("selection=%v err=%v", selected, err)
		}
		if _, err := f.SetLabels(t.Context(), item, []int64{2}); err != nil || !slices.Equal(ids, []int64{2}) {
			t.Fatalf("save=%v err=%v", ids, err)
		}
		if _, err := f.SetLabels(t.Context(), item, nil); err != nil || len(ids) != 0 {
			t.Fatalf("clear=%v err=%v", ids, err)
		}
	}
}

func TestLabelWriteRefusal(t *testing.T) {
	f := stubForge(t, map[string]http.HandlerFunc{
		"PUT /api/v1/repos/{o}/{r}/issues/{n}/labels": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, 403, obj{"message": "write access required"})
		},
	})
	_, err := f.SetLabels(t.Context(), forge.ItemRef{Repo: lazyforge, Number: 18}, []int64{1})
	if !errors.Is(err, forge.ErrUnauthorized) {
		t.Fatalf("write refusal=%v", err)
	}
}
