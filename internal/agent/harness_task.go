package agent

import "fmt"

// TaskBuilder transforms raw task strings into station-ready prompts.
// Applies backend-aware skill wrapping and station-specific suffixes.
type TaskBuilder struct {
	station string
	skill   string
	backend string
}

// Build transforms the task. firstTurn controls skill prefix injection
// (skills activate on first turn only).
func (tb *TaskBuilder) Build(task string, firstTurn bool) string {
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
	if firstTurn && (tb.station == "draft" || tb.station == "design") {
		task += "\n\nAlways provide the full path to the file you create."
	}
	return task
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
