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

// SaveFileArtifact reads the file at path and persists its content as an
// artifact keyed by the path itself. This bridges the gap between files a
// station writes to disk (surfaced to the supervisor as artifact_path) and the
// internal artifact service: afterwards load_artifacts(path) returns the file's
// content, even for files written outside the project tree (e.g. plan output in
// ~/.claude/plans). Best-effort — binary/oversized/unreadable files are skipped.
func (sp *StatePersister) SaveFileArtifact(tctx tool.Context, path string) {
	artifacts := tctx.Artifacts()
	if artifacts == nil || path == "" {
		return
	}
	content, truncated, err := readCappedFile(path)
	if err != nil {
		slog.Debug("Skipping file artifact registration",
			"station", sp.station, "path", path, "error", err)
		return
	}
	if truncated {
		content += "\n\n[truncated at size cap]"
	}
	if _, saveErr := artifacts.Save(tctx, path, genai.NewPartFromText(content)); saveErr != nil {
		slog.Warn("Failed to save file artifact",
			"station", sp.station, "path", path, "error", saveErr)
	}
}
