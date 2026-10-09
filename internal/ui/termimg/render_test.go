package termimg_test

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi/kitty"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/termimg"
)

const windowLines = 10

// scrollMsg moves the rendered window one line down the list.
type scrollMsg struct{}

// imageModel shows a window of windowLines over a list that holds two placeholder images, one per ID.
type imageModel struct {
	offset int
	lines  []string
}

func newImageModel() imageModel {
	var lines []string
	for i := range 3 {
		lines = append(lines, "text "+strconv.Itoa(i))
	}
	lines = append(lines, termimg.Cells(16, 4, 3)...)
	for i := range 6 {
		lines = append(lines, "more text "+strconv.Itoa(i))
	}
	lines = append(lines, termimg.Cells(255, 4, 3)...)
	for i := range 15 {
		lines = append(lines, "tail "+strconv.Itoa(i))
	}
	return imageModel{lines: lines}
}

func (m imageModel) Init() tea.Cmd { return nil }

func (m imageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(scrollMsg); ok {
		m.offset++
	}
	return m, nil
}

func (m imageModel) View() tea.View {
	end := min(m.offset+windowLines, len(m.lines))
	return tea.NewView(strings.Join(m.lines[m.offset:end], "\n"))
}

// syncBuffer lets the test read what the renderer has written while the program still runs.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForOutput blocks until buf holds more than prev bytes, so the next scroll lands in its own frame.
func waitForOutput(t *testing.T, buf *syncBuffer, prev int) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if n := buf.Len(); n > prev {
			return n
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("renderer wrote nothing after %d bytes", prev)
	return prev
}

// fgBefore returns the 38;5;N foreground in effect at byte position pos of out.
func fgBefore(t *testing.T, out string, pos int) int {
	t.Helper()
	const sgr = "38;5;"
	i := strings.LastIndex(out[:pos], sgr)
	if i < 0 {
		t.Fatalf("placeholder at byte %d has no preceding foreground", pos)
	}
	digits := out[i+len(sgr):]
	end := strings.IndexFunc(digits, func(r rune) bool { return r < '0' || r > '9' })
	if end < 0 {
		end = len(digits)
	}
	n, err := strconv.Atoi(digits[:end])
	if err != nil {
		t.Fatalf("foreground at byte %d is not a 256-colour index: %v", i, err)
	}
	return n
}

func TestRendererDrawsImagesThroughScrolling(t *testing.T) {
	var buf syncBuffer
	p := tea.NewProgram(newImageModel(),
		tea.WithOutput(&buf),
		tea.WithInput(nil),
		tea.WithColorProfile(colorprofile.ANSI256),
		tea.WithWindowSize(40, windowLines),
		tea.WithEnvironment([]string{"TERM=xterm-256color"}),
	)
	done := make(chan error, 1)
	go func() {
		_, err := p.Run()
		done <- err
	}()
	// Each scroll waits for the previous frame to reach the output, otherwise the renderer coalesces them and paints only the last.
	seen := waitForOutput(t, &buf, 0)
	for range 10 {
		p.Send(scrollMsg{})
		seen = waitForOutput(t, &buf, seen)
	}
	p.Send(tea.Quit())
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("program did not quit")
	}
	out := buf.String()

	if !strings.Contains(out, "38;5;16") || !strings.Contains(out, "38;5;255") {
		t.Errorf("output lacks the image foregrounds 38;5;16 and 38;5;255")
	}
	if strings.Contains(out, "38;2;") {
		t.Error("output contains a truecolor foreground, want 256-colour only")
	}

	valid := make(map[rune]bool, termimg.MaxSpan)
	for i := range termimg.MaxSpan {
		valid[kitty.Diacritic(i)] = true
	}
	runes := []rune(out)
	for i, r := range runes {
		if r != kitty.Placeholder {
			continue
		}
		if i+2 >= len(runes) || !valid[runes[i+1]] || !valid[runes[i+2]] {
			t.Fatalf("placeholder at rune %d is not followed by two diacritics", i)
		}
	}

	// pairs[id] holds every (row, column) pair written while foreground id was in effect.
	pairs := map[int]map[string]bool{16: {}, 255: {}}
	for r := range 3 {
		for c := range 4 {
			triple := string(kitty.Placeholder) + string(kitty.Diacritic(r)) + string(kitty.Diacritic(c))
			key := strconv.Itoa(r) + "," + strconv.Itoa(c)
			for pos := 0; ; {
				i := strings.Index(out[pos:], triple)
				if i < 0 {
					break
				}
				at := pos + i
				if id := fgBefore(t, out, at); pairs[id] != nil {
					pairs[id][key] = true
				}
				pos = at + len(triple)
			}
		}
	}
	for _, id := range []int{16, 255} {
		if n := len(pairs[id]); n != 12 {
			t.Errorf("image %d drew %d of its 12 (row, column) pairs, want all 12 (%v)", id, n, pairs[id])
		}
	}
}
