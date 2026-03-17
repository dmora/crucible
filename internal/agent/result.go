package agent

import "time"

// AgentResult holds the result of an agent run.
type AgentResult struct {
	TotalUsage UsageInfo
}

// UsageInfo tracks token usage for an agent run.
type UsageInfo struct {
	PromptTokens     int64
	CandidatesTokens int64
	TotalTokens      int64
	ThoughtsTokens   int64
}

// TurnMetrics holds real-time metrics for the current agent turn,
// exposed to the UI for live progress display.
type TurnMetrics struct {
	StartTime     time.Time
	Usage         UsageInfo
	StreamedBytes int64 // accumulated bytes of streamed text + thinking content
}

// oneShotResult holds the output of a one-shot (ephemeral) ADK run.
type oneShotResult struct {
	Text  string
	Usage UsageInfo
}
