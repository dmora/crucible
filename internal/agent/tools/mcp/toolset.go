package mcp

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	adkagent "google.golang.org/adk/agent"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"

	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/permission"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpTool wraps a single MCP tool definition as an ADK-compatible tool.
// It implements tool.Tool and the internal FunctionTool/RequestProcessor
// interfaces via structural typing (matching method signatures without
// importing the internal interface packages).
type mcpTool struct {
	prefixedName    string                     // "mcp_<server>_<tool>"
	rawName         string                     // original MCP tool name
	desc            string                     // tool description
	funcDeclaration *genai.FunctionDeclaration // built once from MCP schema
	serverName      string
	cfg             *config.Config
	permissions     permission.Service
}

// --- tool.Tool (public interface) ---

func (t *mcpTool) Name() string        { return t.prefixedName }
func (t *mcpTool) Description() string { return t.desc }
func (t *mcpTool) IsLongRunning() bool { return false }

// --- toolinternal.FunctionTool (structural) ---

func (t *mcpTool) Declaration() *genai.FunctionDeclaration {
	return t.funcDeclaration
}

func (t *mcpTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	input, ok := args.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected map[string]any, got %T", args)
	}

	// Permission check — gates execution through Crucible's permission.Service.
	if t.permissions != nil {
		granted, err := t.permissions.Request(ctx, permission.CreatePermissionRequest{
			SessionID:   ctx.SessionID(),
			ToolCallID:  ctx.FunctionCallID(),
			ToolName:    t.prefixedName,
			Action:      "execute",
			Params:      input,
			Description: fmt.Sprintf("MCP %s: %s", t.serverName, t.rawName),
		})
		if err != nil {
			return nil, fmt.Errorf("permission check failed: %w", err)
		}
		if !granted {
			Registry.signalTurnAbort(ctx.SessionID())
			return map[string]any{
				"error":  "tool execution denied by user",
				"_abort": true,
			}, nil
		}
	}

	session, err := getOrRenewClient(ctx, t.cfg, t.serverName)
	if err != nil {
		return nil, fmt.Errorf("mcp server %q unavailable: %w", t.serverName, err)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      t.rawName,
		Arguments: input,
	})
	if err != nil {
		return nil, err
	}
	text := formatResult(result)
	if result.IsError {
		return map[string]any{"error": text}, nil
	}
	return map[string]any{"result": text}, nil
}

// --- toolinternal.RequestProcessor (structural) ---

func (t *mcpTool) ProcessRequest(_ tool.Context, req *adkmodel.LLMRequest) error {
	return packToolDeclaration(req, t)
}

// newMCPTool builds an mcpTool from an MCP tool definition.
func newMCPTool(serverName string, def *mcp.Tool, cfg *config.Config, perms permission.Service) *mcpTool {
	fd := &genai.FunctionDeclaration{
		Name:        "mcp_" + serverName + "_" + def.Name,
		Description: def.Description,
	}
	if def.InputSchema != nil {
		fd.ParametersJsonSchema = def.InputSchema
	}

	return &mcpTool{
		prefixedName:    fd.Name,
		rawName:         def.Name,
		desc:            def.Description,
		funcDeclaration: fd,
		serverName:      serverName,
		cfg:             cfg,
		permissions:     perms,
	}
}

// packToolDeclaration packs the tool's declaration into the LLM request.
// Mirrors ADK's internal toolutils.PackTool: registers in the dispatch map,
// initializes Config if nil, finds an existing genai.Tool with
// FunctionDeclarations (skipping GoogleSearch/CodeExecution entries),
// and appends the declaration.
func packToolDeclaration(req *adkmodel.LLMRequest, t *mcpTool) error {
	// Register in the dispatch map so ADK routes tool calls to this tool.
	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}
	if _, exists := req.Tools[t.prefixedName]; exists {
		return fmt.Errorf("duplicate MCP tool: %q", t.prefixedName)
	}
	req.Tools[t.prefixedName] = t

	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}

	// Find an existing genai.Tool entry that already carries FunctionDeclarations.
	var target *genai.Tool
	for _, gt := range req.Config.Tools {
		if gt != nil && gt.FunctionDeclarations != nil {
			target = gt
			break
		}
	}
	if target == nil {
		target = &genai.Tool{}
		req.Config.Tools = append(req.Config.Tools, target)
	}
	target.FunctionDeclarations = append(target.FunctionDeclarations, t.funcDeclaration)
	return nil
}

