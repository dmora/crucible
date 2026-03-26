package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/term"
	"github.com/dmora/crucible/internal/config"
	"github.com/dmora/crucible/internal/db"
	"github.com/dmora/crucible/internal/db/global"
	"github.com/spf13/cobra"
)

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List sessions",
	Long:  "List sessions from the current project or across all projects",
	Example: `
# List sessions for the current project
crucible sessions

# List sessions across all projects
crucible sessions --all

# Output as JSON
crucible sessions --all --json

# Show only orphaned sessions
crucible sessions --all --orphans

# Rebuild global index from project DBs
crucible sessions --reconcile
  `,
	RunE: runSessions,
}

func init() {
	sessionsCmd.Flags().Bool("all", false, "List sessions across all projects")
	sessionsCmd.Flags().String("project", "", "Filter by project path")
	sessionsCmd.Flags().Bool("json", false, "Output as JSON")
	sessionsCmd.Flags().Bool("orphans", false, "Show only orphaned sessions")
	sessionsCmd.Flags().Bool("reconcile", false, "Rebuild global index from project DBs")
}

func runSessions(cmd *cobra.Command, _ []string) error {
	allFlag, _ := cmd.Flags().GetBool("all")
	projectFlag, _ := cmd.Flags().GetString("project")
	jsonFlag, _ := cmd.Flags().GetBool("json")
	orphansFlag, _ := cmd.Flags().GetBool("orphans")
	reconcileFlag, _ := cmd.Flags().GetBool("reconcile")
	ctx := cmd.Context()

	// Handle --reconcile: rebuild and exit.
	if reconcileFlag {
		gDB, _, err := global.Connect(ctx)
		if err != nil {
			return fmt.Errorf("opening global index: %w", err)
		}
		defer gDB.Close()
		if err := global.Reconcile(ctx, gDB); err != nil {
			return fmt.Errorf("reconciliation failed: %w", err)
		}
		cmd.Println("Reconciliation complete.")
		return nil
	}

	// Handle --all: query global index.
	if allFlag {
		return runGlobalSessions(cmd, projectFlag, jsonFlag, orphansFlag)
	}

	// Default: query local project DB.
	return runLocalSessions(cmd, jsonFlag)
}

func runLocalSessions(cmd *cobra.Command, jsonFlag bool) error {
	dataDir, _ := cmd.Flags().GetString("data-dir")
	ctx := cmd.Context()

	if dataDir == "" {
		cfg, err := config.Init("", "", false)
		if err != nil {
			return fmt.Errorf("initializing config: %w", err)
		}
		dataDir = cfg.Options.DataDirectory
	}

	conn, err := db.Connect(ctx, dataDir)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer conn.Close()

	q := db.New(conn)
	sessions, err := q.ListSessions(ctx)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if jsonFlag {
		return printJSON(cmd, sessions)
	}

	if len(sessions) == 0 {
		cmd.Println("No sessions found.")
		return nil
	}

	if term.IsTerminal(os.Stdout.Fd()) {
		t := table.New().
			Border(lipgloss.RoundedBorder()).
			StyleFunc(func(_, _ int) lipgloss.Style {
				return lipgloss.NewStyle().Padding(0, 2)
			}).
			Headers("Title", "Tokens", "Station", "Cost", "Updated")

		for _, s := range sessions {
			t.Row(
				truncate(s.Title, 40),
				formatTokens(s.TotalTokens),
				formatTokens(s.StationTokens),
				formatCost(s.Cost),
				relativeTime(s.UpdatedAt),
			)
		}
		lipgloss.Println(t)
		return nil
	}

	for _, s := range sessions {
		cmd.Printf("%s\t%s\t%s\t%s\t%s\n", s.Title, formatTokens(s.TotalTokens), formatTokens(s.StationTokens), formatCost(s.Cost), relativeTime(s.UpdatedAt))
	}
	return nil
}

func runGlobalSessions(cmd *cobra.Command, projectFilter string, jsonFlag, orphansFlag bool) error {
	ctx := cmd.Context()

	gDB, isNew, err := global.Connect(ctx)
	if err != nil {
		return fmt.Errorf("opening global index: %w", err)
	}
	defer gDB.Close()

	if isNew {
		if err := global.Reconcile(ctx, gDB); err != nil {
			return fmt.Errorf("reconciliation failed: %w", err)
		}
	}

	sessions, err := queryGlobalSessions(ctx, gDB, projectFilter)
	if err != nil {
		return err
	}

	if orphansFlag {
		sessions = filterOrphans(sessions)
	}

	if jsonFlag {
		return printJSON(cmd, sessions)
	}

	return printGlobalSessions(cmd, sessions)
}

func queryGlobalSessions(ctx context.Context, gDB *sql.DB, projectFilter string) ([]global.Session, error) {
	q := global.New(gDB)
	if projectFilter != "" {
		return q.ListSessionsByProject(ctx, projectFilter)
	}
	return q.ListSessions(ctx)
}

func filterOrphans(sessions []global.Session) []global.Session {
	var orphans []global.Session
	for _, s := range sessions {
		if _, err := os.Stat(s.Project); os.IsNotExist(err) {
			orphans = append(orphans, s)
		}
	}
	return orphans
}

func printGlobalSessions(cmd *cobra.Command, sessions []global.Session) error {
	if len(sessions) == 0 {
		cmd.Println("No sessions found.")
		return nil
	}

	if term.IsTerminal(os.Stdout.Fd()) {
		t := table.New().
			Border(lipgloss.RoundedBorder()).
			StyleFunc(func(_, _ int) lipgloss.Style {
				return lipgloss.NewStyle().Padding(0, 2)
			}).
			Headers("Project", "Title", "Model", "Tokens", "Cost", "Status", "Branch", "Updated")

		for _, s := range sessions {
			t.Row(
				filepath.Base(s.Project),
				truncate(s.Title, 25),
				truncate(s.Model, 20),
				formatTokens(s.Tokens),
				formatCost(s.Cost),
				s.Status,
				truncate(s.WorktreeBranch, 15),
				relativeTime(s.UpdatedAt),
			)
		}
		lipgloss.Println(t)
		return nil
	}

	for _, s := range sessions {
		cmd.Printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			filepath.Base(s.Project), s.Title, s.Model,
			formatTokens(s.Tokens), formatCost(s.Cost),
			s.Status, s.WorktreeBranch, relativeTime(s.UpdatedAt))
	}
	return nil
}

func printJSON(cmd *cobra.Command, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	cmd.Println(string(data))
	return nil
}

func formatTokens(tokens int64) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fK", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func formatCost(cost float64) string {
	return fmt.Sprintf("$%.2f", cost)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func relativeTime(unixTime int64) string {
	t := time.Unix(unixTime, 0)
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
