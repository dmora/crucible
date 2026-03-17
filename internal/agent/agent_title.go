package agent

import (
	"cmp"
	"context"
	_ "embed"
	"log/slog"
	"regexp"
	"strings"

	adkagent "google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/session"
)

const (
	// titleMaxOutputTokens is the max output tokens for non-reasoning title generation.
	titleMaxOutputTokens int32 = 40
)

//go:embed templates/title.md
var titlePrompt []byte

// Used to remove <think> tags from generated titles.
var thinkTagRegex = regexp.MustCompile(`<think>.*?</think>`)

// generateTitle generates a session title based on the initial prompt.
func (a *sessionAgent) generateTitle(ctx context.Context, sessionID string, userPrompt string) {
	if userPrompt == "" {
		return
	}

	smallModel := a.smallModel.Get()
	largeModel := a.largeModel.Get()

	titleGenCfg := func(_ Model) *genai.GenerateContentConfig {
		noThinking := int32(0)
		return &genai.GenerateContentConfig{
			MaxOutputTokens: titleMaxOutputTokens,
			ThinkingConfig:  &genai.ThinkingConfig{ThinkingBudget: &noThinking},
		}
	}

	instruction := string(titlePrompt)
	prompt := userPrompt

	// Try small model first, fall back to large.
	model := smallModel
	res, err := runOneShot(ctx, model, adkAppName+"-title", instruction, prompt, titleGenCfg(model))
	if err != nil {
		slog.Error("Error generating title with small model; trying big model", "err", err)
		model = largeModel
		res, err = runOneShot(ctx, model, adkAppName+"-title", instruction, prompt, titleGenCfg(model))
		if err != nil {
			slog.Error("Error generating title with large model", "err", err)
			saveErr := a.sessions.UpdateTitleAndUsage(ctx, sessionID, DefaultSessionName, 0, 0, 0)
			if saveErr != nil {
				slog.Error("Failed to save session title and usage", "error", saveErr)
			}
			return
		}
		slog.Debug("Generated title with large model")
	} else {
		slog.Debug("Generated title with small model")
	}

	// Clean up title.
	title := strings.ReplaceAll(res.Text, "\n", " ")
	title = thinkTagRegex.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	title = cmp.Or(title, DefaultSessionName)

	cost := computeCost(model.Metadata, res.Usage)
	saveErr := a.sessions.UpdateTitleAndUsage(ctx, sessionID, title, res.Usage.PromptTokens, res.Usage.CandidatesTokens, cost)
	if saveErr != nil {
		slog.Error("Failed to save session title and usage", "error", saveErr)
	}
}

// runOneShot runs an ephemeral ADK agent with an in-memory session.
// Used for auxiliary tasks (title generation, etc.) that don't need persistent sessions.
func runOneShot(ctx context.Context, model Model, agentName, instruction, prompt string, genCfg *genai.GenerateContentConfig) (oneShotResult, error) {
	adkSvc := adksession.InMemoryService()
	adkResp, err := adkSvc.Create(ctx, &adksession.CreateRequest{
		AppName: adkAppName,
		UserID:  adkUserID,
	})
	if err != nil {
		return oneShotResult{}, err
	}

	llmAgent, err := llmagent.New(llmagent.Config{
		Name:                  agentName,
		Model:                 model.LLM,
		Instruction:           instruction,
		GenerateContentConfig: genCfg,
	})
	if err != nil {
		return oneShotResult{}, err
	}

	r, err := runner.New(runner.Config{
		AppName:        adkAppName,
		Agent:          llmAgent,
		SessionService: adkSvc,
	})
	if err != nil {
		return oneShotResult{}, err
	}

	userContent := genai.NewContentFromText(prompt, genai.RoleUser)
	var res oneShotResult
	for event, eventErr := range r.Run(ctx, adkUserID, adkResp.Session.ID(), userContent, adkagent.RunConfig{}) {
		if eventErr != nil {
			return oneShotResult{}, eventErr
		}
		if event == nil {
			continue
		}
		if event.UsageMetadata != nil {
			res.Usage = usageFromMetadata(event.UsageMetadata)
		}
		if event.Content != nil {
			for _, p := range event.Content.Parts {
				if p.Text != "" && !p.Thought {
					res.Text += p.Text
				}
			}
		}
	}
	return res, nil
}

// saveSessionUsage persists usage metrics using a targeted column update
// so it cannot clobber todos or other fields written during the turn.
// It drains the usage ledger (which may contain station deltas accumulated
// during this turn) and flushes everything to the DB in one additive write.
func (a *sessionAgent) saveSessionUsage(ctx context.Context, model Model, sess *session.Session, usage UsageInfo) {
	supervisorCost := computeCost(model.Metadata, usage)
	a.eventTokensUsed(sess.ID, model, usage, supervisorCost)

	// Add supervisor turn delta to the ledger.
	ledger := GetOrCreateLedger(sess.ID)
	supervisorTokens := usage.PromptTokens + usage.CandidatesTokens + usage.ThoughtsTokens
	ledger.Add(
		usage.PromptTokens,
		usage.CandidatesTokens,
		usage.ThoughtsTokens,
		0, 0, // no cache data from Gemini ADK currently
		supervisorCost,
	)

	// Drain all unpersisted deltas (supervisor + any station deltas).
	delta := ledger.Drain()

	// Station portion = total delta minus this supervisor turn's contribution.
	stationTokensDelta := delta.TotalTokens() - supervisorTokens
	if stationTokensDelta < 0 {
		stationTokensDelta = 0
	}

	if err := a.sessions.UpdateUsage(ctx, sess.ID,
		usage.PromptTokens,     // supervisor fuel gauge (absolute overwrite)
		usage.CandidatesTokens, // supervisor fuel gauge (absolute overwrite)
		delta.TotalTokens(),    // total_tokens += all sources
		stationTokensDelta,     // station_tokens += station portion only
		delta.CostUSD,          // cost += all sources
	); err != nil {
		slog.Error("Failed to save session usage", "session_id", sess.ID, "error", err)
	}
}

// maybeGenerateTitle returns a wait function that blocks until async title
// generation completes. If the session already has a title, it returns a no-op.
func (a *sessionAgent) maybeGenerateTitle(ctx context.Context, currentTitle, sessionID, prompt string) func() {
	if currentTitle != DefaultSessionName {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.generateTitle(ctx, sessionID, prompt)
	}()
	return func() { <-done }
}
