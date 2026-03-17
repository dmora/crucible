package agent

import (
	"strings"
	"testing"

	"github.com/dmora/crucible/internal/askuser"
)

func TestValidation_QuestionCount(t *testing.T) {
	tests := []struct {
		name    string
		count   int
		wantErr bool
	}{
		{"zero questions", 0, true},
		{"one question", 1, false},
		{"four questions", 4, false},
		{"five questions", 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := askUserInput{
				Questions: make([]askUserQuestion, tt.count),
			}
			for i := range input.Questions {
				input.Questions[i] = askUserQuestion{
					ID:       strings.Repeat("q", i+1), // unique IDs: "q", "qq", "qqq", etc.
					Question: "Question?",
					Header:   "Q",
					Options:  []askUserOption{{Label: "A"}, {Label: "B"}},
				}
			}

			_, err := validateAskUserInput(input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAskUserInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidation_OptionCount(t *testing.T) {
	tests := []struct {
		name     string
		optCount int
		wantErr  bool
	}{
		{"no options (free text)", 0, false},
		{"one option", 1, true},
		{"two options", 2, false},
		{"four options", 4, false},
		{"five options", 5, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := make([]askUserOption, tt.optCount)
			for i := range opts {
				opts[i] = askUserOption{Label: strings.Repeat("O", i+1)}
			}

			input := askUserInput{
				Questions: []askUserQuestion{{
					ID:       "q1",
					Question: "Question?",
					Header:   "Q",
					Options:  opts,
				}},
			}

			_, err := validateAskUserInput(input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAskUserInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidation_HeaderLength(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		wantErr    bool
		wantHeader string // expected header after truncation; empty means unchanged
	}{
		{"empty header", "", false, ""},
		{"12 chars", "123456789012", false, "123456789012"},
		{"13 chars", "1234567890123", false, "123456789012"},
		{"long header", "this is way too long for a header", false, "this is way "},
		{"multi-byte runes", "日本語テスト表示名前確認用追加文字", false, "日本語テスト表示名前確認"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := askUserInput{
				Questions: []askUserQuestion{{
					ID:       "q1",
					Question: "Question?",
					Header:   tt.header,
					Options:  []askUserOption{{Label: "A"}, {Label: "B"}},
				}},
			}

			qs, err := validateAskUserInput(input)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAskUserInput() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && tt.wantHeader != "" {
				got := qs[0].Header
				if got != tt.wantHeader {
					t.Errorf("header = %q, want %q", got, tt.wantHeader)
				}
				if runeLen := len([]rune(got)); runeLen > maxHeaderLen {
					t.Errorf("header rune length = %d, want <= %d", runeLen, maxHeaderLen)
				}
			}
		})
	}
}

func TestValidation_UniqueIDs(t *testing.T) {
	input := askUserInput{
		Questions: []askUserQuestion{
			{ID: "approach", Question: "Q1?", Header: "Q1", Options: []askUserOption{{Label: "A"}, {Label: "B"}}},
			{ID: "approach", Question: "Q2?", Header: "Q2", Options: []askUserOption{{Label: "C"}, {Label: "D"}}},
		},
	}

	_, err := validateAskUserInput(input)
	if err == nil {
		t.Fatal("expected error for duplicate IDs")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected 'duplicate' in error, got: %v", err)
	}
}

func TestValidation_EmptyID(t *testing.T) {
	input := askUserInput{
		Questions: []askUserQuestion{{
			ID:       "",
			Question: "Question?",
			Header:   "Q",
			Options:  []askUserOption{{Label: "A"}, {Label: "B"}},
		}},
	}

	_, err := validateAskUserInput(input)
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestValidation_EmptyQuestion(t *testing.T) {
	input := askUserInput{
		Questions: []askUserQuestion{{
			ID:       "q1",
			Question: "",
			Header:   "Q",
			Options:  []askUserOption{{Label: "A"}, {Label: "B"}},
		}},
	}

	_, err := validateAskUserInput(input)
	if err == nil {
		t.Fatal("expected error for empty question text")
	}
}

func TestResultFormat_SingleSelect(t *testing.T) {
	resp := &askuser.Response{
		Answers: []askuser.Answer{{
			ID:       "approach",
			Question: "Which approach?",
			Values:   []string{"Minimal fix"},
		}},
	}
	questions := []askuser.Question{{
		ID:     "approach",
		Header: "Approach",
	}}

	output := buildAskUserOutput(resp, questions)
	if output.Result != "Approach: Minimal fix" {
		t.Errorf("expected 'Approach: Minimal fix', got %q", output.Result)
	}
}

func TestResultFormat_MultiSelect(t *testing.T) {
	resp := &askuser.Response{
		Answers: []askuser.Answer{{
			ID:       "breaking",
			Question: "Breaking changes?",
			Values:   []string{"Yes", "No backward compat"},
		}},
	}
	questions := []askuser.Question{{
		ID:     "breaking",
		Header: "Breaking",
	}}

	output := buildAskUserOutput(resp, questions)
	if output.Result != "Breaking: Yes, No backward compat" {
		t.Errorf("expected 'Breaking: Yes, No backward compat', got %q", output.Result)
	}
}

func TestResultFormat_FreeText(t *testing.T) {
	resp := &askuser.Response{
		Answers: []askuser.Answer{{
			ID:       "reason",
			Question: "Why?",
			Values:   []string{"Performance concerns"},
		}},
	}
	questions := []askuser.Question{{
		ID:     "reason",
		Header: "Reason",
	}}

	output := buildAskUserOutput(resp, questions)
	if output.Result != "Reason: Performance concerns" {
		t.Errorf("expected 'Reason: Performance concerns', got %q", output.Result)
	}
}

func TestResultFormat_NoHeader(t *testing.T) {
	resp := &askuser.Response{
		Answers: []askuser.Answer{{
			ID:       "q1",
			Question: "Which approach?",
			Values:   []string{"A"},
		}},
	}
	questions := []askuser.Question{{
		ID:     "q1",
		Header: "", // no header — should fall back to question text
	}}

	output := buildAskUserOutput(resp, questions)
	if output.Result != "Which approach?: A" {
		t.Errorf("expected 'Which approach?: A', got %q", output.Result)
	}
}

func TestDualFormatOutput(t *testing.T) {
	resp := &askuser.Response{
		Answers: []askuser.Answer{
			{ID: "approach", Question: "Which approach?", Values: []string{"Minimal fix"}},
			{ID: "scope", Question: "What scope?", Values: []string{"Module-only"}},
		},
	}
	questions := []askuser.Question{
		{ID: "approach", Header: "Approach"},
		{ID: "scope", Header: "Scope"},
	}

	output := buildAskUserOutput(resp, questions)

	// Check Result string.
	expectedResult := "Approach: Minimal fix\nScope: Module-only"
	if output.Result != expectedResult {
		t.Errorf("Result:\n  got:  %q\n  want: %q", output.Result, expectedResult)
	}

	// Check Answers array.
	if len(output.Answers) != 2 {
		t.Fatalf("expected 2 answers, got %d", len(output.Answers))
	}
	if output.Answers[0].ID != "approach" {
		t.Errorf("expected answer[0].ID = 'approach', got %q", output.Answers[0].ID)
	}
	if output.Answers[0].Values[0] != "Minimal fix" {
		t.Errorf("expected answer[0].Values[0] = 'Minimal fix', got %q", output.Answers[0].Values[0])
	}
	if output.Answers[1].ID != "scope" {
		t.Errorf("expected answer[1].ID = 'scope', got %q", output.Answers[1].ID)
	}
}
