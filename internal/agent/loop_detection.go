package agent

const (
	// maxConsecutiveToolCalls triggers when the same tool is called this many times in a row.
	maxConsecutiveToolCalls = 10
)

// loopDetector tracks tool calls to detect infinite loops.
// It checks consecutive identical calls only — no total cap so the supervisor
// can orchestrate long multi-station workflows without being cut short.
type loopDetector struct {
	lastTool string
	count    int
}

// track records a tool call name and returns true if a loop is detected.
func (ld *loopDetector) track(toolName string) bool {
	if toolName == ld.lastTool {
		ld.count++
	} else {
		ld.lastTool = toolName
		ld.count = 1
	}
	return ld.count >= maxConsecutiveToolCalls
}
