// Package renovate holds pure Renovate logic: detection, PR-body and dashboard parsing, grouping and scoring; it imports only domain.
package renovate

import (
	"regexp"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
)

const intro = "This PR contains the following updates:"

var (
	pkgLinkRe = regexp.MustCompile(`^\[([^\]]+)\]\(([^)]*)\)(?: \(\[[^\]]*\]\(([^)]*)\)\))?$`)
	changeRe  = regexp.MustCompile("^`([^`]+)` → `([^`]+)`$")
	sepCellRe = regexp.MustCompile(`^:?-+:?$`)

	// Renovate's documented prBodyColumns; the unlisted ones carry nothing we use but must not reject the body.
	knownHeaders = map[string]bool{
		"Package": true, "Update": true, "Change": true, "Type": true,
		"Pending": true, "References": true, "File": true, "Age": true,
		"Adoption": true, "Passing": true, "Confidence": true,
	}

	depTypeEcosystem = map[string]string{
		"action": "github-actions", "uses-with": "github-actions",
		"final": "docker", "stage": "docker",
		"require": "go", "indirect": "go", "toolchain": "go",
		"dependencies": "npm", "devDependencies": "npm", "peerDependencies": "npm", "optionalDependencies": "npm",
	}

	titleEcosystem = []struct{ noun, ecosystem string }{
		{"docker tag", "docker"},
		{"docker digest", "docker"},
		{"docker image", "docker"},
		{" action ", "github-actions"},
		{" module ", "go"},
		{"helm release", "helm"},
	}
)

// IsRenovate reports whether cr is a Renovate PR: authored by user (case-insensitive, when set) or on a renovate/ branch.
func IsRenovate(cr domain.ChangeRequest, user string) bool {
	return (user != "" && strings.EqualFold(cr.Author, user)) || strings.HasPrefix(cr.SourceBranch, "renovate/")
}

// Parse reads the update table of a Renovate PR body; ok is false, with nil updates, when any part of it isn't understood.
func Parse(title, body string) (updates []domain.RenovateUpdate, ok bool) {
	rows, header, ok := tableAfterIntro(body)
	if !ok {
		return nil, false
	}
	col := map[string]int{}
	for i, h := range header {
		if !knownHeaders[h] {
			return nil, false
		}
		if _, dup := col[h]; dup {
			return nil, false
		}
		col[h] = i
	}
	for _, req := range []string{"Package", "Update", "Change"} {
		if _, has := col[req]; !has {
			return nil, false
		}
	}
	for _, cells := range rows {
		if len(cells) != len(header) {
			return nil, false
		}
		m := changeRe.FindStringSubmatch(cells[col["Change"]])
		name, homepage, source, pkgOK := parsePackage(cells[col["Package"]])
		if m == nil || !pkgOK {
			return nil, false
		}
		u := domain.RenovateUpdate{
			Package: name, UpdateType: strings.ToLower(cells[col["Update"]]), From: m[1], To: m[2],
			SourceURL: cmpOr(source, homepage),
		}
		if i, has := col["Type"]; has {
			u.DepType = cells[i]
		}
		u.Ecosystem = depTypeEcosystem[u.DepType]
		updates = append(updates, u)
	}
	if len(updates) == 1 && updates[0].Ecosystem == "" {
		updates[0].Ecosystem = ecosystemFromTitle(title)
	}
	return updates, true
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// tableAfterIntro returns the data rows and header of the first table after the intro line.
func tableAfterIntro(body string) (rows [][]string, header []string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) != intro {
		i++
	}
	i++
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	var table [][]string
	for ; i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "|"); i++ {
		table = append(table, splitRow(lines[i]))
	}
	if len(table) < 3 {
		return nil, nil, false
	}
	for _, c := range table[1] {
		if !sepCellRe.MatchString(c) {
			return nil, nil, false
		}
	}
	return table[2:], table[0], true
}

func splitRow(line string) []string {
	cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

func parsePackage(cell string) (name, homepage, source string, ok bool) {
	if m := pkgLinkRe.FindStringSubmatch(cell); m != nil {
		return m[1], m[2], m[3], true
	}
	name = strings.Trim(cell, "` ")
	return name, "", "", name != ""
}

func ecosystemFromTitle(title string) string {
	t := " " + strings.ToLower(title) + " "
	for _, e := range titleEcosystem {
		if strings.Contains(t, e.noun) {
			return e.ecosystem
		}
	}
	return ""
}
