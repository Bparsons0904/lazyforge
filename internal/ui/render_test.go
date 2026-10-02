package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

func TestAge(t *testing.T) {
	now := time.Now()
	for d, want := range map[time.Duration]string{4 * time.Minute: "4m", time.Hour: "1h", 5 * time.Hour: "5h", 72 * time.Hour: "3d"} {
		if got := age(now, now.Add(-d)); got != want {
			t.Errorf("age(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	for _, tc := range []struct {
		in   string
		w    int
		want string
	}{{"hello", 5, "hello"}, {"hello world", 6, "hello…"}, {"hello", 1, "…"}, {"hello", 0, ""}, {"日本語日本語", 5, "日本…"}} {
		got := truncate(tc.in, tc.w)
		if got != tc.want || lipgloss.Width(got) > tc.w {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
		}
	}
}

func TestRowKeepsMetaAndTruncatesLabel(t *testing.T) {
	got := strip(row("a very long pull request title", "3d", 16, lipgloss.NewStyle()))
	if lipgloss.Width(got) != 16 || !strings.HasSuffix(got, "3d") || !strings.Contains(got, "…") {
		t.Fatalf("row %q", got)
	}
}

func TestCIIcons(t *testing.T) {
	for s, want := range map[domain.CIState]string{
		domain.CIPass: "✓", domain.CIFail: "✗", domain.CIRunning: "●", domain.CIPending: "●",
	} {
		if got := strip(ciIcon(s, lipgloss.NewStyle())); got != want {
			t.Errorf("ciIcon(%v) = %q, want %q", s, got, want)
		}
	}
	if got := strip(ciIcon(domain.CISkipped, lipgloss.NewStyle())); got == "✓" || got == "✗" || got == "●" {
		t.Errorf("other state shows %q", got)
	}
	for s, want := range map[domain.CIState]string{domain.CIPass: "CI passing", domain.CIFail: "CI failing", domain.CIRunning: "CI running", domain.CIPending: "CI pending"} {
		if got := ciText(s); got != want {
			t.Errorf("ciText(%v) = %q, want %q", s, got, want)
		}
	}
}

func TestFrameTitleOnTopBorder(t *testing.T) {
	out := strip(frame("Title", []string{"x"}, 20, 4, true))
	ls := strings.Split(out, "\n")
	if len(ls) != 4 || !strings.Contains(ls[0], "Title") || !strings.HasPrefix(ls[0], "╭") || !strings.HasPrefix(ls[3], "╰") {
		t.Fatalf("frame:\n%s", out)
	}
	for _, l := range ls {
		if lipgloss.Width(l) != 20 {
			t.Errorf("line %q width %d", l, lipgloss.Width(l))
		}
	}
}

func TestRepoRowShowsCountAndAge(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "j")
	var homelabRow string
	for _, l := range lines(m) {
		if strings.Contains(l, "homelab") && strings.Contains(l, "│") {
			homelabRow = l
		}
	}
	if !strings.Contains(homelabRow, "3 PR") || !strings.Contains(homelabRow, "4m") {
		t.Fatalf("homelab row %q", homelabRow)
	}
}

func TestBoxHeightsFollowFocus(t *testing.T) {
	m := sized(t, 120, 40)
	m = press(t, m, "j", "l")
	heights := func() map[string]int {
		out, cur := map[string]int{}, ""
		for _, l := range lines(m)[1 : len(lines(m))-1] {
			if strings.HasPrefix(l, "╭─ [") {
				cur = l[strings.Index(l, "[")+1 : strings.Index(l, "]")]
			}
			out[cur]++
		}
		return out
	}
	h := heights()
	if h["1"] < 2*h["2"] || h["1"] < 2*h["3"] {
		t.Fatalf("focused box not tallest: %v", h)
	}
	m = press(t, m, "2")
	if h = heights(); h["2"] < 2*h["1"] {
		t.Fatalf("focus moved but heights didn't: %v", h)
	}
}

func TestSplitHeightsFocusRatio(t *testing.T) {
	hs := splitHeights(38, 3, 1)
	if hs[1] < 2*hs[0] || hs[1] < 2*hs[2] || hs[0]+hs[1]+hs[2] != 38 {
		t.Fatalf("heights %v: focused box should be about 2.4x each other box", hs)
	}
	if eq := splitHeights(38, 3, -1); slices.Max(eq)-slices.Min(eq) > 1 {
		t.Fatalf("preview heights %v not equal", eq)
	}
}
