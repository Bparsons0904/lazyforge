package markdown

import (
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

var brRE = regexp.MustCompile(`(?i)^<br\s*/?>$`)

type atom struct {
	text  string
	st    lipgloss.Style
	url   string
	space bool // a break opportunity precedes it
	nl    bool // a hard line break; text is empty
}

func (r renderer) inline(n ast.Node, base lipgloss.Style) []atom {
	var out []atom
	pendingSpace := false
	add := func(s string, st lipgloss.Style, url string) {
		for _, f := range splitKeepSpace(s) {
			if f == " " {
				pendingSpace = true
				continue
			}
			out = append(out, atom{text: f, st: st, url: url, space: pendingSpace && len(out) > 0})
			pendingSpace = false
		}
	}
	var walk func(n ast.Node, st lipgloss.Style, url string)
	walk = func(n ast.Node, st lipgloss.Style, url string) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				add(decode(c.Segment.Value(r.src)), st, url)
				if c.HardLineBreak() {
					out = append(out, atom{nl: true})
					pendingSpace = false
				} else if c.SoftLineBreak() {
					pendingSpace = true
				}
			case *ast.String:
				add(decode(c.Value), st, url)
			case *ast.CodeSpan:
				add(plain(r.src, c, false), style.Code, url)
			case *ast.Emphasis:
				es := st.Italic(true)
				if c.Level == 2 {
					es = st.Bold(true)
				}
				walk(c, es, url)
			case *east.Strikethrough:
				walk(c, st.Strikethrough(true), url)
			case *ast.Link:
				walk(c, style.Link, safeURL(string(c.Destination)))
			case *ast.AutoLink:
				add(string(c.Label(r.src)), style.Link, safeURL(string(c.URL(r.src))))
			case *ast.Image:
				u := url
				if u == "" {
					u = safeURL(string(c.Destination))
				}
				add("🖼 "+plain(r.src, c, true), style.Link, u)
			case *east.TaskCheckBox:
				box := "☐"
				if c.IsChecked {
					box = "☑"
				}
				add(box, style.Faint, "")
				pendingSpace = true
			case *ast.RawHTML:
				if brRE.MatchString(strings.TrimSpace(string(c.Segments.Value(r.src)))) {
					out = append(out, atom{nl: true})
					pendingSpace = false
				}
				// Other raw HTML is dropped so hidden markers like <!-- rebase-check --> never show.
			default:
				walk(c, st, url)
			}
		}
	}
	walk(n, base, "")
	return out
}

func words(s string, st lipgloss.Style) []atom {
	var out []atom
	space := false
	for _, f := range splitKeepSpace(s) {
		if f == " " {
			space = true
			continue
		}
		out = append(out, atom{text: f, st: st, space: space && len(out) > 0})
		space = false
	}
	return out
}

// plain skips escape and entity decoding when decoded is false, which code spans need.
func plain(src []byte, n ast.Node, decoded bool) string {
	var b strings.Builder
	read := func(v []byte) {
		if decoded {
			b.WriteString(decode(v))
			return
		}
		b.Write(v)
	}
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *ast.Text:
				read(c.Segment.Value(src))
				if c.SoftLineBreak() || c.HardLineBreak() {
					b.WriteByte(' ')
				}
			case *ast.String:
				read(c.Value)
			case *ast.RawHTML:
			default:
				walk(c)
			}
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}

// decode resolves the backslash escapes and entity references goldmark leaves in text.
func decode(b []byte) string {
	return clean(string(util.ResolveNumericReferences(util.ResolveEntityNames(util.UnescapePunctuations(b)))))
}

// clean drops control characters other than newline and tab, so forge text can't inject terminal sequences.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		bidi := (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) // would reorder the text around them
		if (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x7f && r <= 0x9f) || bidi {
			return -1
		}
		return r
	}, s)
}

// safeURL returns raw when it is an http, https or mailto link, and "" otherwise.
func safeURL(raw string) string {
	raw = clean(raw)
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return raw
	}
	return ""
}

// splitKeepSpace splits s into words and single " " separators; any whitespace run is one separator.
func splitKeepSpace(s string) []string {
	var out []string
	var word strings.Builder
	space := false
	flush := func() {
		if word.Len() > 0 {
			out = append(out, word.String())
			word.Reset()
		}
	}
	for _, r := range s {
		if unicode.IsSpace(r) {
			flush()
			if !space {
				out = append(out, " ")
			}
			space = true
			continue
		}
		space = false
		word.WriteRune(r)
	}
	flush()
	return out
}

// wrap breaks inside a word only when it can't fit alone; atoms with no space between them stay glued.
func wrap(atoms []atom, w int) []string {
	if len(atoms) == 0 {
		return nil
	}
	var lines []string
	var cur strings.Builder
	cw := 0
	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
		cw = 0
	}
	place := func(run []atom) {
		ww := 0
		for _, a := range run {
			ww += lipgloss.Width(a.text)
		}
		gap := 0
		if run[0].space && cw > 0 {
			gap = 1
		}
		if ww <= w {
			if cw > 0 && cw+gap+ww > w {
				flush()
				gap = 0
			}
			if gap == 1 {
				cur.WriteByte(' ')
			}
			for _, a := range run {
				cur.WriteString(render(a.text, a.st, a.url))
			}
			cw += gap + ww
			return
		}
		for _, a := range run {
			sp := a.space
			for _, piece := range chunks(a.text, w) {
				pw := lipgloss.Width(piece)
				gap := 0
				if sp && cw > 0 {
					gap = 1
				}
				if cw > 0 && cw+gap+pw > w {
					flush()
					gap = 0
				}
				if gap == 1 {
					cur.WriteByte(' ')
				}
				cur.WriteString(render(piece, a.st, a.url))
				cw += gap + pw
				sp = false
			}
		}
	}
	var run []atom
	for _, a := range atoms {
		if a.nl {
			if len(run) > 0 {
				place(run)
				run = nil
			}
			flush()
			continue
		}
		if a.space && len(run) > 0 {
			place(run)
			run = nil
		}
		run = append(run, a)
	}
	if len(run) > 0 {
		place(run)
	}
	if cw > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

func chunks(s string, w int) []string {
	if lipgloss.Width(s) <= w {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	cw := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if cw+rw > w && cw > 0 {
			out = append(out, cur.String())
			cur.Reset()
			cw = 0
		}
		cur.WriteRune(r)
		cw += rw
	}
	return append(out, cur.String())
}

// render wraps s in an OSC 8 hyperlink when url is set; url must already have passed safeURL.
func render(s string, st lipgloss.Style, url string) string {
	out := st.Render(s)
	if url == "" {
		return out
	}
	return "\x1b]8;;" + url + "\x1b\\" + out + "\x1b]8;;\x1b\\"
}
