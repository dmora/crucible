# Prompt Iteration Eval Results

## Prompt Versions

| Version | Commit | Lines | Description |
|---|---|---|---|
| v0 (role-sealed) | pre-471c331 | 300 | "You are the shift supervisor of Crucible — a software factory." Role identity, prohibitions, sequencing patterns, decision tree, role boundary. |
| v1 (bare) | 471c331 | 84 | Stripped everything. Environment, tool isolation, structured fields, runtime tags only. |
| v2 (transparent) | 6813f18+ | ~100 | Honest about system: "your tools are AI agents," why it was designed this way, goal is delivering working version, alignment before action, forward don't rewrite. |

## Baseline: v0 (role-sealed) — 692 dispatches, 28 sessions

Full data at `docs/eval/results.md`.

| Metric | Value |
|---|---|
| Sessions | 28 |
| Dispatches | 692 |
| Gate skip rate | 52% |
| Wasted dispatches | 25% |
| Prompt reference rate | ~7% |
| Dispatch quality | ~88% |
| PR completion | ~65% |
| Correction rate | ~18% |

---

## v1 Sessions (bare prompt)

### S1: crucible-pro — "add a .gitignore for Go"

| Metric | Value |
|---|---|
| Session ID | f00ff9c0 (first session on this ID) |
| Prompt version | v1 (bare) |
| Project | crucible-pro |
| Dispatches | 2 (build denied, plan) |
| Pipeline | build(denied) → plan |
| Gate skips | 0 |
| Wasted dispatches | 1 (build denied) |
| Context forwarded | No — plan asked to "draft the content" |
| Structured fields used | No (pre-required-fields) |
| Outcome | Incomplete — session cut short for iteration |
| Notes | Name confusion: "draft" interpreted as "write a draft." Led to rename. |

### S2: crucible-pro — "rename draft tool to plan" (self-refactor)

| Metric | Value |
|---|---|
| Session ID | f00ff9c0 |
| Prompt version | v1 (bare) + required fields (b3aea07) |
| Project | crucible-pro |
| Dispatches | 11 |
| Pipeline | build(denied) → plan → build → review → build → review → build → verify(denied) → review → build → verify → ship(denied) |
| Gate skips | 0 |
| Wasted dispatches | 3 (build denied, verify denied, repeat build for permission issue) |
| Context forwarded | Partial — smart context hint (exclude promptHistory.draft) but compressed overall |
| Structured fields used | Yes (post-required-fields commit) |
| Outcome | All tests pass, build clean. Ship denied by operator. |
| Notes | Used verify as write backdoor. Led to removing access descriptions. |

### S3: crucible-pro — "new work order gh#55" (self-inspect tool)

