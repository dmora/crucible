package permission

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/dmora/crucible/internal/csync"
	"github.com/dmora/crucible/internal/pubsub"
	"github.com/google/uuid"
)

var ErrorPermissionDenied = errors.New("user denied permission")

type CreatePermissionRequest struct {
	SessionID   string `json:"session_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
	ForcePrompt bool   `json:"force_prompt,omitempty"` // bypass all auto-approvals (hold mode)
}

type PermissionNotification struct {
	ToolCallID string `json:"tool_call_id"`
	Granted    bool   `json:"granted"`
	Denied     bool   `json:"denied"`
}

type PermissionRequest struct {
	ID          string `json:"id"`
	SessionID   string `json:"session_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description"`
	Action      string `json:"action"`
	Params      any    `json:"params"`
	Path        string `json:"path"`
}

type Service interface {
	pubsub.Subscriber[PermissionRequest]
	GrantPersistent(permission PermissionRequest)
	Grant(permission PermissionRequest)
	Deny(permission PermissionRequest)
	Request(ctx context.Context, opts CreatePermissionRequest) (bool, error)
	AutoApproveSession(sessionID string)
	SetSkipRequests(skip bool)
	SkipRequests() bool
	SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[PermissionNotification]
}

type permissionService struct {
	*pubsub.Broker[PermissionRequest]

	notificationBroker    *pubsub.Broker[PermissionNotification]
	workingDir            string
	sessionPermissions    []PermissionRequest
	sessionPermissionsMu  sync.RWMutex
	pendingRequests       *csync.Map[string, chan bool]
	autoApproveSessions   map[string]bool
	autoApproveSessionsMu sync.RWMutex
	skip                  bool
	allowedTools          []string

	// used to make sure we only process one request at a time
	requestMu       sync.Mutex
	activeRequest   *PermissionRequest
	activeRequestMu sync.Mutex
}

func (s *permissionService) GrantPersistent(permission PermissionRequest) {
	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		ToolCallID: permission.ToolCallID,
		Granted:    true,
	})
	respCh, ok := s.pendingRequests.Get(permission.ID)
	if ok {
		respCh <- true
	}

	s.sessionPermissionsMu.Lock()
	s.sessionPermissions = append(s.sessionPermissions, permission)
	s.sessionPermissionsMu.Unlock()

	s.activeRequestMu.Lock()
	if s.activeRequest != nil && s.activeRequest.ID == permission.ID {
		s.activeRequest = nil
	}
	s.activeRequestMu.Unlock()
}

func (s *permissionService) Grant(permission PermissionRequest) {
	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		ToolCallID: permission.ToolCallID,
		Granted:    true,
	})
	respCh, ok := s.pendingRequests.Get(permission.ID)
	if ok {
		respCh <- true
	}

	s.activeRequestMu.Lock()
	if s.activeRequest != nil && s.activeRequest.ID == permission.ID {
		s.activeRequest = nil
	}
	s.activeRequestMu.Unlock()
}

func (s *permissionService) Deny(permission PermissionRequest) {
	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		ToolCallID: permission.ToolCallID,
		Granted:    false,
		Denied:     true,
	})
	respCh, ok := s.pendingRequests.Get(permission.ID)
	if ok {
		respCh <- false
	}

	s.activeRequestMu.Lock()
	if s.activeRequest != nil && s.activeRequest.ID == permission.ID {
		s.activeRequest = nil
	}
	s.activeRequestMu.Unlock()
}

func (s *permissionService) Request(ctx context.Context, opts CreatePermissionRequest) (bool, error) {
	if !opts.ForcePrompt && s.skip {
		return true, nil
	}

	// tell the UI that a permission was requested
	s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
		ToolCallID: opts.ToolCallID,
	})
	s.requestMu.Lock()
	defer s.requestMu.Unlock()

	perm := s.buildPermissionRequest(opts)

	// ForcePrompt (hold mode) bypasses all auto-approval paths.
	if !opts.ForcePrompt {
		if granted, ok := s.checkAutoApprovals(opts, perm); ok {
			return granted, nil
		}
	}

	return s.promptOperator(ctx, perm)
}

// checkAutoApprovals checks allowlist, session auto-approve, and prior grants.
// Returns (result, true) if an auto-approval matched, or (false, false) to
// fall through to the operator prompt.
func (s *permissionService) checkAutoApprovals(opts CreatePermissionRequest, perm PermissionRequest) (bool, bool) {
	commandKey := opts.ToolName + ":" + opts.Action
	if slices.Contains(s.allowedTools, commandKey) || slices.Contains(s.allowedTools, opts.ToolName) {
		return true, true
	}

	s.autoApproveSessionsMu.RLock()
	autoApprove := s.autoApproveSessions[opts.SessionID]
	s.autoApproveSessionsMu.RUnlock()

	if autoApprove {
		s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
			ToolCallID: opts.ToolCallID,
			Granted:    true,
		})
		return true, true
	}

	s.sessionPermissionsMu.RLock()
	defer s.sessionPermissionsMu.RUnlock()
	for _, p := range s.sessionPermissions {
		if p.ToolName == perm.ToolName && p.Action == perm.Action && p.SessionID == perm.SessionID && p.Path == perm.Path {
			s.notificationBroker.Publish(pubsub.CreatedEvent, PermissionNotification{
				ToolCallID: opts.ToolCallID,
				Granted:    true,
			})
			return true, true
		}
	}
	return false, false
}

// buildPermissionRequest resolves the directory and constructs a PermissionRequest.
func (s *permissionService) buildPermissionRequest(opts CreatePermissionRequest) PermissionRequest {
	dir := opts.Path
	if fileInfo, err := os.Stat(opts.Path); err == nil && !fileInfo.IsDir() {
		dir = filepath.Dir(opts.Path)
	}
	if dir == "." {
		dir = s.workingDir
	}
	return PermissionRequest{
		ID:          uuid.New().String(),
		Path:        dir,
		SessionID:   opts.SessionID,
		ToolCallID:  opts.ToolCallID,
		ToolName:    opts.ToolName,
		Description: opts.Description,
		Action:      opts.Action,
		Params:      opts.Params,
	}
}

// promptOperator publishes the permission request to the UI and blocks until
// the operator grants/denies or the context is cancelled.
func (s *permissionService) promptOperator(ctx context.Context, perm PermissionRequest) (bool, error) {
	s.activeRequestMu.Lock()
	s.activeRequest = &perm
	s.activeRequestMu.Unlock()

	respCh := make(chan bool, 1)
	s.pendingRequests.Set(perm.ID, respCh)
	defer s.pendingRequests.Del(perm.ID)

	// Publish the request
	s.Publish(pubsub.CreatedEvent, perm)

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case granted := <-respCh:
		return granted, nil
	}
}

func (s *permissionService) AutoApproveSession(sessionID string) {
	s.autoApproveSessionsMu.Lock()
	s.autoApproveSessions[sessionID] = true
	s.autoApproveSessionsMu.Unlock()
}

func (s *permissionService) SubscribeNotifications(ctx context.Context) <-chan pubsub.Event[PermissionNotification] {
	return s.notificationBroker.Subscribe(ctx)
}

func (s *permissionService) SetSkipRequests(skip bool) {
	s.skip = skip
}

func (s *permissionService) SkipRequests() bool {
	return s.skip
}

func NewPermissionService(workingDir string, skip bool, allowedTools []string) Service {
	return &permissionService{
		Broker:              pubsub.NewBroker[PermissionRequest](),
		notificationBroker:  pubsub.NewBroker[PermissionNotification](),
		workingDir:          workingDir,
		sessionPermissions:  make([]PermissionRequest, 0),
		autoApproveSessions: make(map[string]bool),
		skip:                skip,
		allowedTools:        allowedTools,
		pendingRequests:     csync.NewMap[string, chan bool](),
	}
}
