---
name: comment-standard
description: Use when writing or editing a comment in any file in lazyforge, whether Go, YAML (CI), renovate.json, Makefile, shell hooks, or Markdown docs. Read it while implementing, not only at review time, because the rules decide whether a comment gets written at all. Covers why-vs-what, the paraphrase test, line caps, Go doc comments on exported identifiers, interface-method comments, and the mechanical checklist a reviewer runs against a diff.
---

# Comment Standard

This is the single source of truth for comment rules in this repo. `go-development` and any review brief point here and don't restate it, because copies drift.

Apply it while writing code. A comment deleted in review is a comment that shouldn't have been written, and the cheapest place to apply these rules is the first draft.

There are two registers. `## Rules` is for whoever is writing code. `## Review checklist` is a self-contained pass/fail list for whoever is reviewing a diff, and it may run on a small model. Edit both when a rule changes.

## Rules

### The budget

A number survives a cold context better than a principle does, so apply this first:

| Sits on | Norm | Hard cap |
|---|---|---|
| Function or method doc | 2 lines | 3 |
| Type or interface | 0 (exported: 1) | 2 |
| Comment block inside a function body | 1 | 2 |
| Struct field, const, var | 0 | 1 trailing line |
| Package doc (`doc.go` or the package clause) | 1–3 lines | 5 |
| Anything in a `_test.go` file | free | none |

A comment over a `const` or `var` declared inside a function body takes the const/var cap (1 line), not the block cap.

**A comment that pre-empts an objection nobody raised is padding, even when every sentence is true.** Before you write the third line, ask whether a reader will actually wonder this or whether you're arguing with an imagined reviewer. The usual over-long comment is two sentences of mechanism followed by two defending the design.

### The test

Everything below follows from one question. Apply it to every comment:

> **Could a reader who has only the declaration's name, its signature, and the file it sits in have written this sentence themselves, without reading the body?**

If yes, delete it, or for a lint-required doc see "Go doc comments" below. If no, keep it, subject to the caps.

The context set is deliberately generous: **name, signature, receiver, and surrounding file**. `// View renders the model.` fails on a Bubble Tea model's `View() string`, because the reader already knows what `View` does in Bubble Tea. Don't rescue boilerplate on the technicality that the signature didn't literally spell it out.

### Why, and the caller-must-know exception

Comment the *why*: the reasoning, the constraint, why this approach instead of the obvious one. A good name can't carry "why this and not the simpler thing".

"Why" motivates the test, but it doesn't override it. **Non-obvious behavior a caller must know** passes the test and stays, even though it's grammatically a *what* and sits on simple code. That covers a side effect, coalescing, a caller obligation, an ordering requirement, a failure mode, and what a nil or zero value means. Deleting that kind of comment because "it's WHAT, and this code isn't complex" is a recurring failure. Beyond that class, comment the *what* only when the code really is complex (a dense algorithm, a non-obvious transformation).

These are always forbidden:

- restating the code
- narration (`// Step 1:`, `// Now we...`)
- changelog comments (`// Updated to...`, `// Previously...`)
- commented-out code

A `TODO` needs both an issue reference (`#<n>`) and a clause of reasoning: `// TODO(#14): drop once Forgejo 9 is the floor; older versions omit the field.`

**Restating includes paraphrase.** `// Merge merges the change request.` and `// StartRefresh begins a background refresh.` never repeat the exact words, yet they add nothing to the signature. The test catches them either way. A block label at file or function scope (`// Key handling`, `// Fetch PRs` above the fetch) fails for the same reason, because the reader can already see the grouping.

**Grade a multi-sentence comment sentence by sentence.** A doc comment that opens with a restatement often carries a real why sentence later. Delete the restatement and keep the why. The inverse is just as common: don't keep a five-line block because one sentence in it is good. Trim it to that sentence.

### Go doc comments

