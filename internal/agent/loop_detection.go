package agent

const (
	// maxConsecutiveToolCalls triggers when the same tool is called this many times in a row.
	maxConsecutiveToolCalls = 5

	// maxTotalToolCalls is the absolute cap on tool calls per agent turn.
	// Catches alternating patterns (A→B→A→B...) that the consecutive detector misses.
	maxTotalToolCalls = 50
)

// loopDetector tracks tool calls to detect infinite loops.
// It checks both consecutive identical calls and total call count.
type loopDetector struct {
	lastTool   string
	count      int
	totalCalls int
}

// track records a tool call name and returns true if a loop is detected.
func (ld *loopDetector) track(toolName string) bool {
	ld.totalCalls++
	if toolName == ld.lastTool {
		ld.count++
	} else {
		ld.lastTool = toolName
		ld.count = 1
	}
	return ld.count >= maxConsecutiveToolCalls || ld.totalCalls >= maxTotalToolCalls
}
