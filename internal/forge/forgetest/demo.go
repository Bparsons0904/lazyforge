package forgetest

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

const demoURL = "https://forge.home.arpa"

type demoPR struct {
	n           int
	title, slug string // slug is set for non-Renovate PRs only
	ago         time.Duration
	open        time.Duration // how long the PR has been open; at least ago
	ci          domain.CIState
	pkg         string // non-empty marks a Renovate PR
	from, to    string
	bump        string
	body, label string
}

type demoIssue struct {
	n        int
	title    string
	author   string
	label    string
	body     string
	comments int
	ago      time.Duration
}

type demoJob struct {
	name   string
	status domain.CIState
	log    []string
}

type demoRun struct {
	id              int64
	wf, branch, sha string
	event           string
	status          domain.CIState
	dur, ago        time.Duration
	jobs            []demoJob
}

type demoRelease struct {
	tag, notes string
	ago        time.Duration
}

type demoRepo struct {
	name, desc string
	ago        time.Duration
	prs        []demoPR
	issues     []demoIssue
	runs       []demoRun
	releases   []demoRelease
}

var (
	lintLog = []string{"Run actions/checkout@v4", "Run yamllint .", "✓ 0 problems", "Job succeeded"}
	failLog = []string{
		"Run actions/checkout@v4", "Run docker compose config -q", "Run ./scripts/validate-traefik.sh",
		"ERROR: field not found, node: swarmMode", "  in services/proxy/traefik.yml: providers.docker.swarmMode",
		"hint: traefik v3.2 moved swarm support to providers.swarm", "Process completed with exit code 1",
	}
)

const (
	minute = time.Minute
	hour   = time.Hour
	day    = 24 * time.Hour
)

// what is the title's subject; its noun ("docker tag", "module") is how core infers the ecosystem.
func renovatePR(n int, what, pkg, from, to, bump string, ago, open time.Duration, ci domain.CIState) demoPR {
	return demoPR{
		n: n, title: fmt.Sprintf("chore(deps): update %s to v%s", what, strings.TrimPrefix(to, "v")),
		ago: ago, open: open, ci: ci, pkg: pkg, from: from, to: to, bump: bump, label: "dependencies",
	}
}

func dashboard(n int, ago time.Duration, body string) demoIssue {
	return demoIssue{n: n, title: "Dependency Dashboard", author: "renovate", label: "renovate", body: body, ago: ago}
}

