package model

import (
	"path/filepath"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/home"
	"github.com/dmora/crucible/internal/ui/common"
	"github.com/stretchr/testify/assert"
)

func TestActivePlanInfo(t *testing.T) {
	s := testStyles()
	m := &UI{
		com: &common.Common{Styles: s},
	}

	t.Run("empty log returns empty", func(t *testing.T) {
		m.dispatchLog = nil
		assert.Equal(t, "", m.activePlanInfo(60))
	})

	t.Run("plan running returns empty", func(t *testing.T) {
		m.dispatchLog = []agent.DispatchEntry{
			{Station: "plan", Verdict: agent.VerdictRunning},
		}
		assert.Equal(t, "", m.activePlanInfo(60))
	})

	t.Run("plan done without artifact returns empty", func(t *testing.T) {
		m.dispatchLog = []agent.DispatchEntry{
			{Station: "plan", Verdict: agent.VerdictDone, ArtifactPath: ""},
		}
		assert.Equal(t, "", m.activePlanInfo(60))
	})

	t.Run("plan done with artifact renders link", func(t *testing.T) {
		m.dispatchLog = []agent.DispatchEntry{
			{Station: "plan", Verdict: agent.VerdictDone, ArtifactPath: "/tmp/plan.md"},
		}
		result := m.activePlanInfo(60)
		assert.Contains(t, result, "Active Plan")
		assert.Contains(t, result, "/tmp/plan.md")
	})

	t.Run("latest plan failed hides earlier success", func(t *testing.T) {
		m.dispatchLog = []agent.DispatchEntry{
			{Station: "plan", Verdict: agent.VerdictDone, ArtifactPath: "/tmp/old.md", Seq: 0},
			{Station: "build", Verdict: agent.VerdictDone, Seq: 1},
			{Station: "plan", Verdict: agent.VerdictFailed, Seq: 2},
		}
		assert.Equal(t, "", m.activePlanInfo(60))
	})

	t.Run("non-plan entries skipped to find plan", func(t *testing.T) {
		m.dispatchLog = []agent.DispatchEntry{
			{Station: "plan", Verdict: agent.VerdictDone, ArtifactPath: "/tmp/plan.md", Seq: 0},
			{Station: "build", Verdict: agent.VerdictDone, Seq: 1},
			{Station: "review", Verdict: agent.VerdictDone, Seq: 2},
		}
		result := m.activePlanInfo(60)
		assert.Contains(t, result, "/tmp/plan.md")
	})

	t.Run("narrow width does not panic", func(t *testing.T) {
		m.dispatchLog = []agent.DispatchEntry{
			{Station: "plan", Verdict: agent.VerdictDone, ArtifactPath: "/tmp/plan.md"},
		}
		assert.NotPanics(t, func() {
			result := m.activePlanInfo(10)
			assert.NotEmpty(t, result)
		})
	})

	t.Run("zero width does not panic", func(t *testing.T) {
		m.dispatchLog = []agent.DispatchEntry{
			{Station: "plan", Verdict: agent.VerdictDone, ArtifactPath: "/tmp/plan.md"},
		}
		assert.NotPanics(t, func() {
			m.activePlanInfo(0)
		})
	})

	t.Run("home-relative path displays tilde", func(t *testing.T) {
		homePath := filepath.Join(home.Dir(), "projects", "plan.md")
		m.dispatchLog = []agent.DispatchEntry{
			{Station: "plan", Verdict: agent.VerdictDone, ArtifactPath: homePath},
		}
		result := m.activePlanInfo(80)
		plain := ansi.Strip(result)
		assert.Contains(t, plain, "~/projects/plan.md",
			"display text should use tilde-shortened path")
		assert.NotContains(t, plain, home.Dir(),
			"display text should not contain the full home dir")
	})
}
