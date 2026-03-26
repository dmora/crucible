package agent

import (
	"strings"
	"testing"
)

func TestIsSpawnPerTurn(t *testing.T) {
	tests := []struct {
		backend string
		want    bool
	}{
		{"codex", true},
		{"opencode", true},
		{"opencode-acp", true},
		{"claude", false},
		{"", false},
	}
	for _, tt := range tests {
		tb := &TaskBuilder{backend: tt.backend}
		if got := tb.IsSpawnPerTurn(); got != tt.want {
			t.Errorf("IsSpawnPerTurn(%q) = %v, want %v", tt.backend, got, tt.want)
		}
	}
}

func TestFormatDispatch_TaskOnly(t *testing.T) {
	got := FormatDispatch(stationInput{Task: "do the thing"})
	if !strings.Contains(got, "do the thing") {
		t.Errorf("task should be in output, got %q", got)
	}
}

func TestFormatDispatch_AllFields(t *testing.T) {
	input := stationInput{
		Task:            "implement auth",
		TaskDescription: "Add JWT-based auth to the API layer. See design doc for details.",
		ContextHints:    []string{"design doc at plans/auth.md"},
		Constraints:     []string{"no new dependencies"},
		SuccessCriteria: []string{"all tests pass"},
	}
	got := FormatDispatch(input)

	for _, want := range []string{
		"implement auth",
		"Add JWT-based auth",
		"## Context", "- design doc at plans/auth.md",
		"## Constraints", "- no new dependencies",
		"## Success Criteria", "- all tests pass",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got:\n%s", want, got)
		}
	}

	// Verify section order.
	order := []string{"## Context", "## Constraints", "## Success Criteria"}
	prev := 0
	for _, s := range order {
		idx := strings.Index(got, s)
		if idx < prev {
			t.Errorf("section %q appears before previous section", s)
		}
		prev = idx
	}
}

func TestFormatDispatch_PartialFields(t *testing.T) {
	got := FormatDispatch(stationInput{
		Task:        "do X",
		Constraints: []string{"no API changes"},
	})

	if !strings.Contains(got, "## Constraints") {
		t.Error("expected Constraints section")
	}
	for _, absent := range []string{"## Context", "## Success Criteria"} {
		if strings.Contains(got, absent) {
			t.Errorf("should not contain %q for unpopulated field", absent)
		}
	}
}

func TestFormatDispatch_EmptySlices(t *testing.T) {
	got := FormatDispatch(stationInput{
		Task:         "do Y",
		ContextHints: []string{},
	})
	if !strings.Contains(got, "do Y") {
		t.Errorf("task should be in output, got %q", got)
	}
}

func TestFormatDispatch_EmptyTask(t *testing.T) {
	got := FormatDispatch(stationInput{
		Task:        "",
		Constraints: []string{"no API changes"},
	})
	if strings.HasPrefix(got, "\n\n") {
		t.Errorf("empty task should not produce leading blank lines, got %q", got)
	}
	if !strings.Contains(got, "## Constraints") {
		t.Error("expected Constraints section")
	}
}

func TestValidateStationInput_AllFilled(t *testing.T) {
	err := validateStationInput(stationInput{
		Task:            "implement auth",
		TaskDescription: "Full details here",
		ContextHints:    []string{"file.go"},
		Constraints:     []string{"no deps"},
		SuccessCriteria: []string{"tests pass"},
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateStationInput_MissingFields(t *testing.T) {
	err := validateStationInput(stationInput{
		Task: "implement auth",
	})
	if err == nil {
		t.Fatal("expected error for missing fields")
	}
	for _, field := range []string{"task_description", "context_hints", "constraints", "success_criteria"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("error should mention %q, got: %s", field, err.Error())
		}
	}
}

func TestValidateStationInput_Empty(t *testing.T) {
	err := validateStationInput(stationInput{})
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	if !strings.Contains(err.Error(), "task") {
		t.Errorf("error should mention task, got: %s", err.Error())
	}
}

func TestBuild_StructuredInputWithSkill(t *testing.T) {
	tb := &TaskBuilder{
		station: "build",
		skill:   "feature-dev:feature-dev",
		backend: "claude",
	}
	input := stationInput{
		Task:        "implement auth",
		Constraints: []string{"no new deps"},
	}
	got := tb.Build(input, true, "")

	if !strings.HasPrefix(got, "Load your feature-dev:feature-dev skill and then: implement auth") {
		t.Errorf("expected skill prefix before formatted dispatch, got %q", got[:min(80, len(got))])
	}
	if !strings.Contains(got, "## Constraints") {
		t.Error("structured fields should be present after skill wrapping")
	}
}

func TestBuildWithContinuationBody(t *testing.T) {
	tb := &TaskBuilder{
		station: "build",
		skill:   "feature-dev:feature-dev",
		backend: "claude",
	}
	body := "<continuation_context>\n<mission>do the thing</mission>\n</continuation_context>"
	got := tb.Build(stationInput{Task: body}, true, "")

	if !strings.HasPrefix(got, "Load your feature-dev:feature-dev skill and then:") {
		t.Errorf("expected skill prefix, got %q", got[:min(60, len(got))])
	}
	if !strings.Contains(got, "<continuation_context>") {
		t.Error("continuation body should be preserved")
	}
}

func TestBuild_WithArtifactContext(t *testing.T) {
	tb := &TaskBuilder{
		station: "build",
		skill:   "feature-dev:feature-dev",
		backend: "claude",
	}
	input := stationInput{
		Task:        "implement auth",
		Constraints: []string{"no new deps"},
	}
	artifactCtx := "Active plan artifact: /tmp/spec.md (v1)"
	got := tb.Build(input, true, artifactCtx)

	if !strings.HasPrefix(got, "Load your feature-dev:feature-dev skill and then:") {
		t.Errorf("expected skill prefix, got %q", got[:min(60, len(got))])
	}
	if !strings.Contains(got, "Active plan artifact: /tmp/spec.md (v1)") {
		t.Error("artifact context should be appended to the prompt")
	}
	if !strings.Contains(got, "## Constraints") {
		t.Error("structured fields should still be present")
	}
}

func TestBuild_ArtifactContextEmpty_NoAppend(t *testing.T) {
	tb := &TaskBuilder{station: "build", backend: "claude"}
	got := tb.Build(stationInput{Task: "do X"}, false, "")
	if strings.Contains(got, "Active") {
		t.Error("empty artifact context should not add anything")
	}
}
