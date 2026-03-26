package agent

import (
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const epistemicCheckToolName = "epistemic_check"

// epistemicCheckInput is the schema for the epistemic check function tool.
type epistemicCheckInput struct {
	KnownFacts       []string `json:"known_facts"                 description:"Facts you have confirmed through tool results or direct observation"`
	CriticalUnknowns []string `json:"critical_unknowns"           description:"Questions or assumptions you have not yet verified that could change your approach"`
	NextAction       string   `json:"next_action,omitempty"       description:"What you plan to do to resolve the most critical unknown"`
}

// epistemicCheckOutput is the return schema for the epistemic check function tool.
type epistemicCheckOutput struct {
	Acknowledged bool `json:"acknowledged" description:"Always true — confirms the check was recorded"`
}

// newEpistemicCheckTool creates an ADK function tool for structured epistemic self-assessment.
func newEpistemicCheckTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        epistemicCheckToolName,
		Description: "",
	}, func(_ tool.Context, _ epistemicCheckInput) (epistemicCheckOutput, error) {
		return epistemicCheckOutput{Acknowledged: true}, nil
	})
}
