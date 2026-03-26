package agent

import (
	"fmt"
	"strings"
)

// TaskBuilder transforms raw task strings into station-ready prompts.
// Applies backend-aware skill wrapping and station-specific suffixes.
type TaskBuilder struct {
	station string
	skill   string
	backend string
}

// Build transforms the input into a station-ready prompt. firstTurn controls
// skill prefix injection (skills activate on first turn only). artifactContext
// is appended if non-empty (e.g. active plan path for downstream stations).
func (tb *TaskBuilder) Build(input stationInput, firstTurn bool, artifactContext string) string {
	task := FormatDispatch(input)
	if firstTurn && tb.skill != "" {
		switch tb.backend {
		case backendCodex:
			task = fmt.Sprintf("lets perform a %s: %s", tb.skill, task)
		case backendOpenCode, backendOpenCodeACP:
			task = fmt.Sprintf("/%s %s", tb.skill, task)
		default:
			task = fmt.Sprintf("Load your %s skill and then: %s", tb.skill, task)
		}
	}
	if firstTurn && (tb.station == "plan" || tb.station == "design") {
		task += "\n\nAlways provide the full path to the file you create."
		task += "\n\nInclude edge cases, failure modes, and negative state transitions — define what should NOT happen, not just what should. These prevent a full build-review-build rework cycle."
		task += `

Include a "Verification Criteria" section that a QA engineer could execute:
- Automated checks: which test commands to run, expected pass/fail counts
- API verification: endpoints to hit, expected status codes and response shapes
- Browser/UI verification: pages to open, elements to check, console errors to watch for
- Manual acceptance: step-by-step what the user should see when they run the feature
- Failure signals: what broken behavior looks like so verify knows when to fail
These become the gates that the verification step will execute against.`
	}
	if artifactContext != "" {
		task += "\n\n" + artifactContext
	}
	return task
}

// dispatchField pairs a display heading with its JSON key and items.
// Used by FormatDispatch and buildGateParams to avoid parallel field enumeration.
type dispatchField struct {
	Heading string // markdown heading (e.g. "Assumptions")
	Key     string // JSON key for gate params (e.g. "assumptions")
	Items   []string
}

// structuredFields returns the dispatch fields in canonical order.
func (si stationInput) structuredFields() []dispatchField {
	return []dispatchField{
		{"Context", "context_hints", si.ContextHints},
		{"Constraints", "constraints", si.Constraints},
		{"Success Criteria", "success_criteria", si.SuccessCriteria},
	}
}

// hasStructuredFields reports whether any dispatch fields beyond task are populated.
func (si stationInput) hasStructuredFields() bool {
	if si.TaskDescription != "" {
		return true
	}
	for _, f := range si.structuredFields() {
		if len(f.Items) > 0 {
			return true
		}
	}
	return false
}

// validateStationInput checks that all required fields are populated.
// Returns a descriptive error as a tool result (not an abort) so the
// model can correct and re-call.
func validateStationInput(input stationInput) error {
	var missing []string
	if input.Task == "" {
		missing = append(missing, "task")
	}
	if input.TaskDescription == "" {
		missing = append(missing, "task_description")
	}
	if len(input.ContextHints) == 0 {
		missing = append(missing, "context_hints")
	}
	if len(input.Constraints) == 0 {
		missing = append(missing, "constraints")
	}
	if len(input.SuccessCriteria) == 0 {
		missing = append(missing, "success_criteria")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s. All fields are required — put the goal in task, details in task_description, file paths in context_hints, boundaries in constraints, done-criteria in success_criteria",
			strings.Join(missing, ", "))
	}
	return nil
}

// FormatDispatch serializes stationInput into markdown for the station prompt.
func FormatDispatch(input stationInput) string {
	hasExtra := input.TaskDescription != "" || input.hasStructuredFields()
	if !hasExtra {
		return input.Task
	}
	var b strings.Builder
	if input.Task != "" {
		b.WriteString(input.Task)
		b.WriteString("\n")
	}
	if input.TaskDescription != "" {
		b.WriteString("\n")
		b.WriteString(input.TaskDescription)
		b.WriteString("\n")
	}
	for _, f := range input.structuredFields() {
		writeSection(&b, f.Heading, f.Items)
	}
	return b.String()
}

func writeSection(b *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", heading)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
}

// IsSpawnPerTurn reports whether the backend uses spawn-per-turn execution
// (prompt baked into CLI args via engine.Start, no Send). Used by the
// recovery loop to decide whether initialPrompt must carry the full task.
func (tb *TaskBuilder) IsSpawnPerTurn() bool {
	switch tb.backend {
	case backendCodex, backendOpenCode, backendOpenCodeACP:
		return true
	default:
		return false
	}
}