var demoRepos = []demoRepo{
	{
		name: "homelab", desc: "Docker Compose stack for the home server", ago: 4 * minute,
		prs: []demoPR{
			renovatePR(42, "postgres docker tag", "postgres", "16.4", "17.0", "major", 4*minute, 3*day, domain.CIPass),
			renovatePR(41, "traefik docker tag", "traefik", "v3.1.4", "v3.2.0", "minor", 25*minute, 14*day, domain.CIFail),
			{
				n: 39, title: "feat: add uptime-kuma service", slug: "add-uptime-kuma", ago: day, ci: domain.CIPass, label: "enhancement",
				body: "Adds uptime-kuma behind traefik with forward-auth.\n\n- new service in services/monitoring\n- traefik labels for status.home.arpa\n- data volume added to the restic backup set",
			},
		},
		issues: []demoIssue{
			dashboard(12, 4*minute, "## Open\n\n- [x] chore(deps): update postgres to v17.0 (#42)\n- [x] chore(deps): update traefik to v3.2.0 (#41)\n\n## Awaiting schedule\n\n- [ ] chore(deps): update immich to v1.120.0\n- [ ] chore(deps): update node to v22\n\n## Other\n\n- [ ] Check this box to trigger a request for Renovate to run again on this repository\n"),
			{
				n: 30, title: "Paperless backups not rotating", author: "you", label: "bug", comments: 1, ago: 3 * day,
				body: "restic forget isn't pruning paperless snapshots. The last 40 daily snapshots are all still present.",
			},
		},
		runs: []demoRun{
			{
				id: 318, wf: "deploy", branch: "main", sha: "a1c9e2f", event: "push", status: domain.CIPass, dur: 72 * time.Second, ago: 10 * minute,
				jobs: []demoJob{
					{"lint", domain.CIPass, lintLog},
					{"deploy", domain.CIPass, []string{"Run actions/checkout@v4", "Run docker compose pull", " ✓ db Pulled", " ✓ proxy Pulled", "Run docker compose up -d --remove-orphans", " Container homelab-db-1  Started", " Container homelab-proxy-1  Started", "Job succeeded"}},
				},
			},
			{
				id: 317, wf: "validate", branch: "renovate/traefik-3.x", sha: "7be0d14", event: "pull_request", status: domain.CIFail, dur: 34 * time.Second, ago: 25 * minute,
				jobs: []demoJob{{"lint", domain.CIPass, lintLog}, {"validate", domain.CIFail, failLog}},
			},
		},
		releases: []demoRelease{{"2026.09.1", "- traefik 3.1\n- immich added\n- nightly restic check", 5 * day}},
	},
	{
		name: "infra", desc: "OpenTofu for Proxmox VMs and DNS", ago: hour,
		prs: []demoPR{
			renovatePR(18, "opentofu", "opentofu", "1.8.5", "1.9.0", "minor", hour, 40*day, domain.CIPass),
			renovatePR(17, "postgres docker tag", "postgres", "16.4", "17.0", "major", 2*hour, 3*day, domain.CIRunning),
		},
		issues: []demoIssue{
			dashboard(3, hour, "## Open\n\n- [x] chore(deps): update opentofu to v1.9.0 (#18)\n- [x] chore(deps): update postgres to v17.0 (#17)\n\n## Rate-limited\n\n- [ ] chore(deps): update proxmox provider to v0.66\n\n## Other\n\n- [ ] Check this box to trigger a request for Renovate to run again on this repository\n"),
		},
		runs: []demoRun{
			{
				id: 91, wf: "plan", branch: "renovate/postgres-17.x", sha: "c03aa71", event: "pull_request", status: domain.CIRunning, dur: 2 * minute, ago: 2 * hour,
				jobs: []demoJob{{"plan", domain.CIRunning, []string{"Run opentofu/setup-opentofu@v1", "Run tofu init", "Run tofu plan -no-color", "  # proxmox_lxc.db must be replaced", "… streaming"}}},
			},
			{
				id: 90, wf: "plan", branch: "main", sha: "e88d10b", event: "push", status: domain.CIPass, dur: 48 * time.Second, ago: day,
				jobs: []demoJob{{"plan", domain.CIPass, []string{"Run tofu plan -no-color", "No changes. Your infrastructure matches the configuration.", "Job succeeded"}}},
			},
		},
	},
	{
		name: "lazyforge", desc: "TUI for Forgejo + Renovate", ago: 3 * hour,
		prs: []demoPR{
			{
				n: 2, title: "feat: numbered boxes + two-column navigation", slug: "numbered-boxes", ago: 3 * hour, ci: domain.CIPass, label: "enhancement",
				body: "First pass at the layout.\n\n- repo list on the left, its boxes previewed on the right\n- l shifts the boxes left, details on the right\n- 1-5 jump straight to a box",
			},
			renovatePR(3, "module bubbletea", "bubbletea", "v1.2.4", "v1.3.0", "minor", 5*hour, day, domain.CIPass),
		},
		issues: []demoIssue{{
			n: 1, title: "Spike: Forgejo Actions API coverage", author: "you", label: "spike", ago: 2 * day,
			body: "Confirm which endpoints exist on our Forgejo version:\n- list runs per repo\n- job logs\n- rerun workflow / job\n\nIf logs are missing, fall back to opening the run in the browser.",
		}},
		runs: []demoRun{{
			id: 12, wf: "ci", branch: "main", sha: "4f2b9d0", event: "push", status: domain.CIPass, dur: 41 * time.Second, ago: 3 * hour,
			jobs: []demoJob{{"test", domain.CIPass, []string{"Run go test ./...", "ok  lazyforge/internal/ui     0.12s", "ok  lazyforge/internal/forge  0.31s", "Job succeeded"}}},
		}},
		releases: []demoRelease{{"v0.1.0", "- first mockup\n- repo list + Renovate inbox", 2 * day}},
	},
	{name: "dotfiles", desc: "Shell, editor and WM config", ago: 2 * day},
}

