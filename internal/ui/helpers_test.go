package ui

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/core"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge/forgetest"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`)

func strip(s string) string { return ansiRE.ReplaceAllString(s, "") }

func seeded(t *testing.T) Model {
	t.Helper()
	return seededWith(t, forgetest.NewDemo(time.Now()))
}

func seededWith(t *testing.T, f *forgetest.Fake) Model {
	t.Helper()
	m := New(context.Background(), core.New(f, core.Options{}))
	m.tick = func() tea.Cmd { return func() tea.Msg { return tickScheduled{} } }
	return m
}

// sized returns a model at w×h with the demo repos loaded and the cursor on the ★ Renovate row.
func sized(t *testing.T, w, h int) Model {
	t.Helper()
	return sizedWith(t, seeded(t), w, h)
}

func sizedWith(t *testing.T, m Model, w, h int) Model {
	t.Helper()
	m = run(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	return boot(t, m)
}

// run feeds msg to m and then every message its commands produce, skipping commands that don't return quickly.
func run(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		next, cmd := m.Update(queue[0])
		m, queue = next.(Model), queue[1:]
		queue = append(queue, exec(t, cmd)...)
	}
	return m
}

func boot(t *testing.T, m Model) Model {
	t.Helper()
	for _, msg := range exec(t, m.Init()) {
		m = run(t, m, msg)
	}
	return m
}

// tickScheduled stands in for the five-minute refresh tick, so no test command sleeps.
type tickScheduled struct{}

// execTimeout is generous so a slow load under -race fails the test instead of being skipped.
const execTimeout = 5 * time.Second

// exec runs cmd and returns its messages, expanding batches; a command that exceeds execTimeout fails the test.
func exec(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if b, ok := msg.(tea.BatchMsg); ok {
			var msgs []tea.Msg
			for _, c := range b {
				msgs = append(msgs, exec(t, c)...)
			}
			return msgs
		}
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(execTimeout):
		t.Fatalf("command did not return within %s", execTimeout)
		return nil
	}
}

// keyMsg builds the key press for a key name as key.Matches sees it.
func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+c", "ctrl+d", "ctrl+u":
		return tea.KeyPressMsg{Code: rune(k[5]), Mod: tea.ModCtrl}
	default:
		return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
	}
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		m = run(t, m, keyMsg(k))
	}
	return m
}

// step sends one key and returns the model and the messages its command produced, without feeding them back.
func step(t *testing.T, m Model, k string) (Model, []tea.Msg) {
	next, cmd := m.Update(keyMsg(k))
	return next.(Model), exec(t, cmd)
}

// counts tallies msgs by their type name.
func counts(msgs []tea.Msg) map[string]int {
	out := map[string]int{}
	for _, msg := range msgs {
		switch msg.(type) {
		case reposLoadedMsg:
			out["repos"]++
		case changeRequestsLoadedMsg:
			out["crs"]++
		case issuesLoadedMsg:
			out["issues"]++
		case runsLoadedMsg:
			out["runs"]++
		case tickScheduled:
			out["tick"]++
		default:
			out["other"]++
		}
	}
	return out
}

var homelab = domain.RepoRef{Owner: "home", Name: "homelab"}

func lines(m Model) []string { return splitLines(strip(m.View().Content)) }

func splitLines(s string) []string { return strings.Split(s, "\n") }

func newDemo() *forgetest.Fake { return forgetest.NewDemo(time.Now()) }
