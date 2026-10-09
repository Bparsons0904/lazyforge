package github_test

import (
	"errors"
	"reflect"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

var issueItem = forge.ItemRef{Repo: cli, Kind: forge.ItemIssue, Number: openIssue}

func TestListLabelsPagesAndKeepsHexColor(t *testing.T) {
	f, s := newForge(t)
	got, err := f.ListLabels(t.Context(), cli)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Label{
		{ID: 2341282801, Name: "accessibility", Color: "9ce9f4"},
		{ID: 2353619469, Name: "actions", Color: "f9e98b"},
		{ID: 11473294086, Name: "agentic-workflows", Color: "ededed"},
		{ID: 12040824575, Name: "attachments", Color: "b2fa88"},
		{ID: 2211231815, Name: "auth", Color: "00517a"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("labels:\n got %+v\nwant %+v", got, want)
	}
	if n := len(s.requestsTo("GET", "/cli/labels")); n != 3 {
		t.Fatalf("%d label requests, want 3", n)
	}
}

func TestItemLabels(t *testing.T) {
	f, _ := newForge(t)
	got, err := f.ItemLabels(t.Context(), issueItem)
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Label{{ID: 3490675323, Name: "needs-triage", Color: "D6393F"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSetLabelsSendsNames(t *testing.T) {
	f, s := newForge(t)
	got, err := f.SetLabels(t.Context(), issueItem, []int64{2353619469, 2341282801})
	if err != nil {
		t.Fatal(err)
	}
	puts := s.requestsTo("PUT", "/labels")
	if len(puts) != 1 || puts[0].Body != `{"labels":["actions","accessibility"]}` {
		t.Fatalf("PUT requests %+v", puts)
	}
	want := []domain.Label{
		{ID: 2353619469, Name: "actions", Color: "f9e98b"},
		{ID: 2341282801, Name: "accessibility", Color: "9ce9f4"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if after, _ := f.ItemLabels(t.Context(), issueItem); !reflect.DeepEqual(after, want) {
		t.Fatalf("item labels after set %+v", after)
	}
}

func TestSetLabelsUnknownIDIsAnError(t *testing.T) {
	f, s := newForge(t)
	_, err := f.SetLabels(t.Context(), issueItem, []int64{2353619469, 42})
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	if puts := s.requestsTo("PUT", "/labels"); len(puts) != 0 {
		t.Fatalf("sent %+v despite the unknown ID", puts)
	}
}

func TestSetLabelsEmptyClears(t *testing.T) {
	for _, ids := range [][]int64{nil, {}} {
		f, s := newForge(t)
		got, err := f.SetLabels(t.Context(), issueItem, ids)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("got %+v, want none", got)
		}
		puts := s.requestsTo("PUT", "/labels")
		if len(puts) != 1 || puts[0].Body != `{"labels":[]}` {
			t.Fatalf("PUT requests %+v", puts)
		}
	}
}
