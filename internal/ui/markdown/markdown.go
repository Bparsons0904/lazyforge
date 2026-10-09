// Package markdown renders a forge markdown body as styled, width-fitted terminal text.
package markdown

import (
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	east "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

var (
	md        = parser.New(parser.WithExtensions(extension.GFMParser))
	summaryRE = regexp.MustCompile(`(?is)<summary[^>]*>(.*?)</summary>`)
	tagRE     = regexp.MustCompile(`<[^>]*>`)
	hiddenRE  = regexp.MustCompile(`(?is)<!--.*?-->|<(script|style)[^>]*>.*?</(script|style)>`)
)

// Render returns body as styled lines no wider than width, joined by newlines.
// Raw HTML is dropped except <br> and <summary> text; only http, https and mailto links become hyperlinks.
func Render(body string, width int) string {
	width = max(width, 1)
	src := []byte(clean(body))
	doc := md.Parse(src)
	r := renderer{src: src}
	lines := r.blocks(doc, width)
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	clamp := lipgloss.NewStyle().MaxWidth(width)
	for i, l := range lines {
		if lipgloss.Width(l) > width {
			lines[i] = clamp.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

type renderer struct{ src []byte }

func (r renderer) blocks(n ast.Node, w int) []string { return r.join(n, w, true) }

func (r renderer) join(n ast.Node, w int, blank bool) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		ls := r.block(c, w)
		if len(ls) == 0 {
			continue
		}
		if len(out) > 0 && blank {
			out = append(out, "")
		}
		out = append(out, ls...)
	}
	return out
}

func (r renderer) block(n ast.Node, w int) []string {
	switch n := n.(type) {
	case *ast.Heading:
		return wrap(r.inline(n, style.Heading), w)
	case *ast.Paragraph:
		return wrap(r.inline(n, style.Text), w)
	case *ast.List:
		return r.list(n, w)
	case *ast.Blockquote:
		return prefixed(r.blocks(n, max(w-2, 1)), style.Faint.Render("│ "), style.Faint.Render("│"))
	case *ast.ThematicBreak:
		return []string{style.Faint.Render(strings.Repeat("─", w))}
	case *ast.CodeBlock:
		return r.code(n, w)
	case *east.Table:
		return r.table(n, w)
	case *ast.HTMLBlock:
		return r.summaries(n, w)
	default:
		return nil
	}
}

func (r renderer) list(l *ast.List, w int) []string {
	var out []string
	num := l.Start
	for it := l.FirstChild(); it != nil; it = it.NextSibling() {
		marker := "• "
		if l.IsOrdered() {
			marker = strconv.Itoa(num) + ". "
			num++
		}
		pad := strings.Repeat(" ", lipgloss.Width(marker))
		if status, ok := extension.TaskStatusOf(it); ok {
			marker = "☐ "
			if status == extension.TaskStatusCompleted {
				marker = "☑ "
			}
			pad = "  "
		}
		loose := !l.IsTight
		sub := r.join(it, max(w-len(pad), 1), loose)
		if len(sub) == 0 {
			sub = []string{""}
		}
		styled := style.Faint.Render(marker)
		for i, ln := range sub {
			switch {
			case i == 0:
				out = append(out, styled+ln)
			case ln == "":
				out = append(out, "")
			default:
				out = append(out, pad+ln)
			}
		}
		if loose {
			out = append(out, "")
		}
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func (r renderer) code(n *ast.CodeBlock, w int) []string {
	var out []string
	for _, seg := range n.Value.Segments() {
		line := strings.TrimRight(string(seg.Bytes(r.src)), "\r\n")
		line = strings.ReplaceAll(line, "\t", "    ")
		for _, piece := range chunks(line, max(w-2, 1)) {
			out = append(out, style.Code.Render("  "+piece))
		}
	}
	return out
}

// summaries renders an HTML block as its <summary> headings followed by its remaining text; tags,
// comments and script or style contents are dropped.
func (r renderer) summaries(n *ast.HTMLBlock, w int) []string {
	src := hiddenRE.ReplaceAllString(n.Value.Str(r.src), "")
	var out []string
	for _, m := range summaryRE.FindAllStringSubmatch(src, -1) {
		text := decode([]byte(tagRE.ReplaceAllString(m[1], "")))
		out = append(out, wrap(words(text, style.Heading), w)...)
	}
	rest := decode([]byte(tagRE.ReplaceAllString(summaryRE.ReplaceAllString(src, ""), "")))
	if strings.TrimSpace(rest) != "" {
		out = append(out, wrap(words(rest, style.Text), w)...)
	}
	return out
}

// prefixed puts prefix before every line and blank before empty ones, so a blank line leaves no trailing space.
func prefixed(lines []string, prefix, blank string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if l == "" {
			out[i] = blank
			continue
		}
		out[i] = prefix + l
	}
	return out
}
