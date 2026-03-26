package agent

import (
	"fmt"
	"testing"

	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"
)

func TestStripIdentity_WithDescription(t *testing.T) {
	req := &adkmodel.LLMRequest{Config: &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText(
					"You are a helpful assistant." +
						"\n\nYou are an agent. Your internal name is \"crucible\"." +
						" The description about you is \"Autonomous software orchestrator\"."),
			},
		},
	}}
	stripIdentity(req)
	got := req.Config.SystemInstruction.Parts[0].Text
	if got != "You are a helpful assistant." {
		t.Errorf("got %q, want %q", got, "You are a helpful assistant.")
	}
}

func TestStripIdentity_NoDescription(t *testing.T) {
	req := &adkmodel.LLMRequest{Config: &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText("My instruction.\n\nYou are an agent. Your internal name is \"crucible\"."),
			},
		},
	}}
	stripIdentity(req)
	got := req.Config.SystemInstruction.Parts[0].Text
	if got != "My instruction." {
		t.Errorf("got %q, want %q", got, "My instruction.")
	}
}

func TestStripIdentity_NoIdentityPresent(t *testing.T) {
	req := &adkmodel.LLMRequest{Config: &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText("Clean instruction with no identity appended."),
			},
		},
	}}
	stripIdentity(req)
	got := req.Config.SystemInstruction.Parts[0].Text
	if got != "Clean instruction with no identity appended." {
		t.Errorf("got %q, want %q", got, "Clean instruction with no identity appended.")
	}
}

func TestStripIdentity_NilSafety(_ *testing.T) {
	// None of these should panic.
	stripIdentity(&adkmodel.LLMRequest{})
	stripIdentity(&adkmodel.LLMRequest{Config: &genai.GenerateContentConfig{}})
	stripIdentity(&adkmodel.LLMRequest{Config: &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{},
	}})
}

// TestStripIdentity_CanaryFormat verifies our identityMarker matches what ADK
// actually produces. If ADK changes the format, this test fails — forcing us
// to update the marker rather than silently stop stripping.
func TestStripIdentity_CanaryFormat(t *testing.T) {
	name := "crucible"
	desc := "Autonomous software orchestrator"
	identity := fmt.Sprintf("You are an agent. Your internal name is %q.", name)
	identity += fmt.Sprintf(" The description about you is %q.", desc)

	instruction := "Base prompt." + "\n\n" + identity

	req := &adkmodel.LLMRequest{Config: &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(instruction)},
		},
	}}
	stripIdentity(req)
	got := req.Config.SystemInstruction.Parts[0].Text
	if got != "Base prompt." {
		t.Errorf("canary: identityMarker no longer matches ADK format — got %q, want %q", got, "Base prompt.")
	}
}
