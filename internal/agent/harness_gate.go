package agent

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/dmora/crucible/internal/permission"
)

// GateController enforces per-station gates and runtime hold breakpoints.
type GateController struct {
	station     string
	gated       bool                          // from StationConfig.Gate
	cwd         string                        // default CWD
	cwdResolver func(sessionID string) string // per-session CWD; nil = use gc.cwd
}

// effectiveCWD returns the per-session CWD if a resolver is set, otherwise the default.
func (gc *GateController) effectiveCWD(sessionID string) string {
	if gc.cwdResolver != nil {
		return gc.cwdResolver(sessionID)
	}
	return gc.cwd
}

// Check returns (approved, error). sessionID is required for the
// permission request. Composes the per-station gate bool with the
// runtime hold flag, then delegates to checkGate().
func (gc *GateController) Check(ctx context.Context, sessionID, functionCallID string, input stationInput, holdFlag *atomic.Bool, perms permission.Service) (bool, error) {
	return checkGate(ctx, permission.CreatePermissionRequest{
		SessionID:   sessionID,
		ToolCallID:  functionCallID,
		ToolName:    "gate:" + gc.station,
		Description: fmt.Sprintf("%q requests approval to execute", gc.station),
		Action:      "execute",
		Params:      buildGateParams(input),
		Path:        gc.effectiveCWD(sessionID),
	}, gc.gated, holdFlag, perms)
}

// buildGateParams converts stationInput to a map for the gate permission
// request. Only includes non-empty structured fields.
func buildGateParams(input stationInput) map[string]any {
	params := map[string]any{"task": input.Task, "task_description": input.TaskDescription}
	for _, f := range input.structuredFields() {
		if len(f.Items) > 0 {
			params[f.Key] = f.Items
		}
	}
	return params
}
