package ui

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/markdown"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/ui/style"
)

type details struct {
	vp     viewport.Model
	tab    int
	shown  string // the item and tab on screen; a change scrolls back to the top
	md     mdMemo
	img    *imageSet // nil while images are off
	imgGen int       // bumped whenever rendered image output can change; part of the memo key
	want   []imageRef
	// branchCur is the Branches tab's cursor; it is clamped to the list wherever the list changes.
	branchCur  int
	branchHL   bool   // highlight the cursor; only at the details level
	filesRef   string // the branch Files browses; "" is the default branch
	filesDir   string
	filesCur   int
	filesOff   int
	filesFocus bool
}

// branchesTab is the Branches tab's index in tabs for a Repo.
const branchesTab = 2

type imageRef struct {
	repo domain.RepoRef
	raw  string
}

// Only one item is on screen, so one memo entry is enough.
type mdMemo struct {
	repo  domain.RepoRef
	body  string
	width int
	gen   int
	out   string
	imgs  []string // markdown.Images(body); nil while images are off
	ok    bool
}

// markdown renders body for repo, splicing ready images, and appends body's block images to d.want while images are on.
func (d *details) markdown(repo domain.RepoRef, body string, width int) string {
	if !d.md.ok || d.md.repo != repo || d.md.body != body || d.md.width != width || d.md.gen != d.imgGen {
		d.md = mdMemo{repo: repo, body: body, width: width, gen: d.imgGen, ok: true}
		if d.img != nil {
			d.md.imgs = markdown.Images(body)
			d.md.out = markdown.RenderWithImages(body, width, d.img.blocks(repo, d.md.imgs))
		} else {
			d.md.out = markdown.Render(body, width)
		}
	}
	if d.img != nil {
		for _, raw := range d.md.imgs {
			d.want = append(d.want, imageRef{repo: repo, raw: raw})
		}
	}
	return d.md.out
}

// tabs lists the details tabs for item; a Repo has README, Files and Branches.
func tabs(item any) []string {
	if _, ok := item.(domain.Repo); ok {
		return []string{"README", "Files", "Branches"}
	}
	return []string{"Overview"}
}

// cycleTab moves delta tabs along, wrapping at either end.
func (d *details) cycleTab(item any, delta int) {
	n := len(tabs(item))
	d.tab = ((d.tab+delta)%n + n) % n
}

// contentWidth subtracts the pane border and its one column of padding per side.
func contentWidth(w int) int { return max(w-4, 1) }

func (d *details) sync(item any, b boxes, highlight bool, w, h int, now time.Time) {
	cw := contentWidth(w)
	n := len(tabs(item))
	// The Repo text depends on d.tab, so clamp before choosing it.
	d.tab = min(d.tab, n-1)
	d.branchHL = highlight
	var text string
	if _, ok := item.(domain.Repo); ok {
		text = d.repoText(b, cw, max(h-2, 0), now)
	} else {
		text = overview(item, b.repo, now, func(r domain.RepoRef, body string) string { return d.markdown(r, body, cw) })
	}
	d.syncText(fmt.Sprintf("%v %s", b.repo, itemID(item)), text, n, w, h)
}

// repoText is the Repo box's text for the active tab; ch is the pane's text height.
func (d *details) repoText(b boxes, cw, ch int, now time.Time) string {
	if d.tab == branchesTab {
		return d.branchesText(b.branches, b.crs, b.loaded[boxCRs], b.showBranches, cw, ch, now)
	}
	if d.tab == filesTab {
		return d.filesText(b, cw, ch)
	}
	switch {
	case !b.readme.ok:
		return style.Faint.Render("Loading…")
	case b.readme.r.Name == "":
		return style.Faint.Render("No README")
	}
	return d.markdown(b.repo, b.readme.r.Body, cw)
}

