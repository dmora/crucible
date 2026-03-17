package agent

import (
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const thoughtToolName = "thought"

// thoughtInput is the schema for the thought function tool.
type thoughtInput struct {
	Reasoning  string `json:"reasoning"            description:"Your step-by-step reasoning about the current situation and decision"`
	NextAction string `json:"next_action,omitempty" description:"The action you plan to take based on this reasoning"`
	IsRevision bool   `json:"is_revision,omitempty" description:"True if this revises a previous thought based on new information"`
}

// thoughtOutput is the return schema for the thought function tool.
type thoughtOutput struct {
	Acknowledged bool `json:"acknowledged" description:"Always true — confirms reasoning was recorded"`
}

// thoughtHandler validates and processes a thought tool invocation.
func thoughtHandler(input thoughtInput) (thoughtOutput, error) {
	if strings.TrimSpace(input.Reasoning) == "" {
		return thoughtOutput{}, fmt.Errorf("reasoning is required")
	}
	return thoughtOutput{Acknowledged: true}, nil
}

// newThoughtTool creates an ADK function tool for structured supervisor reasoning.
func newThoughtTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        thoughtToolName,
		Description: "",
	}, func(_ tool.Context, input thoughtInput) (thoughtOutput, error) {
		return thoughtHandler(input)
	})
}