// formatResult converts MCP CallToolResult content to a string.
func formatResult(result *mcp.CallToolResult) string {
	var parts []string
	for _, v := range result.Content {
		switch c := v.(type) {
		case *mcp.TextContent:
			parts = append(parts, c.Text)
		case *mcp.ImageContent:
			parts = append(parts, "[image: "+c.MIMEType+"]")
		case *mcp.AudioContent:
			parts = append(parts, "[audio: "+c.MIMEType+"]")
		default:
			parts = append(parts, fmt.Sprintf("%v", v))
		}
	}
	return strings.Join(parts, "\n")
}

// mcpServerToolset implements tool.Toolset for a single MCP server.
type mcpServerToolset struct {
	serverName  string
	cfg         *config.Config
	permissions permission.Service
}

func (ts *mcpServerToolset) Name() string {
	return "mcp_" + ts.serverName
}

func (ts *mcpServerToolset) Tools(_ adkagent.ReadonlyContext) ([]tool.Tool, error) {
	// Gate on connection state — do not expose tools from disconnected servers.
	state, ok := states.Get(ts.serverName)
	if !ok || state.State != StateConnected {
		return nil, nil
	}

	rawTools, ok := allTools.Get(ts.serverName)
	if !ok {
		return nil, nil
	}

	adkTools := make([]tool.Tool, 0, len(rawTools))
	for _, def := range rawTools {
		adkTools = append(adkTools, newMCPTool(ts.serverName, def, ts.cfg, ts.permissions))
	}
	return adkTools, nil
}

// MCPToolsetRegistry manages MCP server toolsets for ADK integration.
type MCPToolsetRegistry struct {
	mu          sync.RWMutex
	servers     map[string]*mcpServerToolset
	cfg         *config.Config
	permissions permission.Service
	turnAborts  sync.Map // map[sessionID]*atomic.Bool
}

// SetTurnAbort registers a per-session abort flag for MCP tool permission denials.
func (r *MCPToolsetRegistry) SetTurnAbort(sessionID string, flag *atomic.Bool) {
	r.turnAborts.Store(sessionID, flag)
}

// ClearTurnAbort removes the per-session abort flag.
func (r *MCPToolsetRegistry) ClearTurnAbort(sessionID string) {
	r.turnAborts.Delete(sessionID)
}

// signalTurnAbort sets the abort flag for the given session, if registered.
func (r *MCPToolsetRegistry) signalTurnAbort(sessionID string) {
	if v, ok := r.turnAborts.Load(sessionID); ok {
		v.(*atomic.Bool).Store(true)
	}
}

// NewRegistry creates a new MCPToolsetRegistry.
func NewRegistry(cfg *config.Config, perms permission.Service) *MCPToolsetRegistry {
	return &MCPToolsetRegistry{
		servers:     make(map[string]*mcpServerToolset),
		cfg:         cfg,
		permissions: perms,
	}
}

// Register adds a toolset for the given MCP server.
func (r *MCPToolsetRegistry) Register(serverName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers[serverName] = &mcpServerToolset{
		serverName:  serverName,
		cfg:         r.cfg,
		permissions: r.permissions,
	}
	slog.Debug("MCP toolset registered", "server", serverName)
}

// Unregister removes the toolset for the given MCP server.
func (r *MCPToolsetRegistry) Unregister(serverName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.servers, serverName)
	slog.Debug("MCP toolset unregistered", "server", serverName)
}

// Toolsets returns all registered toolsets for ADK agent configuration.
func (r *MCPToolsetRegistry) Toolsets() []tool.Toolset {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]tool.Toolset, 0, len(r.servers))
	for _, ts := range r.servers {
		result = append(result, ts)
	}
	return result
}
