package mcp

import (
	"context"
	"iter"
	"log/slog"
	"slices"

	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/csync"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Tool = mcp.Tool

var allTools = csync.NewMap[string, []*Tool]()

// Tools returns all available MCP tools.
func Tools() iter.Seq2[string, []*Tool] {
	return allTools.Seq2()
}

// RefreshTools gets the updated list of tools from the MCP and updates the
// global state.
func RefreshTools(ctx context.Context, cfg *config.Config, name string) {
	session, ok := sessions.Get(name)
	if !ok {
		slog.Warn("Refresh tools: no session", "name", name)
		return
	}

	tools, err := getTools(ctx, session)
	if err != nil {
		updateState(name, StateError, err, nil, Counts{})
		return
	}

	toolCount := updateTools(cfg, name, tools)

	prev, _ := states.Get(name)
	prev.Counts.Tools = toolCount
	updateState(name, StateConnected, nil, session, prev.Counts)
}

func getTools(ctx context.Context, session *ClientSession) ([]*Tool, error) {
	// Always call ListTools to get the actual available tools.
	// The InitializeResult Capabilities.Tools field may be an empty object {},
	// which is valid per MCP spec, but we still need to call ListTools to discover tools.
	result, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	return result.Tools, nil
}

func updateTools(cfg *config.Config, name string, tools []*Tool) int {
	tools = filterDisabledTools(cfg, name, tools)
	if len(tools) == 0 {
		allTools.Del(name)
		return 0
	}
	allTools.Set(name, tools)
	return len(tools)
}

// filterDisabledTools removes tools that are disabled via config.
func filterDisabledTools(cfg *config.Config, mcpName string, tools []*Tool) []*Tool {
	mcpCfg, ok := cfg.MCP[mcpName]
	if !ok || len(mcpCfg.DisabledTools) == 0 {
		return tools
	}

	filtered := make([]*Tool, 0, len(tools))
	for _, tool := range tools {
		if !slices.Contains(mcpCfg.DisabledTools, tool.Name) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}
