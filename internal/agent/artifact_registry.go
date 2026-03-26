package agent

import (
	"encoding/json"
	"errors"

	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
)

// activeArtifact records the latest artifact produced by a station.
// Stored in ADK session state so it survives session reloads.
type activeArtifact struct {
	Path    string `json:"path"`
	Station string `json:"station"`
	Seq     int    `json:"seq"`
	Version int    `json:"version"`
}

const activeArtifactKeyPrefix = "app:active_artifact:"

func activeArtifactKey(station string) string {
	return activeArtifactKeyPrefix + station
}

// setActiveArtifact writes or updates the active artifact for a station.
// Only called on VerdictDone with non-empty artifactPath.
func setActiveArtifact(tctx tool.Context, station, path string, seq int) {
	existing, _ := getActiveArtifact(tctx, station)
	version := 1
	if existing != nil {
		version = existing.Version + 1
	}
	art := activeArtifact{Path: path, Station: station, Seq: seq, Version: version}
	data, _ := json.Marshal(art)
	_ = tctx.State().Set(activeArtifactKey(station), string(data))
}

// getActiveArtifact reads the active artifact for a station.
// Returns (nil, nil) if the key is absent. Returns a non-nil error only for
// real infrastructure failures (DB errors), not missing keys.
func getActiveArtifact(tctx tool.Context, station string) (*activeArtifact, error) {
	raw, err := tctx.State().Get(activeArtifactKey(station))
	if err != nil {
		if isStateKeyMissing(err) {
			return nil, nil
		}
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	jsonStr, ok := raw.(string)
	if !ok {
		return nil, nil
	}
	var art activeArtifact
	if err := json.Unmarshal([]byte(jsonStr), &art); err != nil {
		return nil, err
	}
	return &art, nil
}

// isStateKeyMissing returns true if the error indicates the key simply doesn't
// exist in ADK session state (vs a real infrastructure error).
func isStateKeyMissing(err error) bool {
	return errors.Is(err, adksession.ErrStateKeyNotExist)
}
