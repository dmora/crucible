package agent

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/dmora/crucible/internal/askuser"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// askUserInput is the schema for the ask_user function tool.
type askUserInput struct {
	Questions []askUserQuestion `json:"questions" description:"1-4 structured questions for the user"`
}

// askUserQuestion is a single question in the ask_user input.
type askUserQuestion struct {
	ID          string          `json:"id"           description:"Stable identifier for this question (e.g. 'approach', 'scope')"`
	Question    string          `json:"question"     description:"Full question text"`
	Header      string          `json:"header"       description:"Short chip label (max 12 chars)"`
	Options     []askUserOption `json:"options"      description:"2-4 selectable options. Omit for free text input."`
	MultiSelect bool            `json:"multi_select" description:"Allow multiple selections (only when options present)"`
}

// askUserOption is a selectable option within a question.
type askUserOption struct {
	Label       string `json:"label"       description:"Display text (1-5 words). Add '(Recommended)' suffix to indicate preferred choice."`
	Description string `json:"description" description:"What this option means"`
}

// askUserOutput is the return schema for the ask_user function tool.
// Dual-format: Result string survives session reload via extractFunctionResponseContent(),
// Answers array provides structured data for the live LLM.
type askUserOutput struct {
	Result  string          `json:"result"  description:"Human-readable summary of all answers"`
	Answers []askUserAnswer `json:"answers" description:"Structured answers for programmatic use"`
}

// askUserAnswer is a single answer in the ask_user output.
type askUserAnswer struct {
	ID       string   `json:"id"       description:"Echoed question ID"`
	Question string   `json:"question" description:"Echoed question text"`
	Values   []string `json:"values"   description:"Selected labels; single-element for single-select/free-text"`
}

const (
	askUserToolName = "ask_user"
	maxQuestions    = 4
	maxOptionsPerQ  = 4
	minOptionsPerQ  = 2
	maxHeaderLen    = 12
)

// validateAskUserInput validates the input and converts it to askuser.Question slice.
func validateAskUserInput(input askUserInput) ([]askuser.Question, error) {
	if len(input.Questions) == 0 || len(input.Questions) > maxQuestions {
		return nil, fmt.Errorf("expected 1-%d questions, got %d", maxQuestions, len(input.Questions))
	}

	seenIDs := make(map[string]bool, len(input.Questions))
	questions := make([]askuser.Question, len(input.Questions))
	for i, q := range input.Questions {
		converted, err := validateQuestion(i, q, seenIDs)
		if err != nil {
			return nil, err
		}
		questions[i] = converted
	}
	return questions, nil
}

// validateQuestion validates a single question and converts it to an askuser.Question.
func validateQuestion(idx int, q askUserQuestion, seenIDs map[string]bool) (askuser.Question, error) {
	if q.ID == "" {
		return askuser.Question{}, fmt.Errorf("question %d: ID is required", idx)
	}
	if seenIDs[q.ID] {
		return askuser.Question{}, fmt.Errorf("question %d: duplicate ID %q", idx, q.ID)
	}
	seenIDs[q.ID] = true

	if q.Question == "" {
		return askuser.Question{}, fmt.Errorf("question %d: question text is required", idx)
	}
	if runes := []rune(q.Header); len(runes) > maxHeaderLen {
		q.Header = string(runes[:maxHeaderLen])
	}
	if len(q.Options) > 0 && (len(q.Options) < minOptionsPerQ || len(q.Options) > maxOptionsPerQ) {
		return askuser.Question{}, fmt.Errorf("question %d: expected %d-%d options, got %d", idx, minOptionsPerQ, maxOptionsPerQ, len(q.Options))
	}

	opts := make([]askuser.Option, len(q.Options))
	for j, o := range q.Options {
		opts[j] = askuser.Option{Label: o.Label, Description: o.Description}
	}

	return askuser.Question{
		ID:          q.ID,
		Question:    q.Question,
		Header:      q.Header,
		Options:     opts,
		MultiSelect: q.MultiSelect,
	}, nil
}

// buildAskUserOutput constructs the dual-format output from answers and original questions.
func buildAskUserOutput(resp *askuser.Response, questions []askuser.Question) askUserOutput {
	resultLines := make([]string, len(resp.Answers))
	answers := make([]askUserAnswer, len(resp.Answers))

	// Build a map from question ID → header for compact display.
	headers := make(map[string]string, len(questions))
	for _, q := range questions {
		if q.Header != "" {
			headers[q.ID] = q.Header
		}
	}

	for i, a := range resp.Answers {
		header := a.Question
		if h, ok := headers[a.ID]; ok {
			header = h
		}
		resultLines[i] = header + ": " + strings.Join(a.Values, ", ")
		answers[i] = askUserAnswer{
			ID:       a.ID,
			Question: a.Question,
			Values:   a.Values,
		}
	}

	return askUserOutput{
		Result:  strings.Join(resultLines, "\n"),
		Answers: answers,
	}
}

// newAskUserTool creates an ADK function tool for structured operator questions.
func newAskUserTool(svc askuser.Service, sessionID string, turnAbort *atomic.Bool) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        askUserToolName,
		Description: "Ask the user structured questions when you need their input — clarifying requirements, choosing between approaches, or confirming destructive operations. Batch related questions (up to 4). Each question needs a unique ID.",
	}, func(tctx tool.Context, input askUserInput) (askUserOutput, error) {
		questions, err := validateAskUserInput(input)
		if err != nil {
			return askUserOutput{}, err
		}

		resp, err := svc.Request(tctx, sessionID, tctx.FunctionCallID(), questions)
		if err != nil {
			return askUserOutput{}, err
		}
		if resp.Canceled {
			turnAbort.Store(true)
			return askUserOutput{}, fmt.Errorf("%s", askuser.ErrCanceledMessage) //nolint:goerr113 // deterministic string for reload detection
		}

		return buildAskUserOutput(resp, questions), nil
	})
}