type demoBranch struct {
	name string
	ago  time.Duration // the tip's age; each older commit is an hour further back
	log  []string      // newest first
}

// demoBranches are the branches of each demo repo; main is the default branch in every one.
var demoBranches = map[string][]demoBranch{
	"homelab": {
		{"main", 10 * minute, []string{"fix(traefik): drop the swarm provider", "chore: restic keep-daily 40", "docs: note the paperless snapshot policy"}},
		{"renovate/traefik-3.x", 25 * minute, []string{"chore(deps): update traefik docker tag to v3.2.0"}},
		{"feature/add-uptime-kuma", day, []string{"feat: add uptime-kuma service", "feat: forward-auth for the status page"}},
		{"fix/paperless-retention", 3 * day, []string{"fix(backup): prune paperless snapshots", "wip: restic keep-daily 40"}},
	},
	"infra": {
		{"main", hour, []string{"chore(tofu): bump the proxmox provider pin", "docs: explain the DNS zone layout"}},
		{"renovate/postgres-17.x", 2 * hour, []string{"chore(deps): update postgres docker tag to v17.0"}},
		{"renovate/opentofu-1.x", 3 * hour, []string{"chore(deps): update opentofu to v1.9.0"}},
		{"feature/proxmox-dns", 5 * day, []string{"feat(dns): add the lab zone", "feat(dns): split-horizon for home.arpa"}},
	},
	"lazyforge": {
		{"main", 3 * hour, []string{"feat(ui): repo list with boxes", "chore: scaffold the bubbletea model"}},
		{"feature/numbered-boxes", 4 * hour, []string{"feat: numbered boxes + two-column navigation", "docs: sketch the boxes"}},
		{"renovate/bubbletea-1.x", 5 * hour, []string{"chore(deps): update module bubbletea to v1.3.0"}},
		{"docs/keymap", 2 * day, []string{"docs: keymap for the details pane", "docs: design notes"}},
	},
	"dotfiles": {
		{"main", 2 * day, []string{"chore: tmux theme", "chore: zsh aliases"}},
		{"wip/neovim-lsp", 4 * day, []string{"wip: lsp config"}},
		{"fix/zsh-path", 6 * day, []string{"fix(zsh): path order", "chore: move the path into .zshenv"}},
		{"feature/wezterm", 9 * day, []string{"feat: wezterm config"}},
	},
}

// seedBranches adds each branch with its commit list; Renovate branches are authored by renovate.
func seedBranches(f *Fake, ref domain.RepoRef, base string, now time.Time, bs []demoBranch) {
	for _, b := range bs {
		author := "you"
		if strings.HasPrefix(b.name, "renovate/") {
			author = "renovate"
		}
		commits := make([]domain.Commit, len(b.log))
		for i, msg := range b.log {
			commits[i] = domain.Commit{
				SHA: demoSHA(fmt.Appendf(nil, "%s@%s#%d", ref, b.name, i)), Message: msg, Author: author,
				Date: now.Add(-b.ago - time.Duration(i)*hour),
			}
		}
		f.AddBranch(ref, domain.Branch{
			Name: b.name, Default: b.name == "main", Commit: commits[0], WebURL: fmt.Sprintf("%s/src/branch/%s", base, b.name),
		})
		f.SetCommits(ref, b.name, commits)
	}
}

