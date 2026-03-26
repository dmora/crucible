// Package agent — sanitize plugin strips ADK's auto-appended agent identity
// text from the system instruction before it reaches the model.
//
// ADK's identityRequestProcessor unconditionally appends agent identity text
// ("You are an agent. Your internal name is ...") to the system instruction
// every turn. This is not configurable via llmagent.Config. The plugin removes
// it during BeforeModelCallback, which fires after all request processors.
//
// Wire this plugin FIRST in the chain so all other plugins see the clean state.
package agent

import (
	"strings"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
)

// identityMarker is the exact prefix ADK's identityRequestProcessor appends
// via utils.AppendInstructions with a "\n\n" separator.
// Pinned here so version bumps surface as a test failure (see canary test).
const identityMarker = "\n\nYou are an agent."

// newSanitizePlugin creates an ADK plugin that strips framework-injected
// identity text from the system instruction before it reaches the model.
func newSanitizePlugin() *plugin.Plugin {
	plug, _ := plugin.New(plugin.Config{
		Name:                "crucible_sanitize",
		BeforeModelCallback: sanitizeBeforeModel,
	})
	return plug
}

func sanitizeBeforeModel(_ adkagent.CallbackContext, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
	stripIdentity(req)
	return nil, nil //nolint:nilnil // ADK convention: nil,nil = continue to LLM
}

// stripIdentity removes ADK's auto-appended identity instruction from the
// system instruction. The identity text is concatenated onto the last Part
// via "\n\n" separator by utils.AppendInstructions.
func stripIdentity(req *adkmodel.LLMRequest) {
	if req.Config == nil || req.Config.SystemInstruction == nil {
		return
	}
	parts := req.Config.SystemInstruction.Parts
	if len(parts) == 0 {
		return
	}
	last := parts[len(parts)-1]
	if idx := strings.Index(last.Text, identityMarker); idx >= 0 {
		last.Text = last.Text[:idx]
	}
}
