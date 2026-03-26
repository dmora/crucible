package agent

import (
	"fmt"
	"log/slog"

	"google.golang.org/adk/tool"
)

// checkRoute enforces station routing constraints before dispatch.
// It checks both "requires" (must have completed in this session) and
// "afterDone" (must have completed more recently than this station's last
// VerdictDone) constraints against the session's dispatch log.
//
// Returns ("", nil) if all constraints pass.
// Returns (denial message, nil) if a constraint fails — the caller should
// return DENIED with the message, same as the gate rejection pattern.
func checkRoute(sessionID, station string, requires, afterDone []string) (string, error) {
	if len(requires) == 0 && len(afterDone) == 0 {
		return "", nil
	}

	log := GetDispatchLog(sessionID)

	// Check requires: each required station must have VerdictDone at least once.
	for _, req := range requires {
		if !hasCompleted(log, req) {
			return fmt.Sprintf(
				"DENIED: %q requires %q to complete first. Run %s before %s.",
				station, req, req, station,
			), nil
		}
	}

	// Check afterDone: each afterDone station must have VerdictDone more
	// recently (higher Seq) than this station's last VerdictDone.
	// If this station has never completed, afterDone does not apply.
	lastDoneSeq := lastDoneSeqFor(log, station)
	if lastDoneSeq >= 0 {
		for _, req := range afterDone {
			if lastDoneSeqFor(log, req) <= lastDoneSeq {
				return fmt.Sprintf(
					"DENIED: %q completed successfully. %q must run before %s can run again. Run %s.",
					station, req, station, req,
				), nil
			}
		}
	}

	return "", nil
}

// hasCompleted returns true if the station has at least one VerdictDone entry.
func hasCompleted(log []DispatchEntry, station string) bool {
	for i := range log {
		if log[i].Station == station && log[i].Verdict == VerdictDone {
			return true
		}
	}
	return false
}

// lastDoneSeqFor returns the highest Seq of a VerdictDone entry for the given
// station, or -1 if no such entry exists.
func lastDoneSeqFor(log []DispatchEntry, station string) int {
	seq := -1
	for i := range log {
		if log[i].Station == station && log[i].Verdict == VerdictDone && log[i].Seq > seq {
			seq = log[i].Seq
		}
	}
	return seq
}

// checkArtifact verifies that required upstream artifacts exist.
// Primary: reads active artifact registry from ADK session state.
// Fallback (legacy only): if app:artifact_registry_version is absent
// (pre-feature session), checks dispatch log for a VerdictDone entry.
// Current sessions (version marker present) get strict enforcement.
//
// Returns ("", nil) if all artifacts present or legacy fallback passes.
// Returns (denial, nil) if an artifact is missing.
func checkArtifact(
	tctx tool.Context,
	station string,
	requiresArtifact []string,
	disableEnforcement bool,
	sessionID string,
) (string, error) {
	if len(requiresArtifact) == 0 || disableEnforcement {
		return "", nil
	}
	// Runtime escape hatch.
	skipVal, skipErr := tctx.State().Get("app:skip_artifact_check")
	if skipErr != nil && !isStateKeyMissing(skipErr) {
		return "", fmt.Errorf("artifact check skip flag for %q: %w", station, skipErr)
	}
	if skipVal == "true" {
		return "", nil
	}

	// Determine if this is a legacy (pre-feature) session.
	// Missing key (ErrStateKeyNotExist) → legacy. Real DB error → propagate.
	registryVersion, rvErr := tctx.State().Get("app:artifact_registry_version")
	if rvErr != nil && !isStateKeyMissing(rvErr) {
		return "", fmt.Errorf("artifact check version marker for %q: %w", station, rvErr)
	}
	isLegacySession := registryVersion == nil

	for _, req := range requiresArtifact {
		art, err := getActiveArtifact(tctx, req)
		if err != nil {
			return "", fmt.Errorf("artifact check for %q: %w", station, err)
		}
		if art != nil && art.Path != "" {
			continue // registry has it — pass
		}
		// Legacy fallback: only for sessions created before this feature.
		// Current sessions must have an artifact in the registry — if plan
		// completed without producing a file, that's a real enforcement failure.
		if isLegacySession && hasCompleted(GetDispatchLog(sessionID), req) {
			slog.Info("Artifact check legacy fallback: station completed but no registry",
				"station", station, "requires", req, "sessionID", sessionID)
			continue
		}
		return fmt.Sprintf(
			"DENIED: %q requires an artifact from %q, but none exists. "+
				"Run %s first — it must produce a file.",
			station, req, req,
		), nil
	}
	return "", nil
}