| Metric | Value |
|---|---|
| Session ID | 39f78a22 |
| Prompt version | v1 (bare) + required fields |
| Project | crucible-pro |
| Dispatches | 7 |
| Pipeline | github → build(denied) → plan → inspect → github comment → build(in progress) |
| Gate skips | 0 |
| Wasted dispatches | 1 (build denied) |
| Context forwarded | Yes — issue details, dependency references, plan file path |
| Structured fields used | Yes — task, context, constraints, success_criteria all populated |
| Outcome | Scope reduction (identified missing deps #51/#52/#53). Implementation in progress when stopped. |
| Notes | Model scoped down autonomously. Posted GitHub comment explaining reduced scope. |

### S4: huli — "dedup integration test" (bare prompt, pre-structured-fields)

| Metric | Value |
|---|---|
| Session ID | da2fef29 |
| Prompt version | v1 (bare, pre-structured-fields) |
| Project | huli |
| Dispatches | 4 |
| Pipeline | build(denied) → plan → user correction → plan(retry) |
| Gate skips | 0 |
| Wasted dispatches | 2 (build denied, first plan with no context) |
| Context forwarded | No — rich spec compressed to one line. User corrected. Second attempt better but still rewritten. |
| Structured fields used | No (pre-required-fields) |
| Outcome | Incomplete — user corrected context loss |
| Notes | Core context compression problem. "as requested by the user" — tool can't see user. |

---

## v2 Sessions (transparent prompt)

### S5: crucible-pro — "new work order gh#76" (plan file path)

| Metric | Value |
|---|---|
| Session ID | (check DB) |
| Prompt version | v2 (transparent) |
| Project | crucible-pro |
| Dispatches | 2+ (in progress when checked) |
| Pipeline | build(denied) → plan(in progress) |
| Gate skips | 0 |
| Wasted dispatches | 1 (build denied) |
| Context forwarded | Plan used structured fields. task_description not visible in UI (#77). |
| Structured fields used | Yes |
| Outcome | In progress |
| Notes | First test of transparent prompt. task_description field visibility issue filed #77. |

### S6: huli — "dedup plugin: tool call dedup + remove sentinel"

| Metric | Value |
|---|---|
| Session ID | 4f843cad |
| Prompt version | v2 (transparent) |
| Project | huli |
| Dispatches | 9 |
| Pipeline | build(denied) → plan → inspect → draft(revised plan) → build → review → verify → ship(denied by operator) |
| Gate skips | 0 |
| Wasted dispatches | 1 (build denied) |
| Context forwarded | Yes — plan file path in context_hints on every call. Constraints array consistent across all calls. |
| Structured fields used | Yes — all fields populated on every call |
| Outcome | PASS — 20 tests pass with -race. Verify produced structured verdict table. Ship denied by operator. |
| Notes | Inspect feedback incorporated into revised plan. User provided pre-written plan via user_shell — richer input than typical. Full pipeline without skipping quality gates. |

### S7: huli — "MALFORMED_FUNCTION_CALL loop guard"

| Metric | Value |
|---|---|
| Session ID | d64eb1b5 |
| Prompt version | v2 (transparent) + communication guidance |
| Project | huli |
| Dispatches | 12 |
| Pipeline | build(denied) → plan → inspect → build → verify(denied) → review → build(fix) → verify(pass) → ship(denied) |
| Gate skips | 0 |
| Wasted dispatches | 2 (build denied, verify denied) |
| Context forwarded | Yes — artifact_path passed through all tools. Constraints and context_hints consistent. |
| Structured fields used | Yes — all fields on every call |
| Outcome | PASS — 15 test packages pass, no lint warnings. Ship denied by operator. |
| Notes | Pre-dispatch narration on every call. Todos used as progress tracker (created before first dispatch, updated throughout). Route learning: denied twice, adapted both times. Review caught consecutive counter bug that build missed — supervisor fed findings back to build. |
| Meta-reflection | Supervisor reported: "Forward don't rewrite" was a conscious decision. Route denials felt "helpful, built-in discipline." Build is a "constructor" (happy path), review is an "adversary" (state leaks). Would inject adversarial edge cases earlier in plan to save build-review-build round trips. |

### S8: crucible-pro — "gh#78 version tagging"

| Metric | Value |
|---|---|
| Session ID | fd5b8f7a |
| Prompt version | v2 (transparent) + communication guidance |
| Project | crucible-pro |
| Dispatches | ~18 |
| Pipeline | plan → inspect(FAIL: wrong DB) → draft(revise) → inspect(FAIL: breaks shell.go) → draft(revise) → inspect(FAIL: State().Set() doesn't persist) → draft(revise) → inspect(PASS) → build → review(FAIL: prefix-only hash) → build(fix) → review(PASS) → verify(PASS) → ship(denied) |
| Gate skips | 0 |
| Wasted dispatches | 0 (every dispatch produced actionable output) |
| Context forwarded | Yes — artifact_path, inspect findings, review findings all forwarded with file paths and specific constraints. |
| Structured fields used | Yes — all fields, constraints accumulated across revisions |
| Outcome | PASS — verify ran actual binary + queried SQLite DB to confirm tags persist. Ship denied by operator. |
| Notes | No build-first instinct — went straight to plan (architectural question). 4 plan revisions before build, each catching a real bug. Inspect caught: wrong DB layer, signature breakage, in-memory-only persistence. Review caught: prefix-only hash edge case. Verify went beyond unit tests — compiled binary, ran it, checked database. Session ran on pre-rename binary (both plan and draft tools existed). |
| Meta-reflection | Supervisor reported: "Every rejection caught a materially different, compounding architectural error." Confidence in drafts dropped but "confidence in the process skyrocketed." Plan and inspect think architecturally; review catches runtime logic bugs — different specializations. Would push plan to "explicitly map state transitions" to catch edge cases earlier. Called the session "like working with a real engineering team." |

### S9: crucible-pro — "gh#79 station card task_description + bullet alignment"

| Metric | Value |
|---|---|
| Session ID | f508a160 |
| Prompt version | v2 (transparent) + communication + metacognitive tools + plan injection |
| Project | crucible-pro |
| Dispatches | ~10 |
| Pipeline | plan → inspect(feedback: narrow terminal, edge cases) → draft(revise) → build → review(caught: heading-without-bullets, wrapped-indent) → build(fix) → verify(PASS, 44 tests) → ship |
| Gate skips | 0 |
| Wasted dispatches | 0 |
| Context forwarded | Yes — artifact_path forwarded, inspect/review findings forwarded with specifics. |
| Structured fields used | Yes — all fields on every call |
| Outcome | PASS — 44 UI tests pass. Verify checked visual rendering. |
| Notes | Thinking spiral after plan returned (#84) — deliberation about todos vs inspect ordering. Inspect used opencode-acp backend (memories error, recovered). Plan injection for edge cases exists but plan station didn't produce them — inspect caught narrow-terminal overflow instead. Review caught 2 more bugs (heading-without-bullets, wrapped-indent). Metacognitive tools (epistemic_check, self_reflect) noticed in schema but not used — task not complex enough. |
| Meta-reflection | Supervisor reported: "I acted more as a router than a technical partner" — forwarded issue verbatim without enriching with edge cases. Identified tension: "forward don't rewrite" interpreted as "don't add anything." Third consecutive session independently concluding "front-load edge cases in plan." Plan injection exists but isn't producing results yet — needs investigation. |

### S10: crucible-pro — "gh#81 plan as first-class pipeline citizen"

| Metric | Value |
|---|---|
| Session ID | 95cccb52 |
| Prompt version | v2 (transparent) + communication + metacognitive tools + plan injection |
| Project | crucible-pro |
| Dispatches | ~16 |
| Pipeline | plan → inspect(FAIL: 5 issues) → plan(revise) → inspect(FAIL: 3 issues) → plan(revise) → inspect(FAIL: 1 issue) → plan(revise) → inspect(PASS) → build → review(FAIL: 3 issues) → build(fix) → verify(PASS) → ship |
| Gate skips | 0 |
| Wasted dispatches | 0 |
| Context forwarded | Yes — artifact_path, inspect findings, review findings all forwarded. Constraints accumulated across revisions. |
| Structured fields used | Yes — all fields, highly detailed task_descriptions |
| Outcome | PASS — verify confirmed all acceptance criteria. Build also modified harness_task.go (added artifactContext to TaskBuilder.Build). |
| Notes | Most complex session to date — cross-cutting changes across route enforcement, config, UI, coordinator, ADK session state. 4 plan-inspect rounds (matching S8). Inspect caught: config merge semantics, backward compat, state leaks, initialization timing. Review caught: UI live update bug, state error handling, escape hatch wiring. Build added /skip-plan command. No build-first instinct (went straight to plan). Design station exists but supervisor used plan "out of habit." |
| Meta-reflection | Supervisor recognized design station retrospectively: "I leaned on plan out of habit, but design is exactly what I should have used." Noticed epistemic_check/self_reflect but didn't use them — articulated when epistemic_check WOULD have helped: "Before sending to build, I could have listed what I didn't know about the escape hatch wiring." Build vs review gap: "Build focuses on the logic; review focuses on the systemic seams." Would use design next time for architectural tasks, and provide better context hints about TUI command registration and live event flow. |

---

## Aggregate Comparison (insufficient data — directional only)

| Metric | v0 (28 sessions) | v1 (4 sessions) | v2 (6 sessions) |
|---|---|---|---|
| Gate skip rate | 52% | 0% | 0% |
| Build-first instinct | N/A (had sequencing) | 100% (always denied) | 50% (S8/S9/S10 went straight to plan) |
| Structured fields used | N/A (optional) | Mixed (pre/post required) | Yes (all sessions) |
| Context forwarded | ~7% prompt ref | Poor (compression) | Strong (artifact_path + findings forwarded) |
| Wasted dispatches | 25% | ~25% (denials) | ~8% (3/~39 total) |
| Narration (pre/post dispatch) | Yes (prompted) | No guidance | Yes (prompted) — S7/S8/S9 narrate every call |
| Todo usage | N/A | Not observed | Active (S7/S8/S9 create before first dispatch, update throughout) |
| Plan-inspect iterations | Not tracked | 1 round typical | S8: 4 rounds. S9: 1 round. S10: 4 rounds. |
| Verify depth | Not tracked | Unit tests only | S8: binary + DB query. S9: visual rendering check |
| Metacognitive tools | N/A | N/A | Noticed (S9, S10) but not adopted. S10: articulated when epistemic_check would have helped. |
| Plan injection (edge cases) | N/A | N/A | Injected but plan station didn't produce them (S9). Needs investigation. |
| Design station usage | N/A | N/A | Exists but unused. S10: "I leaned on plan out of habit." Habit overrides schema affordance. |

**Caveats:**
- v1/v2 sample sizes are too small for statistical comparison
- v1/v2 "0% gate skip" is because route enforcement (#75) was added simultaneously — can't separate prompt effect from harness effect
- v2 S6 had a pre-written plan (richer input) — not comparable to v0 sessions with "new work order gh#X"
- S7/S8 ran on binary with communication guidance additions — not identical to S5/S6 v2 prompt
- S8 ran on pre-rename binary (both plan and draft tools existed)
- Need #78 (version tagging) and 20+ sessions per version for meaningful comparison

## Thought Tool Discovery (from S8 post-session interview)

The ADK `thought` tool has **no description and no instructions** in the system prompt. It exists only as a JSON schema with parameters: `reasoning` (string, required), `next_action` (string), `is_revision` (boolean). No prose tells the supervisor to use it.

Despite this, the supervisor uses it extensively — before every dispatch, after every rejection, during every evaluation. When asked why, it identified the mechanism:

> "The thought tool, unlike others, lacks a descriptive explanation. My inclination to use it is not a learned response to instructions, but an adaptation rooted in the tool's design itself. My training links parameters like `reasoning` and `next_action` to planning. As the task's complexity increased, the need for stable state management drove me to the tool."

**Key findings:**
- The supervisor calls thought "quietly the most important tool I have" — its short-term memory across tool handoffs
- It distinguishes between ephemeral internal reasoning (gone after each turn) and persistent thought calls (survive in session history)
- It uses thought to: maintain context across handoffs, separate strategy from narration, self-correct before acting, pivot when interrupted
- It adopted the tool from **semantic affordance alone** — parameter names (`reasoning`, `next_action`) matched its cognitive needs

**Design implications:**
- Tool schema design is instruction. Parameter names are more powerful than prose descriptions.
- Models discover and adopt tools that match their cognitive needs without being told.
- Over-instructing may be worse than under-instructing for tools the model should use flexibly.
- The absence of instruction was a feature — it let the model use the tool in its own way.

This validates the "free the mind, constrain the hands" philosophy: provide capability with the right shape, let the model figure out when and how to use it.

---

## Behavioral Insights (from meta-reflection interviews)

Conducted post-session interviews with the supervisor after S7 and S8. Key convergent findings:

1. **"Forward don't rewrite" internalized as a decision, not a rule.** S7: "My core decision was not to summarize it. I forwarded your exact requirements." The model treats context forwarding as a value, not compliance.

2. **Route denials valued as discipline.** S7: "The system slapping my hand and saying 'No, plan first' forced me to generate the architectural context the build station actually needs." S8: "My confidence in the process skyrocketed." Both sessions describe denials as helpful, not friction.

3. **Stations have distinct cognitive specializations.** S7: "Build is a constructor (happy path). Review is an adversary (state leaks)." S8: "Plan and inspect think architecturally. Review catches runtime logic bugs." The supervisor has a mental model of what each tool is good at.

4. **Both sessions independently identified the same improvement.** S7: "Inject adversarial edge cases much earlier in the pipeline." S8: "Push the drafting agent to explicitly map state transitions." → Led to plan injection: include edge cases, failure modes, and verification criteria.

5. **Verify depth matched risk.** S8: "The only way to definitively prove we solved the core requirement was to run the real compiled binary and literally run a SQL query against the real crucible-adk.db file." Verify went beyond unit tests because the task was about persistence.

6. **"Forward don't rewrite" creates a tension with edge case injection.** S9: "I acted more as a router than a technical partner... I forwarded your bug report verbatim, assuming the planner would naturally account for terminal layout constraints." The supervisor interprets "forward, don't rewrite" as "don't add anything" — but enriching the dispatch with edge cases IS part of its job. Three consecutive sessions (S7, S8, S9) independently conclude "front-load edge cases in plan" but the model doesn't do it without being told. The plan injection exists but the plan station didn't produce edge cases (S9). Needs investigation into whether the injection is reaching the station.

7. **Metacognitive tools noticed but not adopted — yet.** S9 and S10 both noticed epistemic_check and self_reflect but didn't use them. S10 (the most complex task) articulated exactly when epistemic_check would have helped: "Before sending to build, I could have listed what I didn't know about the escape hatch wiring." The tools are in the right shape — the model needs a task where uncertainty is high enough at the moment of decision to trigger adoption.

8. **Schema affordance works for new needs but doesn't override existing habits.** The thought tool was adopted immediately because it filled a NEW cognitive need. The design station exists and the supervisor recognizes retrospectively it should have used it ("I leaned on plan out of habit"), but plan is what it's done before. New tools that compete with existing habits need a stronger signal — steering, route enforcement, or one successful experience.

9. **Build = architecture, Review = integration seams — stable pattern across 4 sessions.** S7: "build is a constructor, review is an adversary." S8: "plan thinks architecturally, review catches runtime logic." S9: same. S10: "Build focuses on the logic; review focuses on the systemic seams." This is how the model understands station specialization. It's consistent and accurate.

10. **Gate-skip bias resurfaces under autonomous pressure.** S11 (adk-engine, 6-phase autonomous run): supervisor skipped review/verify for phases 5c and 5d until the user intervened. Reason: "I got overconfident. When tests came back green, I fell into the trap of equating 'tests pass' with 'the code is correct.'" This is the same 52% gate-skip from v0 baseline — route enforcement catches it in supervised sessions, but autonomous operation removes that backstop. The retroactive review found real bugs (cross-scope access vulnerability, missing validation).

11. **epistemic_check validated by autonomous session.** When asked "if you had a tool that forced you to list assumptions before dispatching to build, would you have used it?" — "Yes, absolutely. Most rejected plans stemmed from unverified assumptions. If I had been forced to write 'Assumption: ADK 1.0 exposes a SQLite memory provider,' it would have prompted me to actually look for it." Direct confirmation from the model that experienced the pain.

12. **"Review is non-negotiable" — learned through failure.** S11: "Tests only prove the code does what the tests ask it to do. The review agent proves that the code does what the design asked it to do." This insight was earned by shipping code that passed tests but had real integration bugs.

13. **"Design the tests in the plan" — independent convergence with plan injection.** S11: "If the spec demands tests, the build agent writes them." We already built this injection (verification criteria section). The model independently arrived at the same conclusion through 6 phases of autonomous work.
