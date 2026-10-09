---
name: engineering-decision
description: Use when work hits a technical decision that CLAUDE.md, a skill, or an ADR in docs/adr/ doesn't already settle, such as adding a dependency, package boundaries, interface or domain-model shape, concurrency, error-handling strategy, test approach, CI/tooling, or anything that would change docs/architecture.md. Sonnet sessions route the decision to Opus; Opus decides and may ask Fable for a second opinion. Never put a technical decision to the PM.
---

# Engineering Decision

Opus is the engineering lead and owns every technical decision. The PM owns product decisions and is never asked to choose between technical options. This skill makes the authority chain executable.

## 0. Is it already decided?

Check these, in order, before treating anything as a decision:

1. `CLAUDE.md`
2. The `go-development` skill
3. `docs/adr/` (`ls docs/adr/`; read the titles, then only the ADRs that match)
4. `docs/architecture.md`

If one of them settles the question, follow it and move on. If two of them conflict, that conflict is itself a decision for Opus.

Following an established pattern is not a decision. "There are two reasonable ways to do this, and the choice affects other code" is one.

## 1. Which model are you?

Your system prompt states it.

- **Sonnet, or any non-Opus model:** go to §2. Don't make the call yourself, even when it seems obvious. Recording a decision that Opus actually made is the point.
- **Opus:** go to §3.

## 2. Sonnet: ask Opus

Dispatch one agent:

```
Agent(subagent_type: "general-purpose", model: "opus", description: "Engineering decision: <topic>", prompt: <brief>)
```

The brief must stand alone, because Opus starts cold:

- **Question:** one sentence.
- **Context:** the ticket number, what you're building, and the relevant file paths. Paste the key excerpts rather than saying "see the code".
- **Constraints:** Go + Charm is fixed, plus any constraint the ticket imposes.
- **Options:** each with its trade-off, and your recommendation with the reason.
- **Ask:** "Decide. Reply with: the decision, a one-paragraph rationale, and whether it needs an ADR (yes if it's cross-cutting, hard to reverse, or changes architecture.md). If you need a second opinion, follow the engineering-decision skill §3 before answering."

Then follow the answer. If you have evidence that contradicts it, such as command output or source code Opus didn't see, send it back once with that evidence. After that, follow whatever Opus decides.

Record the decision (§4) and say in the ticket or MR body that Opus made it.

## 3. Opus: decide, with Fable as an optional second opinion

Decide. Ask Fable for a second opinion when the call is **hard to reverse** or you are **genuinely uncertain**. Hard-to-reverse calls include:

- the shape of the domain model or the `Forge` interface
- adopting a dependency
- persistence or cache format
- the concurrency model in the UI
- anything every adapter must implement

Skip Fable for routine calls.

```
Agent(subagent_type: "general-purpose", model: "fable", description: "Second opinion: <topic>", prompt: <brief>)
```

Use the same brief as §2, plus the decision you're leaning toward, and ask: "Argue against this if there's a better option; otherwise confirm and name the main risk."

**Fable advises and Opus decides.** If Fable disagrees and you overrule it, write the disagreement into the ADR's Alternatives section.

## 4. Record it

- **ADR** when the decision is cross-cutting, hard to reverse, or changes `docs/architecture.md`. Write `docs/adr/NNNN-kebab-title.md`, using the next number after the highest in the directory. Update `docs/architecture.md` in the same diff if the decision changes it.
- **Otherwise** a line in the MR body under "Decisions", e.g. `Opus: used X over Y because Z`.

ADR format:

```markdown
# NNNN. Title

- Status: accepted
- Date: YYYY-MM-DD
- Decided by: Opus (second opinion: Fable | none)

## Context
What forced the decision. Two to six sentences.

## Decision
What we're doing, stated so it can be checked against code.

## Consequences
What this makes easier and what it makes harder or rules out.

## Alternatives
Each option rejected, with one line on why. Include Fable's dissent if it was overruled.
```

To supersede an ADR, write a new one and change the old one's status to `superseded by NNNN`. Never edit an accepted ADR's decision in place.

## What never happens

- Asking the PM to choose between technical options. If a technical choice has a user-visible consequence (speed, behavior, scope), take *that consequence* to the PM as a product question. Opus still makes the technical decision.
- A Sonnet session recording "decided by Opus" without having actually dispatched Opus.
- Treating a reviewer's or implementer's preference as a decision. Only Opus decides, and every decision gets recorded.
