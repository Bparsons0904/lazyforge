package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/config"
)

func withSplash(c config.Config) config.Config {
	c.Splash.Show = true
	return c
}

func TestSplashInFrontOfStartScreen(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    Deps
		want appScreen
	}{
		{"fresh", Deps{Fresh: true, Config: config.Defaults()}, screenOnboarding},
		{"picker", Deps{Config: withSplash(twoHosts())}, screenPicker},
		{"session", Deps{Config: withSplash(twoHosts()), Host: "a", Forge: newDemo()}, screenSession},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := testApp(t, tc.d)
			if a.screen != screenSplash || a.splashTo != tc.want {
				t.Fatalf("screen %v behind %v, want splash behind %v", a.screen, a.splashTo, tc.want)
			}
			off := tc.d
			off.Config.Splash.Show = false
			if a, _ := testApp(t, off); a.screen != tc.want {
				t.Fatalf("splash off: screen %v, want %v", a.screen, tc.want)
			}
		})
	}
}

func TestSplashKeySkipsAndIsSwallowed(t *testing.T) {
	a, _ := testApp(t, Deps{Config: withSplash(twoHosts())})
	before := a.picker.cursor
	a = appPress(t, a, "j")
	if a.screen != screenPicker {
		t.Fatalf("screen %v after a key, want picker", a.screen)
	}
	if a.picker.cursor != before {
		t.Fatalf("skip key moved the picker cursor %d -> %d", before, a.picker.cursor)
	}
}

func TestSplashCtrlCQuits(t *testing.T) {
	a, _ := testApp(t, Deps{Config: withSplash(twoHosts())})
	_, cmd := appKey(a, "ctrl+c")
	if cmd == nil {
		t.Fatal("ctrl+c returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c on the splash didn't quit")
	}
}

func TestSplashTimerLeavesOnceOnly(t *testing.T) {
	a, _ := testApp(t, Deps{Config: withSplash(twoHosts())})
	a = appRun(t, a, splashDoneMsg{})
	if a.screen != screenPicker {
		t.Fatalf("screen %v after the timer, want picker", a.screen)
	}

	// A timer firing after the user already moved on must not pull them back to the picker.
	a, _ = testApp(t, Deps{Config: withSplash(twoHosts())})
	a = appPress(t, a, "space", "S")
	if a.screen != screenSettings {
		t.Fatalf("screen %v, want settings", a.screen)
	}
	a = appRun(t, a, splashDoneMsg{})
	if a.screen != screenSettings {
		t.Fatalf("late timer moved the user to %v", a.screen)
	}
	if _, cmd := a.Update(splashFrameMsg{}); cmd != nil {
		t.Fatal("frame tick rescheduled after the splash closed")
	}
}

func TestSplashFramesAdvanceWhileShown(t *testing.T) {
	a, _ := testApp(t, Deps{Config: withSplash(twoHosts())})
	next, cmd := a.Update(splashFrameMsg{})
	if a = next.(App); a.splash.frame != 1 || cmd == nil {
		t.Fatalf("frame %d, rescheduled %v", a.splash.frame, cmd != nil)
	}
}

// TestSessionLoadsBehindSplash drives Init by hand: its frame ticks repeat forever, so appRun can't run them.
func TestSessionLoadsBehindSplash(t *testing.T) {
	a, _ := testApp(t, Deps{Config: withSplash(twoHosts()), Host: "a", Forge: newDemo()})
	queue := appExec(a.Init())
	for len(queue) > 0 {
		msg := queue[0]
		queue = queue[1:]
		if _, ok := msg.(splashFrameMsg); ok {
			continue
		}
		next, cmd := a.Update(msg)
		a = next.(App)
		queue = append(queue, appExec(cmd)...)
	}
	if a.screen != screenSplash {
		t.Fatalf("screen %v, want the splash still up", a.screen)
	}
	if !a.session.repos.loaded {
		t.Fatal("repos didn't load behind the splash")
	}
}

func TestSplashGivesOnboardingTheWidth(t *testing.T) {
	a, _ := testApp(t, Deps{Fresh: true, Config: config.Defaults()})
	a = appPress(t, a, "space")
	if a.screen != screenOnboarding || a.onboard.step != stepWelcome {
		t.Fatalf("screen %v step %v, want onboarding welcome", a.screen, a.onboard.step)
	}
	if got := a.onboard.addr.Width(); got != 100-8 {
		t.Fatalf("onboarding input width %d, want %d", got, 100-8)
	}
}

func TestSplashViewFits(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{100, 30}, {80, 24}, {20, 6}, {1, 1}} {
		for frame := range 3 * strikeEvery {
			s := splash{frame: frame, tagline: taglines[0]}
			out := strings.Split(s.view(sz.w, sz.h), "\n")
			if len(out) != sz.h {
				t.Fatalf("%dx%d frame %d: %d lines", sz.w, sz.h, frame, len(out))
			}
			for i, l := range out {
				if lipgloss.Width(l) != sz.w {
					t.Fatalf("%dx%d frame %d line %d: width %d", sz.w, sz.h, frame, i, lipgloss.Width(l))
				}
			}
		}
	}
	// Before the first WindowSizeMsg the app renders the splash at 0x0.
	if out := (splash{tagline: taglines[0]}).view(0, 0); out != "" {
		t.Errorf("0x0 splash rendered %q", out)
	}
	big := strip((splash{tagline: taglines[0]}).view(80, 24))
	for _, want := range []string{"lazyforge", taglines[0], "press any key", "|____________|"} {
		if !strings.Contains(big, want) {
			t.Errorf("80x24 splash lacks %q:\n%s", want, big)
		}
	}
	small := strip((splash{tagline: taglines[0]}).view(40, 8))
	if !strings.Contains(small, "lazyforge") || strings.Contains(small, "|____________|") {
		t.Errorf("40x8 splash should drop the art and keep the name:\n%s", small)
	}
}

func TestSplashSparksFly(t *testing.T) {
	sparks := func(frame int) int {
		n := 0
		for _, row := range (splash{frame: frame}).sky() {
			for _, c := range row {
				if strings.ContainsRune("*+·.", c.r) && c.r != 0 {
					n++
				}
			}
		}
		return n
	}
	if sparks(0) != 0 {
		t.Fatal("sparks before the first strike")
	}
	if sparks(strikeEvery-1) == 0 {
		t.Fatal("no sparks right after the strike")
	}
}
