package agent

import (
	"cmp"
	"encoding/json"
	"log/slog"

	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// StatePersister saves station state and artifacts to ADK session state.
type StatePersister struct {
	station      string
	artifactType string
}

// PersistState saves ProcessInfo as durable state to ADK session state.
func (sp *StatePersister) PersistState(tctx tool.Context, sessionID string) {
	key := processStateKey(sessionID, sp.station)
	info, ok := processStates.Get(key)
	if !ok {
		return
	}
	ds := stationDurableState{
		Station:     info.Station,
		Backend:     info.Backend,
		Model:       info.Model,
		ResumeID:    info.ResumeID,
		ContextUsed: info.ContextUsed,
		ContextSize: info.ContextSize,
	}
	if !info.StartedAt.IsZero() {
		ds.StartedAt = info.StartedAt.Unix()
	}
	data, err := json.Marshal(ds)
	if err != nil {
		slog.Warn("Failed to marshal station durable state", "station", sp.station, "error", err)
		return
	}
	if setErr := tctx.State().Set(stationStateKey(sp.station), string(data)); setErr != nil {
		slog.Warn("Failed to persist station state", "station", sp.station, "error", setErr)
	}
}

// SaveArtifact persists the station result as a typed artifact.
// Uses sp.station and sp.artifactType internally.
func (sp *StatePersister) SaveArtifact(tctx tool.Context, result string) {
	if artifacts := tctx.Artifacts(); artifacts != nil {
		artifactType := cmp.Or(sp.artifactType, "result")
		artifactName := "station-" + sp.station + "-" + artifactType
		if _, saveErr := artifacts.Save(tctx, artifactName, genai.NewPartFromText(result)); saveErr != nil {
			slog.Warn("Failed to save station artifact",
				"station", sp.station, "artifact", artifactName, "error", saveErr)
		}
	}
}