Lint (revive's `exported` rule and `godot`) requires a doc comment on exported identifiers that starts with the identifier's name and ends with a period. That fixes the **form**. The **content** is still graded by the test, so:

1. **Prefer not exporting.** Almost everything lives under `internal/`. An identifier exported only so that another file in the same package can use it doesn't need to be exported, and unexporting it removes the need for a comment.
2. **When it must be exported, make the doc one line that states the contract**, meaning the part the signature can't carry: the error it returns for a missing capability, what an empty slice versus nil means, whether it's safe to call from a `tea.Cmd` goroutine, whether it blocks. `// ListChangeRequests returns open PRs only; closed and merged are never included.` is a contract. `// ListChangeRequests lists change requests.` is the lint form with nothing in it. If you can find nothing to say, that's evidence the identifier shouldn't be exported.
3. **Unexported identifiers need no doc**, and when they have one it is graded by the test like anything else.
4. **Package docs** start with `Package <name>`. Say what the package owns and what it must not import when that's the point of the boundary, for example `Package domain holds lazyforge's own types; it imports nothing from internal/.`
5. Use Go doc syntax (`[Forge]` links, a blank `//` line between paragraphs) only in package docs and doc comments that are already more than one line. Never add a heading to a function doc.

"Every other exported func in this package has a doc" is not a reason to write a longer one. Lint requires the doc to exist; it doesn't require padding.

### Types, fields, consts

**Consts and vars almost never need a comment.** `maxBulkMerge = 25` says everything there is to say. Comment one only when the value encodes something you can't recover from the code, such as a limit taken from the forge's API or a value a consumer depends on. Use one trailing line at most. If you're commenting several in a block, you're commenting reflexively.

**Struct fields** get one trailing line at most, and usually none. A field paragraph is the most common over-commenting smell, and it applies to why-comments too.

**Types get no comment beyond the lint line** unless the type itself embodies a decision that no single method carries, such as a posture (it's immutable after construction, or it's never shared across goroutines) or an accepted gap marked `ponytail:`. Even then, keep it to two lines. If you're reaching for a third, the content belongs on the function where it's actionable.

### Interface methods

lazyforge has a real multi-implementation interface: `Forge` and its capability interfaces, with one adapter per forge. This is the rule:

- **A comment on an interface method states only the contract every implementation must honor**, in one line: the sentinel error for an unsupported operation, the ordering guarantee, the meaning of a zero result. Callers read the interface, so the contract lives there.
- **Implementation-specific reasoning goes on the implementation**, never on the interface. "Gitea paginates at 50, so this loops" belongs on the gitea adapter's method.
- An interface with one implementation plus test fakes gets no method comments. The reasoning goes on the implementation.

### Tests

Tests are exempt from everything above, so comment freely. Any per-test comment requirement, such as attribution, lives in `go-development`.

### Non-Go files

**YAML (CI workflows), Makefile, shell hooks:** a section label is often the only structure the file has, so keep section labels. Everything else still faces the test. `# run tests` above `run: make test` restates the line. `# fetch-depth: 0 because the version stamp is computed from tags` is exactly what a config comment is for. In the Makefile, don't comment self-describing targets. A `## help` convention, if one is adopted, is the target's help text and not a comment under these rules. In shell, `set -euo pipefail` needs no comment.

**`renovate.json`:** JSON has no comments. Put the why for a package rule in its `description` field, and hold that field to the same test and a one-sentence cap.

**Markdown docs:** these rules govern code comments, not prose. `<!-- -->` comments in docs follow the TODO rule and are otherwise avoided.

**Cross-file claims rot.** A comment asserting a fact about a *different* file ("the CI job runs this with -race", "Renovate pins this elsewhere") can't be checked by any linter and goes stale silently. Put the claim in the file it describes, or make it executable. A comment saying "never remove X" is not evidence that X exists. Measured facts ("benchmarked: 40ms for 200 repos") are the exception, because they record what was observed.

## Go examples

```go
// ❌ Restates the name
// maxBulkMerge is the maximum number of PRs in a bulk merge.
const maxBulkMerge = 25

// ✅ Value traceable to an external constraint, in one trailing line
const pageSize = 50 // the server's max page size; it clamps larger requests without saying so

// ❌ Paraphrase of what every Bubble Tea Update does
// Update handles messages and updates the model.
func (m reposModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { ... }

// ✅ Caller-must-know: grammatically WHAT, but not recoverable from the signature
// Refresh is safe to call while a refresh is in flight; the second call is dropped.
func (c *Cache) Refresh(ctx context.Context) tea.Cmd { ... }

// ✅ Why, inline on the branch it explains
if pr.Author == host.RenovateUser || strings.HasPrefix(pr.SourceBranch, "renovate/") {
	// Branch prefix is the fallback: self-hosted Renovate often runs as a personal account.
	...
}

// ❌ Narration
// Now group the updates by package.
groups := groupByDependency(prs)

// ✅ Lint-required exported doc that carries a contract
// ParseRenovateBody returns ok=false when the PR body has no update table; callers show the plain title.
func ParseRenovateBody(body string) (u Update, ok bool) { ... }

// ❌ Field paragraph, even though it's a why
type detailsModel struct {
	// tab is the index of the active details tab. It's an int rather than
	// a named type because the set of tabs depends on forge capabilities,
	// which are only known after the host is picked...
	tab int
}

// ✅ One trailing line, or nothing
type detailsModel struct {
	tab int // index into the capability-filtered tab list, not a fixed enum
}

// ✅ Interface method comment: contract every adapter honors, one line
type DashboardTicker interface {
	// TickEntry returns ErrStale if the issue body changed since it was read.
	TickEntry(ctx context.Context, repo domain.Repo, issue int, entry string) error
}

// ❌ Adapter-specific detail on the interface; move it to the gitea method
type Forge interface {
	// Gitea returns 409 here when branch protection blocks the merge, so map it.
	Merge(ctx context.Context, cr domain.ChangeRequest, s MergeStrategy) error
}

// ❌ TODO with no issue, or an issue with no reason
// TODO: fix this
// TODO(#31)
```

## Review checklist

This section is self-contained on purpose: a reviewer works from it alone, without reading `## Rules`.

**Grade length first, before reading any sentence.** Count the lines of every comment in the diff and flag each one over its cap, whatever it says:

| Sits on | Cap |
|---|---|
| Function or method doc | 3 lines |
| Type or interface | 2 lines |
| Block inside a function body | 2 lines |
| Struct field, const, var (including inside a function body) | 1 trailing line |
| Interface method | 1 line |
| Package doc | 5 lines |

Over the cap is over the cap, and the fix is "cut to the cap". Grade what's left only after that. Sentence-level grading misses length, because four individually defensible sentences still make a comment too long. A comment at its cap still gets the question of whether the last sentence answers an objection nobody raised.

**Then apply the test:** could a reader who has only the declaration's name, its signature, and the file it sits in have written this sentence without reading the body? If yes, flag it. Apply it to exported and unexported identifiers alike.

Delete on sight:

- Restating trivial code (`// increment` over `i++`)
- Restating or paraphrasing the name or signature (`// Merge merges the change request.`). On an exported identifier, flag it with "state the contract or unexport", not "delete", because lint requires the doc.
- Narration (`// Step 1:`, `// Now we...`)
- Changelog comments (`// Updated to...`, `// Previously...`)
- Commented-out code
- A `TODO` missing an issue reference or a reason
- Block labels in Go code (`// Key handling`, `// Fetch PRs`)
- A comment on a field, const, or var longer than one trailing line
- Implementation-specific detail on an interface method, or any comment on the methods of a single-implementation interface
- A type comment that explains the subsystem instead of a decision the type itself embodies
- In a multi-line function comment, sentences that narrate implementation steps or restate the signature after the real reasoning has been given. Flag those sentences, not the whole comment.
- A comment asserting a fact about a different file. Grep before trusting it. Measured facts are exempt.

Keep, and don't flag:

- Why-comments
- Caller-must-know behavior: side effects, coalescing, caller obligations, ordering, failure modes, nil or zero meaning
- A one-line contract on an exported identifier or on a multi-implementation interface method
- `ponytail:` markers
- Everything in `_test.go` files
- Section labels in YAML, Makefile, and shell files. Flag only comments that restate the adjacent key, value, or command.

Grade multi-sentence comments **sentence by sentence**. Flag the restating sentence and keep the why sentence. Don't spare a long block because one sentence in it is good.
