package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

func TestLabelPickerSaveCancelAndRetry(t *testing.T) {
	for _, issue := range []bool{false, true} {
		t.Run(map[bool]string{false: "PR", true: "issue"}[issue], func(t *testing.T) {
			f := listFake(domain.AccessWrite, 1)
			f.AddLabels(repoOR, domain.Label{ID: 1, Name: "bug", Color: "ff0000"}, domain.Label{ID: 2, Name: "feature", Color: "00ff00"})
			m := atCRs(t, f, core.Options{})
			if issue {
				m = press(t, m, "2")
			}
			m = press(t, m, "L", "space", "esc")
			if m.labels != nil || len(f.Mutations()) != 0 {
				t.Fatal("cancel wrote labels")
			}
			m = press(t, m, "L", "space")
			if m.labels == nil || !m.labels.selected[1] {
				t.Fatal("label not toggled")
			}
			f.FailNext(forge.ErrUnauthorized)
			m = press(t, m, "enter")
			if m.labels == nil || m.labels.saving || !errors.Is(m.labels.err, forge.ErrUnauthorized) || !m.labels.selected[1] {
				t.Fatal("failed save lost selection")
			}
			m = press(t, m, "enter")
			if m.labels != nil || len(f.Mutations()) != 1 {
				t.Fatal("retry did not save")
			}
			var names []string
			if issue {
				names = m.boxes.issues[0].Labels
			} else {
				names = m.boxes.crs[0].Labels
			}
			if !slices.Equal(names, []string{"bug"}) {
				t.Fatalf("saved labels = %v", names)
			}
			m = press(t, m, "L", "f")
			if got := m.labels.filtered(); len(got) != 1 || got[0].Name != "feature" {
				t.Fatalf("filter = %v", got)
			}
			m = press(t, m, "space", "enter")
			m = press(t, m, "L")
			if !m.labels.selected[1] || !m.labels.selected[2] {
				t.Fatal("filter discarded hidden selection")
			}
			m = press(t, m, "space", "down", "space", "enter")
			if issue {
				names = m.boxes.issues[0].Labels
			} else {
				names = m.boxes.crs[0].Labels
			}
			if len(names) != 0 {
				t.Fatalf("clear all left labels: %v", names)
			}
		})
	}
}

func TestLabelPickerRequiresWriteAccess(t *testing.T) {
	m := atCRs(t, listFake(domain.AccessRead, 1), core.Options{})
	if m.keys.Labels.Enabled() {
		t.Fatal("labels enabled with read access")
	}
	m = press(t, m, "L")
	if m.labels != nil {
		t.Fatal("picker opened with read access")
	}
}

func TestLabelPickerLateLoadAndExistingLabel(t *testing.T) {
	m := atCRs(t, listFake(domain.AccessWrite, 1), core.Options{})
	m, msgs := step(t, m, "L")
	old := m.labels
	m = press(t, m, "esc", "L")
	for _, msg := range msgs {
		m, _ = update(m, msg)
	}
	if m.labels == old {
		t.Fatal("late load replaced new picker")
	}
	m.labels.load(labelsLoadedMsg{selected: []domain.Label{{ID: 9, Name: "organization label"}}})
	if !m.labels.selected[9] || len(m.labels.labels) != 1 {
		t.Fatal("existing label absent from choices was lost")
	}
	if !strings.Contains(strip(m.labels.view(50, 15)), "organization label") {
		t.Fatal("existing label not shown")
	}
}