// branchesText composes the Branches tab: the branch list above the cursor branch's commits, exactly ch lines.
func (d *details) branchesText(bs branchesState, crs []domain.ChangeRequest, crsLoaded, available bool, cw, ch int, now time.Time) string {
	switch {
	case !available:
		return style.Faint.Render("Branches aren't available on this host")
	case !bs.ok:
		return style.Faint.Render("Loading…")
	case len(bs.list) == 0:
		return style.Faint.Render("No branches")
	}
	cur := max(min(d.branchCur, len(bs.list)-1), 0)
	rows := branchRows(ch)
	first := max(cur-rows+1, 0)
	// Names share one column, capped so a long name can't crowd out the subject.
	nameW := 0
	for _, b := range bs.list {
		nameW = max(nameW, lipgloss.Width(b.Name))
	}
	nameW = min(nameW, max(cw/2, 0))
	lines := []string{style.Heading.Render("Branches")}
	for i := first; i < len(bs.list) && i < first+rows; i++ {
		base := lipgloss.NewStyle()
		if d.branchHL && i == cur {
			base = style.Selected
		}
		lines = append(lines, branchRow(bs.list[i], nameW, crs, crsLoaded, now, cw, base))
	}
	name := bs.list[cur].Name
	lines = append(lines, style.Heading.Render("Commits on "+name))
	cs, ok := bs.commits[name]
	switch {
	case !ok:
		lines = append(lines, style.Faint.Render("Loading…"))
	case len(cs) == 0:
		lines = append(lines, style.Faint.Render("— none —"))
	default:
		for _, c := range cs[:min(len(cs), max(ch-len(lines), 0))] {
			lines = append(lines, commitRow(c, now, cw))
		}
	}
	return fitLines(lines, cw, ch)
}

// branchRows is how many branch rows fit above the commits in a pane ch lines tall.
func branchRows(ch int) int { return max(1, (ch+1)/2-1) }

// branchRow is one branch: name, tip subject, then the default and open-PR markers and the tip's age.
func branchRow(b domain.Branch, nameW int, crs []domain.ChangeRequest, crsLoaded bool, now time.Time, w int, base lipgloss.Style) string {
	var marks []string
	if b.Default {
		marks = append(marks, "default")
	}
	if i := slices.IndexFunc(crs, func(c domain.ChangeRequest) bool { return c.SourceBranch == b.Name }); crsLoaded && i >= 0 {
		marks = append(marks, fmt.Sprintf("#%d", crs[i].Number))
	}
	marks = append(marks, age(now, b.Commit.Date))
	meta := style.Faint.Inherit(base).Render(strings.Join(marks, " "))
	tag := fitLine(truncate(b.Name, nameW), nameW) + " "
	return tagRow(tag, style.Text, b.Commit.Message, style.Text, meta, w, base)
}

// commitRow is one commit: short SHA, subject, then author and age.
func commitRow(c domain.Commit, now time.Time, w int) string {
	meta := style.Faint.Render(c.Author + " · " + age(now, c.Date))
	return tagRow(c.SHA[:min(7, len(c.SHA))]+" ", style.Faint, c.Message, style.Text, meta, w, lipgloss.NewStyle())
}

// syncText shows text in the pane; a changed id scrolls back to the top. tabCount bounds d.tab.
func (d *details) syncText(id, text string, tabCount, w, h int) {
	d.tab = min(d.tab, tabCount-1)
	cw, ch := contentWidth(w), max(h-2, 0)
	d.vp.SetWidth(cw)
	d.vp.SetHeight(ch)
	d.vp.SetContent(lipgloss.NewStyle().Width(cw).Render(text))
	if id = fmt.Sprintf("%s %d", id, d.tab); id != d.shown {
		d.vp.GotoTop()
		d.shown = id
	}
}

func (d details) view(item any, w, h int, active bool) string {
	names := tabs(item)
	parts := make([]string, len(names))
	for i, t := range names {
		if i == d.tab {
			parts[i] = style.CurrentTab.Render(t)
		} else {
			parts[i] = style.Tab.Render(t)
		}
	}
	title := strings.Join(parts, style.Tab.Render(" │ "))
	return frame(title, strings.Split(d.vp.View(), "\n"), w, h, active)
}

func itemID(item any) string {
	switch it := item.(type) {
	case domain.ChangeRequest:
		return fmt.Sprintf("cr %d", it.Number)
	case domain.Issue:
		return fmt.Sprintf("issue %d", it.Number)
	case domain.Run:
		return fmt.Sprintf("run %d", it.ID)
	case domain.Release:
		return "release " + it.Tag
	case domain.Repo:
		return "repo"
	default:
		return ""
	}
}

