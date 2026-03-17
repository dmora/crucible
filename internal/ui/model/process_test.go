package model

import (
	"testing"
	"time"

	"github.com/dmora/crucible/internal/agent"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/ui/styles"
	"github.com/stretchr/testify/assert"
)

func testStyles() *styles.Styles {
	s := styles.NewStyles("steel-blue", false)
	return &s
}

func TestStationTimelineEmpty(t *testing.T) {
	s := testStyles()
	result := stationTimeline(s, nil, nil, nil, 60, 10)
	assert.Contains(t, result, "None")
}

func TestStationTimelineWaitingOnly(t *testing.T) {
	s := testStyles()
	stations := map[string]config.StationConfig{
		"draft":   {},
		"build":   {},
		"review":  {},
		"inspect": {},
	}
	result := stationTimeline(s, nil, nil, stations, 60, 10)
	assert.Contains(t, result, "build")
	assert.Contains(t, result, "draft")
	assert.Contains(t, result, "inspect")
	assert.Contains(t, result, "review")
	assert.Contains(t, result, "waiting")
}

func TestStationTimelineWithDispatches(t *testing.T) {
	s := testStyles()
	stations := map[string]config.StationConfig{
		"draft":   {},
		"build":   {},
		"review":  {},
		"inspect": {},
	}
	log := []agent.DispatchEntry{
		{Station: "draft", Verdict: agent.VerdictDone, StartedAt: time.Now().Add(-3 * time.Minute), Duration: 134 * time.Second, Seq: 0},
		{Station: "inspect", Verdict: agent.VerdictFailed, StartedAt: time.Now().Add(-1 * time.Minute), Duration: 62 * time.Second, Seq: 1},
		{Station: "draft", Verdict: agent.VerdictRunning, StartedAt: time.Now(), Seq: 2},
	}
	states := map[string]agent.ProcessInfo{
		"sess1:draft": {
			SessionID: "sess1",
			Station:   "draft",
			State:     agent.ProcessStateRunning,
			Model:     "claude-sonnet-4-5",
			StartedAt: time.Now(),
		},
	}

	result := stationTimeline(s, log, states, stations, 60, 10)
	// Should contain completed entries.
	assert.Contains(t, result, "done")
	assert.Contains(t, result, "failed")
	// Should contain waiting stations (build and review not dispatched).
	assert.Contains(t, result, "waiting")
}

func TestStationTimelineTruncation(t *testing.T) {
	s := testStyles()
	stations := map[string]config.StationConfig{
		"review": {},
	}
	// Create many completed entries.
	log := make([]agent.DispatchEntry, 10)
	for i := range log {
		log[i] = agent.DispatchEntry{
			Station:   "draft",
			Verdict:   agent.VerdictDone,
			StartedAt: time.Now(),
			Duration:  time.Minute,
			Seq:       i,
		}
	}

	// maxItems=4: 1 waiting + up to 3 completed.
	result := stationTimeline(s, log, nil, stations, 60, 4)
	assert.Contains(t, result, "earlier")
}

func TestFormatElapsedDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{0, "0:00"},
		{45 * time.Second, "0:45"},
		{134 * time.Second, "2:14"},
		{10*time.Minute + 5*time.Second, "10:05"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatElapsedDuration(tt.d))
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{500, "500"},
		{1234, "1.2k"},
		{42000, "42k"},
		{150000, "150k"},
		{200000, "200k"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatTokens(tt.n))
	}
}

func TestFuelGauge(t *testing.T) {
	assert.Equal(t, "42k/200k", fuelGauge(42000, 200000))
	assert.Equal(t, "42k ctx", fuelGauge(42000, 0))
}

func TestRenderCompletedDispatchWithFuelGauge(t *testing.T) {
	s := testStyles()
	entry := agent.DispatchEntry{
		Station:     "build",
		Verdict:     agent.VerdictDone,
		Duration:    134 * time.Second,
		ContextUsed: 42000,
		ContextSize: 200000,
	}
	states := map[string]agent.ProcessInfo{
		"s1:build": {Station: "build", Model: "claude-opus-4-6"},
	}
	result := renderCompletedDispatch(s, entry, states, 80)
	assert.Contains(t, result, "done")
	assert.Contains(t, result, "2:14")
	assert.Contains(t, result, "42k/200k")
	assert.Contains(t, result, "claude-opus-4-6")
}

func TestRenderCompletedDispatchWithoutFuelGauge(t *testing.T) {
	s := testStyles()
	entry := agent.DispatchEntry{
		Station:  "draft",
		Verdict:  agent.VerdictDone,
		Duration: 45 * time.Second,
	}
	result := renderCompletedDispatch(s, entry, nil, 80)
	assert.Contains(t, result, "done")
	assert.Contains(t, result, "0:45")
	assert.NotContains(t, result, "ctx")
	assert.NotContains(t, result, "/")
}

