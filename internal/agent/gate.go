package agent

import (
	"context"
	"sync/atomic"

	"github.com/dmora/crucible/internal/permission"
)

// checkGate enforces per-station gates and runtime breakpoints.
// Returns (true, nil) if approved or no gate applies.
// Returns (false, nil) if the operator denied.
// Returns (false, err) on context cancellation or service error.
func checkGate(ctx context.Context, req permission.CreatePermissionRequest,
	gated bool, holdFlag *atomic.Bool, perms permission.Service,
) (bool, error) {
	holdActive := holdFlag != nil && holdFlag.Load()
	needsApproval := gated || holdActive

	if !needsApproval {
		return true, nil
	}
	if perms == nil {
		return true, nil // No permission service = no gate enforcement.
	}

	// Hold mode forces the operator prompt, bypassing yolo/allowlist/auto-approve.
	req.ForcePrompt = holdActive

	// Context cancellation propagates as a tool error, matching MCP
	// permission behavior.
	return perms.Request(ctx, req)
}