// itemCrumb is the breadcrumb's last segment for item, or "" when nothing is selected.
func itemCrumb(item any) string {
	switch it := item.(type) {
	case domain.ChangeRequest:
		return fmt.Sprintf("#%d", it.Number)
	case domain.Issue:
		return fmt.Sprintf("#%d", it.Number)
	case domain.Run:
		return fmt.Sprintf("%s #%d", it.Workflow, it.Number)
	case domain.Release:
		return sanitizeLine(it.Tag)
	default:
		return ""
	}
}

func overview(item any, repo domain.RepoRef, now time.Time, md func(domain.RepoRef, string) string) string {
	var lines []string
	switch it := item.(type) {
	case domain.ChangeRequest:
		ci := ciIcon(it.CI, lipgloss.NewStyle()) + " " + style.Text.Render(ciText(it.CI))
		if len(it.Labels) > 0 {
			ci += style.Faint.Render(" · ") + renderLabels(it.Labels, it.LabelColors)
		}
		lines = []string{
			style.Heading.Render(it.Title),
			style.Faint.Render(fmt.Sprintf("%s #%d · %s · %s ago", repo, it.Number, it.Author, age(now, it.UpdatedAt))),
			ci,
			style.Faint.Render(it.SourceBranch + " → " + it.TargetBranch),
			"",
			md(repo, withAttachments(it.Body, it.Attachments)),
		}
	case domain.Issue:
		lines = []string{
			style.Heading.Render(it.Title),
			style.Faint.Render(fmt.Sprintf("%s #%d · %s · %s ago", repo, it.Number, it.Author, age(now, it.UpdatedAt))),
		}
		if len(it.Labels) > 0 {
			lines = append(lines, renderLabels(it.Labels, it.LabelColors))
		}
		lines = append(lines, "", md(repo, withAttachments(it.Body, it.Attachments)))
	case domain.Run:
		commit := it.Commit[:min(7, len(it.Commit))]
		lines = []string{
			ciIcon(it.Status, lipgloss.NewStyle()) + " " + style.Heading.Render(fmt.Sprintf("%s #%d", it.Workflow, it.Number)),
			style.Faint.Render(fmt.Sprintf("%s · %s · %s @ %s", repo, it.Event, it.Branch, commit)),
			style.Faint.Render(fmt.Sprintf("%s ago · took %s", age(now, it.StartedAt), it.Duration.Round(time.Second))),
		}
	case domain.Release:
		heading := it.Name
		if heading == "" {
			heading = it.Tag
		}
		meta := []string{repo.String(), sanitizeLine(it.Tag)}
		if it.Draft {
			meta = append(meta, "draft")
		}
		if it.Prerelease {
			meta = append(meta, "pre-release")
		}
		if e := publishedAge(now, it.PublishedAt); e != "" {
			meta = append(meta, e+" ago")
		}
		body := style.Faint.Render("No release notes")
		if strings.TrimSpace(it.Notes) != "" {
			body = md(repo, it.Notes)
		}
		lines = []string{
			style.Heading.Render(sanitizeLine(heading)),
			style.Faint.Render(strings.Join(meta, " · ")),
			"",
			body,
		}
	default:
		lines = []string{style.Faint.Render("Nothing here.")}
	}
	return strings.Join(lines, "\n")
}

func renderLabels(names []string, colors map[string]string) string {
	labels := make([]string, len(names))
	for i, name := range names {
		labels[i] = style.Label(name, colors[name]).Render(name)
	}
	return strings.Join(labels, " ")
}

// withAttachments appends each attached image the body doesn't already reference as a markdown image,
// so it takes the same fetch, draw and link-fallback path as an image written into the body.
func withAttachments(body string, as []domain.Attachment) string {
	var b strings.Builder
	b.WriteString(body)
	for _, a := range as {
		u, err := url.Parse(a.URL)
		if err != nil || u.Path == "" || !isImageName(a.Name) || strings.Contains(b.String(), u.Path) {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "![%s](%s)", markdownAlt.Replace(a.Name), a.URL)
	}
	return b.String()
}

var markdownAlt = strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`)

// isImageName reports whether name has an extension the image pipeline decodes.
func isImageName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}
