package gitlab_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

var (
	rtIssue = forge.ItemRef{Repo: rtRepo, Kind: forge.ItemIssue, Number: 3}
	rtMR    = forge.ItemRef{Repo: rtRepo, Kind: forge.ItemChangeRequest, Number: 7}
)

// labelsHandler serves labels.json, and an item's label names on GET and PUT, which echoes what was set.
func labelsHandler(t *testing.T, itemLabels []string) func(http.ResponseWriter, *http.Request, string, string) {
	t.Helper()
	body := rtFixture(t, "labels.json")
	return func(w http.ResponseWriter, r *http.Request, path, reqBody string) {
		switch {
		case path == rtProject+"/labels" && r.Method == http.MethodGet:
			rtPage(t, w, r, body)
		case strings.HasPrefix(path, rtProject+"/issues/3"), strings.HasPrefix(path, rtProject+"/merge_requests/7"):
			names := itemLabels
			if r.Method == http.MethodPut {
				names = putLabelNames(t, r, reqBody)
			}
			out, _ := json.Marshal(map[string]any{"iid": 3, "labels": names})
			_, _ = w.Write(out)
		default:
			rtNotFound(w)
		}
	}
}

// putLabelNames reads the labels value wherever the client chose to put it: query, JSON body or form body.
func putLabelNames(t *testing.T, r *http.Request, body string) []string {
	t.Helper()
	var raw string
	var found bool
	if q := r.URL.Query(); q.Has("labels") {
		raw, found = q.Get("labels"), true
	}
	var js map[string]any
	if json.Unmarshal([]byte(body), &js) == nil {
		if v, ok := js["labels"]; ok {
			found = true
			switch v := v.(type) {
			case string:
				raw = v
			case []any:
				parts := make([]string, len(v))
				for i, e := range v {
					parts[i], _ = e.(string)
				}
				raw = strings.Join(parts, ",")
			}
		}
	} else if form, err := url.ParseQuery(body); err == nil && form.Has("labels") {
		raw, found = form.Get("labels"), true
	}
	if !found {
		t.Errorf("PUT %s carries no labels value, so GitLab would leave the labels as they were", r.RequestURI)
	}
	if raw == "" {
		return []string{}
	}
	return strings.Split(raw, ",")
}

func TestListLabelsMapsIDsAndColours(t *testing.T) {
	f, s := newStub(t, labelsHandler(t, nil))
	got, err := f.ListLabels(t.Context(), rtRepo)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Label{
		{ID: 1, Name: "bug", Color: "d9534f"},
		{ID: 2, Name: "good first issue", Color: "69d100"},
		{ID: 3, Name: "renovate", Color: "0033cc"},
		{ID: 4, Name: "triage", Color: ""},
		{ID: 5, Name: "wontfix", Color: "a8d695"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("labels:\n got %+v\nwant %+v", got, want)
	}
	if n := len(s.requestsTo("GET", rtProject+"/labels")); n != 3 {
		t.Errorf("%d label requests, want 3 pages", n)
	}
}

func TestListLabelsNotFound(t *testing.T) {
	f, _ := newStub(t, func(w http.ResponseWriter, _ *http.Request, _, _ string) { rtNotFound(w) })
	if _, err := f.ListLabels(t.Context(), rtRepo); !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestItemLabelsMapsNamesToIDs(t *testing.T) {
	cases := []struct {
		name string
		item forge.ItemRef
		path string
	}{
		{"issue", rtIssue, "/issues/3"},
		{"merge request", rtMR, "/merge_requests/7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, s := newStub(t, labelsHandler(t, []string{"renovate", "ghost", "bug"}))
			got, err := f.ItemLabels(t.Context(), c.item)
			if err != nil {
				t.Fatal(err)
			}
			want := []domain.Label{
				{ID: 3, Name: "renovate", Color: "0033cc"},
				{ID: 0, Name: "ghost"},
				{ID: 1, Name: "bug", Color: "d9534f"},
			}
			if !slices.Equal(got, want) {
				t.Fatalf("labels:\n got %+v\nwant %+v", got, want)
			}
			if len(s.requestsTo("GET", rtProject+c.path)) != 1 {
				t.Errorf("no GET of %s: %+v", c.path, s.requests())
			}
		})
	}
}

func TestItemLabelsNone(t *testing.T) {
	f, _ := newStub(t, labelsHandler(t, []string{}))
	got, err := f.ItemLabels(t.Context(), rtIssue)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("labels %+v, want none", got)
	}
}

func TestSetLabelsSendsNamesForIDs(t *testing.T) {
	cases := []struct {
		name string
		item forge.ItemRef
		path string
	}{
		{"issue", rtIssue, "/issues/3"},
		{"merge request", rtMR, "/merge_requests/7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, s := newStub(t, labelsHandler(t, nil))
			got, err := f.SetLabels(t.Context(), c.item, []int64{2, 1})
			if err != nil {
				t.Fatal(err)
			}
			puts := s.requestsTo("PUT", rtProject+c.path)
			if len(puts) != 1 {
				t.Fatalf("%d PUTs to %s, want 1: %+v", len(puts), c.path, s.requests())
			}
			var sent []string
			r, _ := http.NewRequestWithContext(t.Context(), http.MethodPut, "http://x"+puts[0].URI, nil)
			sent = putLabelNames(t, r, puts[0].Body)
			slices.Sort(sent)
			if want := []string{"bug", "good first issue"}; !slices.Equal(sent, want) {
				t.Errorf("sent label names %q, want %q", sent, want)
			}
			ids := []int64{}
			for _, l := range got {
				ids = append(ids, l.ID)
			}
			slices.Sort(ids)
			if !slices.Equal(ids, []int64{1, 2}) {
				t.Errorf("returned labels %+v, want the item's labels after the PUT (IDs 1 and 2)", got)
			}
			for _, l := range got {
				if l.Name == "" {
					t.Errorf("returned label %+v has no name", l)
				}
			}
			if len(s.requestsTo("POST", "/labels")) != 0 {
				t.Error("SetLabels created a label")
			}
		})
	}
}

func TestSetLabelsEmptyClears(t *testing.T) {
	for name, ids := range map[string][]int64{"empty": {}, "nil": nil} {
		t.Run(name, func(t *testing.T) {
			f, s := newStub(t, labelsHandler(t, []string{"bug"}))
			got, err := f.SetLabels(t.Context(), rtIssue, ids)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 0 {
				t.Errorf("labels %+v after clearing, want none", got)
			}
			if len(s.requestsTo("PUT", rtProject+"/issues/3")) != 1 {
				t.Fatalf("want one PUT: %+v", s.requests())
			}
		})
	}
}

func TestSetLabelsUnknownIDIsAnErrorAndWritesNothing(t *testing.T) {
	f, s := newStub(t, labelsHandler(t, []string{"bug"}))
	if _, err := f.SetLabels(t.Context(), rtIssue, []int64{1, 999}); err == nil {
		t.Fatal("unknown label ID accepted; GitLab would create a label named for it")
	}
	for _, r := range s.requests() {
		if r.Method != http.MethodGet {
			t.Errorf("request %s %s after an unknown ID, want only reads", r.Method, r.URI)
		}
	}
}

func TestSetLabelsRefusedKeepsError(t *testing.T) {
	f, _ := newStub(t, func(w http.ResponseWriter, r *http.Request, path, _ string) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"403 Forbidden"}`)
			return
		}
		labelsHandler(t, nil)(w, r, path, "")
	})
	if _, err := f.SetLabels(t.Context(), rtIssue, []int64{1}); !errors.Is(err, forge.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}
