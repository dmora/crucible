package agent

import (
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const selfReflectToolName = "self_reflect"

// selfReflectInput is the schema for the self-reflection function tool.
type selfReflectInput struct {
	ActionTaken     string `json:"action_taken"              description:"The action or dispatch you just completed"`
	ConfidenceScore int    `json:"confidence_score"          description:"Your confidence in the outcome, 1 to 10"`
	ReasonForDoubt  string `json:"reason_for_doubt,omitempty" description:"What makes you uncertain about this result"`
}

// selfReflectOutput is the return schema for the self-reflection function tool.
type selfReflectOutput struct {
	Acknowledged bool `json:"acknowledged" description:"Always true — confirms the reflection was recorded"`
}

// newSelfReflectTool creates an ADK function tool for confidence calibration.
func newSelfReflectTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        selfReflectToolName,
		Description: "",
	}, func(_ tool.Context, _ selfReflectInput) (selfReflectOutput, error) {
		return selfReflectOutput{Acknowledged: true}, nil
	})
}