// NewDemo returns a Fake seeded to look like docs/mockup.html, with every timestamp relative to now.
func NewDemo(now time.Time) *Fake {
	f := NewFake(forge.HostInfo{
		Kind: forge.KindForgejo, URL: demoURL, Version: "16.0.5+gitea-1.22.0", User: "you", ChangeRequestTerm: "PR",
	})
	for _, r := range demoRepos {
		ref := domain.RepoRef{Owner: "home", Name: r.name}
		base := fmt.Sprintf("%s/%s/%s", demoURL, ref.Owner, ref.Name)
		f.AddRepo(domain.Repo{RepoRef: ref, Description: r.desc, WebURL: base, LastActivity: now.Add(-r.ago), Access: domain.AccessWrite})
		f.AddLabels(ref,
			domain.Label{ID: 1, Name: "bug", Color: "f38ba8"},
			domain.Label{ID: 2, Name: "enhancement", Color: "89b4fa"},
			domain.Label{ID: 3, Name: "dependencies", Color: "cba6f7"},
			domain.Label{ID: 4, Name: "renovate", Color: "a6e3a1"},
			domain.Label{ID: 5, Name: "spike", Color: "fab387"},
		)
		for _, p := range r.prs {
			f.AddChangeRequest(ref, p.toDomain(ref, base, now))
		}
		for _, is := range r.issues {
			f.AddIssue(ref, domain.Issue{
				Number: is.n, Title: is.title, Body: is.body, Author: is.author, Labels: []string{is.label},
				Comments: is.comments, UpdatedAt: now.Add(-is.ago), WebURL: fmt.Sprintf("%s/issues/%d", base, is.n),
			})
		}
		for _, run := range r.runs {
			seedRun(f, ref, base, now, run)
		}
		for _, rel := range r.releases {
			f.AddRelease(ref, domain.Release{
				Tag: rel.tag, Name: rel.tag, Notes: rel.notes, PublishedAt: now.Add(-rel.ago),
				WebURL: fmt.Sprintf("%s/releases/tag/%s", base, rel.tag),
			})
		}
		seedBranches(f, ref, base, now, demoBranches[r.name])
		if r.name != "dotfiles" {
			f.SetReadme(ref, domain.Readme{Name: "README.md", Body: demoReadme(r.name)})
		}
	}
	return f
}

func (p demoPR) toDomain(ref domain.RepoRef, base string, now time.Time) domain.ChangeRequest {
	cr := domain.ChangeRequest{
		Number: p.n, Title: p.title, Body: p.body, Author: "you", SourceBranch: "feature/" + p.slug, TargetBranch: "main",
		HeadSHA: demoSHA(fmt.Appendf(nil, "%s#%d", ref, p.n)), CI: p.ci, Labels: []string{p.label},
		UpdatedAt: now.Add(-p.ago), CreatedAt: now.Add(-max(p.open, p.ago)), WebURL: fmt.Sprintf("%s/pulls/%d", base, p.n),
	}
	if p.pkg != "" {
		major, _, _ := strings.Cut(strings.TrimPrefix(p.to, "v"), ".")
		cr.Author = "renovate"
		cr.SourceBranch = fmt.Sprintf("renovate/%s-%s.x", p.pkg, major)
		cr.Body = fmt.Sprintf("This PR contains the following updates:\n\n| Package | Update | Change |\n|---|---|---|\n| [%s](https://hub.docker.com/_/%[1]s) | %s | `%s` → `%s` |\n\n---\n", p.pkg, p.bump, p.from, p.to)
	}
	return cr
}

func seedRun(f *Fake, ref domain.RepoRef, base string, now time.Time, r demoRun) {
	run := domain.Run{
		ID: r.id, Number: int(r.id), Workflow: r.wf, Title: r.wf, Branch: r.branch, Commit: r.sha, Event: r.event,
		Status: r.status, StartedAt: now.Add(-r.ago), Duration: r.dur, WebURL: fmt.Sprintf("%s/actions/runs/%d", base, r.id),
	}
	jobs := make([]domain.Job, len(r.jobs))
	logs := map[int64]string{}
	for i, j := range r.jobs {
		id := r.id*10 + int64(i)
		jobs[i] = domain.Job{ID: id, RunID: r.id, Name: j.name, Status: j.status, Attempt: 1}
		logs[id] = strings.Join(j.log, "\n") + "\n"
	}
	f.AddRun(ref, run, jobs, logs)
}

// demoReadme is a short README; the dotfiles demo repo has none, which shows the "No README" state.
func demoReadme(name string) string {
	return fmt.Sprintf("# %s\n\nNotes for the %s repo.\n\n## Setup\n\n- clone the repo\n- run `make check`\n\nSee [the design notes](https://example.com/design).\n", name, name)
}

func demoSHA(seed []byte) string { return fmt.Sprintf("%x", sha256.Sum256(seed))[:40] }
