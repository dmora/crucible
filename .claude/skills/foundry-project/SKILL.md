---
name: crucible-project
description: Manage the Crucible GitHub project board, triage issues, and guide work through the discovery → design → blueprint → implementation lifecycle. Use when creating issues, triaging work, planning iterations, or deciding what phase an issue is in.
---

# Crucible Project Management

## Project Board

**GitHub Project:** `dmora/projects/5` ("crucible project")
**Repo:** `dmora/crucible`

### Board Views

- **Current iteration** — Board layout, filtered by `@current` iteration
- **Next iteration** — Board layout, filtered by `@next` iteration
- **Prioritized backlog** — Board layout
- **Roadmap** — Roadmap layout
- **In review** — Table layout
- **My items** — Table layout

### Status Columns (the board workflow)

```
Backlog → Discovery → Design → Ready → In progress → In review → Done
```

Status IS the phase. When an issue moves from Discovery to Design, change its Status field. No separate Phase field needed.

### Iteration Policy

Iterations are date-labeled but **work-driven, not calendar-driven**. Move items to the current iteration when they're ready to be worked on — don't wait for dates to roll over. Close an iteration when its work is done, not when the calendar says so. AI development moves fast; a week's worth of work can happen in a day.

### Fields and IDs

```
Project ID: PVT_kwHOAALzsc4BRFdl

Status:     PVTSSF_lAHOAALzsc4BRFdlzg_BF2U
  Backlog:     c24bd32f
  Discovery:   7f01b8a7
  Design:      19278600
  Ready:       6dbb5740
  In progress: c6cd96dd
  In review:   7fbc1414
  Done:        e60af12b

Priority:   PVTSSF_lAHOAALzsc4BRFdlzg_BF8A
  P0: 79628723
  P1: 0a877460
  P2: da944a9c

Size:       PVTSSF_lAHOAALzsc4BRFdlzg_BF8E
  XS: 911790be
  S:  b277fb01
  M:  86db8eb3
  L:  853c8207
  XL: 2d0801e2

Iteration:  PVTIF_lAHOAALzsc4BRFdlzg_BF8M
  Iteration 1: 381c7c80  (Mar 7–21)
  Iteration 2: 54cf5c95  (Mar 21–Apr 4)
  Iteration 3: d2c335bc  (Apr 4–18)
  Iteration 4: b6a8f1bb  (Apr 18–May 2)
  Iteration 5: 955c1297  (May 2–16)
```

### Commands

```bash
# Create issue
gh issue create --repo dmora/crucible --title "..." --body "..."

# Add to project and get item ID
ITEM_ID=$(gh project item-add 5 --owner dmora --url <issue_url> --format json | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

# Set fields
gh project item-edit --project-id PVT_kwHOAALzsc4BRFdl --id $ITEM_ID --field-id <field_id> --single-select-option-id <option_id>
gh project item-edit --project-id PVT_kwHOAALzsc4BRFdl --id $ITEM_ID --field-id PVTIF_lAHOAALzsc4BRFdlzg_BF8M --iteration-id <iter_id>

# List project items (save to file for python parsing — piping to python heredocs can break)
gh project item-list 5 --owner dmora --format json --limit 50 > /tmp/proj.json

# Close issue
gh issue close <number> --repo dmora/crucible --comment "..."
```

### Gotchas

- **Status field option IDs are fragile.** If you update the field options via GraphQL `updateProjectV2Field`, ALL option IDs change — even for options you didn't modify. You must re-set the Status on every existing item after updating options. Always verify IDs after field mutations.
- **Piping `gh project item-list` to python via `<<` heredoc can fail** due to shell escaping. Save to a temp file first, then parse.
- **Closed issues keep their project item** but lose board visibility if their Status isn't set. Always set Status=Done when closing.

## Issue Lifecycle

Issues move through phases tracked by the Status column on the board.

### Discovery

**What:** Open-ended exploration. Understand the problem, identify questions, map the design space.

**Entry:** Someone identifies a problem or opportunity.

**Issue format:**
- Title: `"Discovery: <problem statement>"`
- Body: Problem description + open questions. NO implementation details, NO solution proposals, NO file lists.
- Keep it short. Questions, not answers.

**Exit → Design:** The problem is understood. Questions are answered. The solution direction is clear.

**Status:** Discovery

### Design

**What:** Make design decisions. Choose approach, define interfaces, resolve tradeoffs.

**Entry:** Discovery questions are answered. The problem is well-understood.

**Issue format:**
- Title: `"<descriptive title>"` (drop the "Discovery:" prefix)
- Body: Problem + chosen approach + design decisions + tradeoffs considered.
- Include: data flow, key interfaces, what changes and why.
- Do NOT include: line-by-line implementation, test code, exact file diffs.

**Exit → Ready:** Design decisions are made. Architecture is clear. Ready to specify exactly what to build.

**Status:** Design

### Blueprint (Ready)

**What:** Detailed implementation specification. Exact files, schemas, acceptance criteria. This is the work order for stations.

**Entry:** Design is complete and reviewed.

**A blueprint contains:**
- Intent (what and why)
- Scope (files/components in and out)
- Requirements (functional, edge cases)
- Acceptance criteria (observable pass/fail)
- File changes (which files, what changes)
- Verification plan (test commands, lint checks)

**In Crucible:** The supervisor creates a blueprint and sends it through the station pipeline: `draft → inspect → build → verify → ship`

**Exit → Done:** Implementation complete, PR merged.

**Status:** Ready → In progress → In review → Done

### Bug

**What:** Something is broken. Skip discovery/design when the root cause is clear.

**Issue format:**
- Title: `"Bug: <what's wrong>"`
- Body: Problem observed, root cause (if known), reproduction steps.

**Bugs with clear root cause:** Go straight to Ready (P0/P1).
**Bugs needing investigation:** Start as Discovery.

## Triage Rules

### Priority
- **P0:** Blocks usage. Broken functionality, data loss, crashes. Fix this sprint.
- **P1:** Important capability gap. Core pipeline, key features. Plan for next 1-2 sprints.
- **P2:** Nice to have. Discovery, exploration, future design. Backlog or late sprints.

### Size
- **XS:** Config change, one-liner. < 1 hour.
- **S:** Single file, focused change. < half day.
- **M:** Multiple files, moderate complexity. 1-2 days.
- **L:** Cross-cutting, multiple components. 3-5 days.
- **XL:** Epic or large design effort. > 1 week.

### When to create an issue
- Problem identified → Discovery issue (short, questions only)
- Design decision made → Update existing issue with design, or create new one
- Ready to build → Ensure issue has blueprint-level detail, set to Ready
- Bug found → Bug issue, triage immediately

### When NOT to create an issue
- Don't create issues for work already tracked
- Don't create implementation issues — that's what blueprints are for
- Don't split discovery into sub-issues prematurely

## Pipeline (issue to merged PR)

```
Discovery → Design → Ready → In progress → In review → Done
                       ↓
               draft → inspect → build → verify → ship → PR
                 ↑        |
                 └────────┘ (rework)
```

The supervisor owns the right half (draft → ship). The operator owns the left half (discovery → blueprint). The handoff point is when an issue reaches "Ready" with blueprint-level detail.
