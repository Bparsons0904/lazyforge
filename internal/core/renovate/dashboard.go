package renovate

import (
	"regexp"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

var (
	entryRe   = regexp.MustCompile(`^\s*- \[( |x|X)\] (.*)$`)
	branchRe  = regexp.MustCompile(`<!--\s*[a-z]+-branch=(\S+?)\s*-->`)
	commentRe = regexp.MustCompile(`<!--.*?-->`)
	linkRe    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// Entry is one checkbox line of a Dependency Dashboard.
type Entry struct {
	Section, Title, Branch string
	Checked                bool
}

// IsDashboard reports whether is is a Renovate Dependency Dashboard: titled so, or opened by user with Renovate's intro line.
func IsDashboard(is domain.Issue, user string) bool {
	if strings.EqualFold(strings.TrimSpace(is.Title), "Dependency Dashboard") {
		return true
	}
	return user != "" && strings.EqualFold(is.Author, user) && strings.Contains(is.Body, "This issue lists Renovate updates")
}

// ParseDashboard returns the checkbox entries above the Detected Dependencies section.
func ParseDashboard(body string) []Entry {
	var out []Entry
	section := ""
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			if strings.TrimSpace(h) == "Detected Dependencies" {
				break
			}
			section = strings.TrimSpace(h)
			continue
		}
		m := entryRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		e := Entry{Section: section, Checked: m[1] != " "}
		if b := branchRe.FindStringSubmatch(m[2]); b != nil {
			e.Branch = b[1]
		}
		e.Title = strings.TrimSpace(linkRe.ReplaceAllString(commentRe.ReplaceAllString(m[2], ""), "$1"))
		out = append(out, e)
	}
	return out
}
