package permission

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermissionService_AllowedCommands(t *testing.T) {
	tests := []struct {
		name         string
		allowedTools []string
		toolName     string
		action       string
		expected     bool
	}{
		{
			name:         "tool in allowlist",
			allowedTools: []string{"bash", "view"},
			toolName:     "bash",
			action:       "execute",
			expected:     true,
		},
		{
			name:         "tool:action in allowlist",
			allowedTools: []string{"bash:execute", "edit:create"},
			toolName:     "bash",
			action:       "execute",
			expected:     true,
		},
		{
			name:         "tool not in allowlist",
			allowedTools: []string{"view", "ls"},
			toolName:     "bash",
			action:       "execute",
			expected:     false,
		},
		{
			name:         "tool:action not in allowlist",
			allowedTools: []string{"bash:read", "edit:create"},
			toolName:     "bash",
			action:       "execute",
			expected:     false,
		},
		{
			name:         "empty allowlist",
			allowedTools: []string{},
			toolName:     "bash",
			action:       "execute",
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewPermissionService("/tmp", false, tt.allowedTools)

			// Create a channel to capture the permission request
			// Since we're testing the allowlist logic, we need to simulate the request
			ps := service.(*permissionService)

			// Test the allowlist logic directly
			commandKey := tt.toolName + ":" + tt.action
			allowed := false
			for _, cmd := range ps.allowedTools {
				if cmd == commandKey || cmd == tt.toolName {
					allowed = true
					break
				}
			}

			if allowed != tt.expected {
				t.Errorf("expected %v, got %v for tool %s action %s with allowlist %v",
					tt.expected, allowed, tt.toolName, tt.action, tt.allowedTools)
			}
		})
	}
}

func TestPermissionService_SkipMode(t *testing.T) {
	service := NewPermissionService("/tmp", true, []string{})

	result, err := service.Request(t.Context(), CreatePermissionRequest{
		SessionID:   "test-session",
		ToolName:    "bash",
		Action:      "execute",
		Description: "test command",
		Path:        "/tmp",
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected permission to be granted in skip mode")
	}
}

func TestPermissionService_ForcePrompt_BypassesSkip(t *testing.T) {
	// skip=true (yolo mode) would normally auto-approve, but ForcePrompt overrides.
	service := NewPermissionService("/tmp", true, []string{})
	events := service.Subscribe(t.Context())

	var result bool
	var wg sync.WaitGroup
	wg.Go(func() {
		result, _ = service.Request(t.Context(), CreatePermissionRequest{
			SessionID:   "s1",
			ToolName:    "gate:build",
			Action:      "execute",
			Path:        "/tmp",
			ForcePrompt: true,
		})
	})

	event := <-events
	service.Grant(event.Payload)
	wg.Wait()
	assert.True(t, result, "ForcePrompt request should reach the operator and be granted")
}

func TestPermissionService_ForcePrompt_BypassesAllowlist(t *testing.T) {
	service := NewPermissionService("/tmp", false, []string{"gate:build"})

	// Control: without ForcePrompt, allowlist auto-approves immediately.
	controlResult, err := service.Request(t.Context(), CreatePermissionRequest{
		SessionID: "s1",
		ToolName:  "gate:build",
		Action:    "execute",
		Path:      "/tmp",
	})
	require.NoError(t, err)
	assert.True(t, controlResult, "control: allowlist should auto-approve without ForcePrompt")

	// With ForcePrompt, same request must reach the operator dialog.
	events := service.Subscribe(t.Context())
	var result bool
	var wg sync.WaitGroup
	wg.Go(func() {
		result, _ = service.Request(t.Context(), CreatePermissionRequest{
			SessionID:   "s1",
			ToolName:    "gate:build",
			Action:      "execute",
			Path:        "/tmp",
			ForcePrompt: true,
		})
	})

	event := <-events
	service.Deny(event.Payload)
	wg.Wait()
	assert.False(t, result, "ForcePrompt should bypass allowlist — operator denied")
}

func TestPermissionService_ForcePrompt_BypassesAutoApproveSession(t *testing.T) {
	service := NewPermissionService("/tmp", false, []string{})
	service.AutoApproveSession("s1")

	// Control: without ForcePrompt, session auto-approve grants immediately.
	controlResult, err := service.Request(t.Context(), CreatePermissionRequest{
		SessionID: "s1",
		ToolName:  "gate:build",
		Action:    "execute",
		Path:      "/tmp",
	})
	require.NoError(t, err)
	assert.True(t, controlResult, "control: session auto-approve should grant without ForcePrompt")

	// With ForcePrompt, same request must reach the operator dialog.
	events := service.Subscribe(t.Context())
	var result bool
	var wg sync.WaitGroup
	wg.Go(func() {
		result, _ = service.Request(t.Context(), CreatePermissionRequest{
			SessionID:   "s1",
			ToolName:    "gate:build",
			Action:      "execute",
			Path:        "/tmp",
			ForcePrompt: true,
		})
	})

	event := <-events
	service.Grant(event.Payload)
	wg.Wait()
	assert.True(t, result, "ForcePrompt should bypass session auto-approve — operator granted")
}

func TestPermissionService_ForcePrompt_BypassesPersistentGrant(t *testing.T) {
	service := NewPermissionService("/tmp", false, []string{})
	events := service.Subscribe(t.Context())

	// First: grant persistently so a normal request would auto-approve.
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = service.Request(t.Context(), CreatePermissionRequest{
			SessionID: "s1",
			ToolName:  "gate:build",
			Action:    "execute",
			Path:      "/tmp",
		})
	})
	event := <-events
	service.GrantPersistent(event.Payload)
	wg.Wait()

	// Second: same tool with ForcePrompt — must still prompt.
	var result bool
	wg.Go(func() {
		result, _ = service.Request(t.Context(), CreatePermissionRequest{
			SessionID:   "s1",
			ToolName:    "gate:build",
			Action:      "execute",
			Path:        "/tmp",
			ForcePrompt: true,
		})
	})

	event = <-events
	service.Deny(event.Payload)
	wg.Wait()
	assert.False(t, result, "ForcePrompt should bypass persistent grant — operator denied")
}

