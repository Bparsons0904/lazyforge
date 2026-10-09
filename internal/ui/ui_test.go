package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestSplitHeights(t *testing.T) {
	for _, tc := range []struct{ total, n, focus int }{{22, 3, 0}, {22, 3, -1}, {22, 2, 1}, {10, 3, 2}, {3, 3, 0}, {0, 2, 0}} {
		hs := splitHeights(tc.total, tc.n, tc.focus)
		sum := 0
		for _, h := range hs {
			sum += h
		}
		if tc.total > 0 && sum != tc.total {
			t.Errorf("%v: heights %v sum to %d", tc, hs, sum)
		}
		if tc.total >= minBoxHeight*tc.n && slices.Min(hs) < minBoxHeight {
			t.Errorf("%v: heights %v below minimum", tc, hs)
		}
		if tc.focus >= 0 && tc.total >= 12 && hs[tc.focus] != slices.Max(hs) {
			t.Errorf("%v: focused box isn't tallest: %v", tc, hs)
		}
	}
}

func TestViewFitsWindow(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {20, 5}} {
		m := seeded(t)
		m = run(t, m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = boot(t, m)
		check := func(name string) {
			lines := strings.Split(strip(m.View().Content), "\n")
			if len(lines) != size[1] {
				t.Errorf("%v %s: %d lines", size, name, len(lines))
			}
			for _, l := range lines {
				if lipgloss.Width(l) > size[0] {
					t.Errorf("%v %s: line too wide: %q", size, name, l)
				}
			}
		}
		check("repos")
		m = press(t, m, "j")
		check("preview")
		m = press(t, m, "l")
		check("boxes")
		m = press(t, m, "l")
		check("details")
		m = press(t, m, "?")
		check("help")
	}
}

func TestBreadcrumbElides(t *testing.T) {
	m := seeded(t)
	m = run(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = boot(t, m)
	m = press(t, m, "j", "l")
	if got := strip(m.breadcrumb(80)); got != "forge.home.arpa › home/homelab › [1] Pull requests › #42" {
		t.Errorf("breadcrumb %q", got)
	}
	if got := strip(m.breadcrumb(30)); got != "forge.home.arpa › … › #42" {
		t.Errorf("narrow breadcrumb %q", got)
	}
}

func TestCtrlCQuitsWithHelpOpen(t *testing.T) {
	m := boot(t, seeded(t))
	m = run(t, m, tea.KeyPressMsg{Code: '?', Text: "?"})
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c with help open returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c with help open did not quit")
	}
}

func TestRowSanitizesTabs(t *testing.T) {
	got := row("a\ttitle\twith tabs", "1d", 20, lipgloss.NewStyle())
	if w := lipgloss.Width(strip(got)); w != 20 || strings.ContainsRune(got, '\t') {
		t.Fatalf("row width %d, tab kept: %q", w, got)
	}
}