func TestStationEntryCount(t *testing.T) {
	tests := []struct {
		name     string
		log      []agent.DispatchEntry
		stations map[string]config.StationConfig
		want     int
	}{
		{
			name:     "no log, no stations",
			log:      nil,
			stations: nil,
			want:     0,
		},
		{
			name: "all stations waiting",
			log:  nil,
			stations: map[string]config.StationConfig{
				"draft": {}, "build": {}, "inspect": {}, "review": {}, "design": {},
			},
			want: 5, // 5 waiting × 1 line
		},
		{
			name: "some dispatched, some waiting",
			log: []agent.DispatchEntry{
				{Station: "draft", Verdict: agent.VerdictDone},
				{Station: "inspect", Verdict: agent.VerdictRunning},
			},
			stations: map[string]config.StationConfig{
				"draft": {}, "build": {}, "inspect": {}, "review": {}, "design": {},
			},
			want: 7, // 2 dispatched × 2 lines + 3 waiting × 1 line
		},
		{
			name: "duplicate dispatches count each entry",
			log: []agent.DispatchEntry{
				{Station: "draft", Verdict: agent.VerdictDone},
				{Station: "draft", Verdict: agent.VerdictRunning},
			},
			stations: map[string]config.StationConfig{
				"draft": {}, "build": {},
			},
			want: 5, // 2 dispatch entries × 2 lines + 1 waiting × 1 line
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, stationEntryCount(tt.log, tt.stations))
		})
	}
}

func TestGetDynamicHeightLimits(t *testing.T) {
	tests := []struct {
		name                                       string
		availableHeight, stationDemand, fileDemand int
		mcpDemand                                  int
		wantFiles, wantStations, wantMCPs          int
	}{
		{
			name:            "tiny terminal, all get minimum floor",
			availableHeight: 5, stationDemand: 5, fileDemand: 5, mcpDemand: 5,
			wantFiles: 2, wantStations: 2, wantMCPs: 2,
		},
		{
			name:            "5 stations, no files, no MCPs",
			availableHeight: 16, stationDemand: 5, fileDemand: 0, mcpDemand: 0,
			wantFiles: 2, wantStations: 5, wantMCPs: 2,
		},
		{
			name:            "5 stations, 10 files, 5 MCPs, budget 10",
			availableHeight: 16, stationDemand: 5, fileDemand: 10, mcpDemand: 5,
			wantFiles: 9, wantStations: 5, wantMCPs: 2,
		},
		{
			name:            "low demand across all sections",
			availableHeight: 30, stationDemand: 3, fileDemand: 3, mcpDemand: 3,
			wantFiles: 3, wantStations: 3, wantMCPs: 3,
		},
		{
			name:            "empty sections yield space to stations",
			availableHeight: 12, stationDemand: 8, fileDemand: 0, mcpDemand: 0,
			wantFiles: 2, wantStations: 8, wantMCPs: 2,
		},
		{
			name:            "station demand fills available space",
			availableHeight: 30, stationDemand: 20, fileDemand: 0, mcpDemand: 0,
			wantFiles: 2, wantStations: 20, wantMCPs: 2,
		},
		{
			name:            "file demand capped at max 10",
			availableHeight: 30, stationDemand: 0, fileDemand: 20, mcpDemand: 0,
			wantFiles: 10, wantStations: 2, wantMCPs: 2,
		},
		{
			name:            "all sections high demand, budget exhausted by priority",
			availableHeight: 14, stationDemand: 8, fileDemand: 10, mcpDemand: 8,
			wantFiles: 4, wantStations: 8, wantMCPs: 2,
		},
		{
			name:            "large terminal, high station demand expands past 8",
			availableHeight: 24, stationDemand: 11, fileDemand: 3, mcpDemand: 2,
			wantFiles: 3, wantStations: 11, wantMCPs: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFiles, gotStations, gotMCPs := getDynamicHeightLimits(
				tt.availableHeight, tt.stationDemand, tt.fileDemand, tt.mcpDemand,
			)
			assert.Equal(t, tt.wantFiles, gotFiles, "maxFiles")
			assert.Equal(t, tt.wantStations, gotStations, "maxStations")
			assert.Equal(t, tt.wantMCPs, gotMCPs, "maxMCPs")
		})
	}
}

func TestGetFilesWithChanges(t *testing.T) {
	files := []SessionFile{
		{Additions: 5, Deletions: 2},
		{Additions: 0, Deletions: 0},
		{Additions: 0, Deletions: 3},
		{Additions: 1, Deletions: 0},
	}
	result := getFilesWithChanges(files)
	assert.Len(t, result, 3)
	assert.Equal(t, 5, result[0].Additions)
	assert.Equal(t, 3, result[1].Deletions)
	assert.Equal(t, 1, result[2].Additions)

	// Empty input returns nil.
	assert.Nil(t, getFilesWithChanges(nil))
}

func TestTruncateCompleted(t *testing.T) {
	s := testStyles()

	// No truncation needed.
	completed := []string{"a", "b", "c"}
	result := truncateCompleted(s, completed, 10)
	assert.Equal(t, []string{"a", "b", "c"}, result)

	// Truncation: 5 completed, max 2.
	completed = []string{"a", "b", "c", "d", "e"}
	result = truncateCompleted(s, completed, 2)
	assert.Len(t, result, 2)
	assert.Contains(t, result[0], "earlier")
}