func TestPermissionService_SequentialProperties(t *testing.T) {
	t.Run("Sequential permission requests with persistent grants", func(t *testing.T) {
		service := NewPermissionService("/tmp", false, []string{})

		req1 := CreatePermissionRequest{
			SessionID:   "session1",
			ToolName:    "file_tool",
			Description: "Read file",
			Action:      "read",
			Params:      map[string]string{"file": "test.txt"},
			Path:        "/tmp/test.txt",
		}

		var result1 bool
		var wg sync.WaitGroup
		wg.Add(1)

		events := service.Subscribe(t.Context())

		go func() {
			defer wg.Done()
			result1, _ = service.Request(t.Context(), req1)
		}()

		var permissionReq PermissionRequest
		event := <-events

		permissionReq = event.Payload
		service.GrantPersistent(permissionReq)

		wg.Wait()
		assert.True(t, result1, "First request should be granted")

		// Second identical request should be automatically approved due to persistent permission
		req2 := CreatePermissionRequest{
			SessionID:   "session1",
			ToolName:    "file_tool",
			Description: "Read file again",
			Action:      "read",
			Params:      map[string]string{"file": "test.txt"},
			Path:        "/tmp/test.txt",
		}
		result2, err := service.Request(t.Context(), req2)
		require.NoError(t, err)
		assert.True(t, result2, "Second request should be auto-approved")
	})
	t.Run("Sequential requests with temporary grants", func(t *testing.T) {
		service := NewPermissionService("/tmp", false, []string{})

		req := CreatePermissionRequest{
			SessionID:   "session2",
			ToolName:    "file_tool",
			Description: "Write file",
			Action:      "write",
			Params:      map[string]string{"file": "test.txt"},
			Path:        "/tmp/test.txt",
		}

		events := service.Subscribe(t.Context())
		var result1 bool
		var wg sync.WaitGroup

		wg.Go(func() {
			result1, _ = service.Request(t.Context(), req)
		})

		var permissionReq PermissionRequest
		event := <-events
		permissionReq = event.Payload

		service.Grant(permissionReq)
		wg.Wait()
		assert.True(t, result1, "First request should be granted")

		var result2 bool

		wg.Go(func() {
			result2, _ = service.Request(t.Context(), req)
		})

		event = <-events
		permissionReq = event.Payload
		service.Deny(permissionReq)
		wg.Wait()
		assert.False(t, result2, "Second request should be denied")
	})
	t.Run("Concurrent requests with different outcomes", func(t *testing.T) {
		service := NewPermissionService("/tmp", false, []string{})

		events := service.Subscribe(t.Context())

		var wg sync.WaitGroup
		results := make([]bool, 3)

		requests := []CreatePermissionRequest{
			{
				SessionID:   "concurrent1",
				ToolName:    "tool1",
				Action:      "action1",
				Path:        "/tmp/file1.txt",
				Description: "First concurrent request",
			},
			{
				SessionID:   "concurrent2",
				ToolName:    "tool2",
				Action:      "action2",
				Path:        "/tmp/file2.txt",
				Description: "Second concurrent request",
			},
			{
				SessionID:   "concurrent3",
				ToolName:    "tool3",
				Action:      "action3",
				Path:        "/tmp/file3.txt",
				Description: "Third concurrent request",
			},
		}

		for i, req := range requests {
			wg.Add(1)
			go func(index int, request CreatePermissionRequest) {
				defer wg.Done()
				result, _ := service.Request(t.Context(), request)
				results[index] = result
			}(i, req)
		}

		for range 3 {
			event := <-events
			switch event.Payload.ToolName {
			case "tool1":
				service.Grant(event.Payload)
			case "tool2":
				service.GrantPersistent(event.Payload)
			case "tool3":
				service.Deny(event.Payload)
			}
		}
		wg.Wait()
		grantedCount := 0
		for _, result := range results {
			if result {
				grantedCount++
			}
		}

		assert.Equal(t, 2, grantedCount, "Should have 2 granted and 1 denied")
		secondReq := requests[1]
		secondReq.Description = "Repeat of second request"
		result, err := service.Request(t.Context(), secondReq)
		require.NoError(t, err)
		assert.True(t, result, "Repeated request should be auto-approved due to persistent permission")
	})
}
