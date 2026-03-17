package agent

import (
	"time"

	"github.com/dmora/crucible/internal/event"
)

func (a *sessionAgent) eventPromptSent(sessionID string) {
	event.PromptSent(
		a.eventCommon(sessionID, a.largeModel.Get())...,
	)
}

func (a *sessionAgent) eventPromptResponded(sessionID string, duration time.Duration) {
	event.PromptResponded(
		append(
			a.eventCommon(sessionID, a.largeModel.Get()),
			"prompt duration pretty", duration.String(),
			"prompt duration in seconds", int64(duration.Seconds()),
		)...,
	)
}

func (a *sessionAgent) eventTokensUsed(sessionID string, model Model, usage UsageInfo, cost float64) {
	event.TokensUsed(
		append(
			a.eventCommon(sessionID, model),
			"input tokens", usage.PromptTokens,
			"output tokens", usage.CandidatesTokens,
			"thoughts tokens", usage.ThoughtsTokens,
			"total tokens", usage.TotalTokens,
			"cost", cost,
		)...,
	)
}

func (a *sessionAgent) eventCommon(sessionID string, model Model) []any {
	m := model.ModelCfg

	return []any{
		"session id", sessionID,
		"provider", m.Provider,
		"model", m.Model,
		"reasoning effort", m.ReasoningEffort,
		"thinking mode", m.Think,
	}
}
