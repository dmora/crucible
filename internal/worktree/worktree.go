// Package worktree manages per-session git worktree isolation.
//
// Each Crucible session gets its own worktree under <repoRoot>/.crucible/worktrees/<shortID>/,
// branching from HEAD at creation time. Stations within a session share the same worktree.
package worktree

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Info holds the resolved paths and branch name for a session's worktree.
type Info struct {
	Path        string // absolute path to worktree root (e.g. /repo/.crucible/worktrees/abc12345)
	Branch      string // e.g. "crucible/session-abc12345"
	ResolvedCWD string // worktree root + relative subdir offset
}

// Manager wraps git worktree CLI commands.
type Manager struct {
	repoRoot  string // from git rev-parse --show-toplevel
	baseDir   string // <repoRoot>/.crucible/worktrees/
	subdirRel string // relative path from repoRoot to original WorkingDir (may be ".")
}

const (
	shortIDLen = 8
	branchPfx  = "crucible/session-"
)

// NewManager creates a Manager for the given working directory.
// Returns an error if the directory is not inside a git repository.
func NewManager(workingDir string) (*Manager, error) {
	// Resolve symlinks so that filepath.Rel works on macOS (/var → /private/var).
	workingDir, err := filepath.EvalSymlinks(workingDir)
	if err != nil {
		return nil, fmt.Errorf("resolving working dir: %w", err)
	}

	// #nosec G204 -- args are constant strings, workingDir is from trusted config
	out, err := exec.Command("git", "-C", workingDir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, fmt.Errorf("not a git repository (or git not installed): %w", err)
	}
	repoRoot := strings.TrimSpace(string(out))

	// Resolve symlinks in repoRoot too (git may return a symlinked path).
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolving repo root: %w", err)
	}

	subdirRel, err := filepath.Rel(repoRoot, workingDir)
	if err != nil {
		return nil, fmt.Errorf("computing subdir offset: %w", err)
	}

	m := &Manager{
		repoRoot:  repoRoot,
		baseDir:   filepath.Join(repoRoot, ".crucible", "worktrees"),
		subdirRel: subdirRel,
	}

	if err := m.ensureGitExclude(); err != nil {
		slog.Warn("Failed to update .git/info/exclude", "error", err)
	}

	return m, nil
}

// Provision creates (or reuses) a worktree for the given session ID.
// Idempotent: if the worktree already exists, returns its info.
func (m *Manager) Provision(sessionID string) (*Info, error) {
	if info, ok := m.Status(sessionID); ok {
		return info, nil
	}

	shortID := m.shortID(sessionID)
	branch := branchPfx + shortID
	wtPath := filepath.Join(m.baseDir, shortID)

	if err := os.MkdirAll(m.baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating worktree base dir: %w", err)
	}

	if err := m.gitWorktreeAdd(wtPath, branch); err != nil {
		return nil, err
	}

	slog.Info("Provisioned worktree", "session", sessionID, "path", wtPath, "branch", branch)
	return m.infoFor(shortID, wtPath, branch), nil
}

// Cleanup removes the worktree and branch for a session.
func (m *Manager) Cleanup(sessionID string) error {
	shortID := m.shortID(sessionID)
	wtPath := filepath.Join(m.baseDir, shortID)
	branch := branchPfx + shortID

	m.removeWorktree(wtPath)
	m.deleteBranch(branch)

	slog.Info("Cleaned up worktree", "session", sessionID, "path", wtPath)
	return nil
}

// Prune removes worktrees whose session IDs are NOT in the active set.
// Also runs `git worktree prune` to clean up any git-level orphans.
func (m *Manager) Prune(activeSessionIDs []string) error {
	activeSet := m.buildActiveSet(activeSessionIDs)

	entries, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading worktree dir: %w", err)
	}

	errs := m.pruneOrphans(entries, activeSet)

	// Final git-level prune for anything we missed.
	// #nosec G204 -- all args are constant strings
	if out, err := exec.Command("git", "-C", m.repoRoot, "worktree", "prune").CombinedOutput(); err != nil {
		errs = append(errs, fmt.Errorf("git worktree prune: %s: %w", strings.TrimSpace(string(out)), err))
	}

	// Prune orphaned branches that may remain after worktree directories were
	// already removed (e.g. crash, manual deletion).
	errs = append(errs, m.pruneOrphanBranches(activeSet)...)

	return errors.Join(errs...)
}

// Status returns the worktree info for a session if it exists on disk.
func (m *Manager) Status(sessionID string) (*Info, bool) {
	shortID := m.shortID(sessionID)
	wtPath := filepath.Join(m.baseDir, shortID)

	if _, err := os.Stat(wtPath); err != nil {
		return nil, false
	}

	return m.infoFor(shortID, wtPath, branchPfx+shortID), true
}

// RepoRoot returns the detected git repository root.
func (m *Manager) RepoRoot() string {
	return m.repoRoot
}

func (m *Manager) shortID(sessionID string) string {
	if len(sessionID) < shortIDLen {
		return sessionID
	}
	return sessionID[:shortIDLen]
}

func (m *Manager) infoFor(shortID, wtPath, branch string) *Info {
	_ = shortID // clarity: shortID was used to derive wtPath and branch
	return &Info{
		Path:        wtPath,
		Branch:      branch,
		ResolvedCWD: filepath.Join(wtPath, m.subdirRel),
	}
}

// gitWorktreeAdd creates a git worktree, reusing the branch if it already exists.
func (m *Manager) gitWorktreeAdd(wtPath, branch string) error {
	// #nosec G204 -- branch/wtPath derived from session UUID + constant prefix
	branchExists := exec.Command("git", "-C", m.repoRoot, "rev-parse", "--verify", branch).Run() == nil

	var args []string
	if branchExists {
		args = []string{"-C", m.repoRoot, "worktree", "add", "-f", wtPath, branch}
	} else {
		args = []string{"-C", m.repoRoot, "worktree", "add", "-f", "-b", branch, wtPath}
	}

	// #nosec G204 -- args built from trusted constants and config-derived paths
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// removeWorktree removes a git worktree, logging on failure.
func (m *Manager) removeWorktree(wtPath string) {
	// #nosec G204 -- wtPath derived from baseDir + session short ID
	out, err := exec.Command("git", "-C", m.repoRoot, "worktree", "remove", "--force", wtPath).CombinedOutput()
	if err != nil {
		if _, statErr := os.Stat(wtPath); !os.IsNotExist(statErr) {
			slog.Warn("Failed to remove worktree", "path", wtPath, "output", strings.TrimSpace(string(out)))
		}
	}
}

// deleteBranch deletes a git branch, logging on failure.
func (m *Manager) deleteBranch(branch string) {
	// #nosec G204 -- branch is branchPfx + short ID (trusted)
	out, err := exec.Command("git", "-C", m.repoRoot, "branch", "-D", branch).CombinedOutput()
	if err != nil {
		slog.Debug("Branch delete skipped", "branch", branch, "output", strings.TrimSpace(string(out)))
	}
}

func (m *Manager) buildActiveSet(ids []string) map[string]bool {
	activeSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		if len(id) >= shortIDLen {
			activeSet[id[:shortIDLen]] = true
		}
	}
	return activeSet
}

func (m *Manager) pruneOrphans(entries []os.DirEntry, activeSet map[string]bool) []error {
	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() || activeSet[entry.Name()] {
			continue
		}
		shortID := entry.Name()
		wtPath := filepath.Join(m.baseDir, shortID)
		branch := branchPfx + shortID

		// #nosec G204 -- wtPath/branch derived from trusted baseDir + directory listing
		if out, err := exec.Command("git", "-C", m.repoRoot, "worktree", "remove", "--force", wtPath).CombinedOutput(); err != nil {
			errs = append(errs, fmt.Errorf("prune worktree %s: %s: %w", shortID, strings.TrimSpace(string(out)), err))
			continue
		}
		m.deleteBranch(branch)
		slog.Info("Pruned orphan worktree", "short_id", shortID, "path", wtPath)
	}
	return errs
}

// pruneOrphanBranches lists all crucible/session-* branches and deletes any
// whose short ID is not in the active set. This catches branches that remain
// after a worktree directory was removed (crash, manual deletion).
func (m *Manager) pruneOrphanBranches(activeSet map[string]bool) []error {
	// #nosec G204 -- all args are constant strings
	out, err := exec.Command("git", "-C", m.repoRoot,
		"branch", "--list", branchPfx+"*", "--format=%(refname:short)",
	).Output()
	if err != nil {
		return []error{fmt.Errorf("listing crucible branches: %w", err)}
	}

	var errs []error
	for _, branch := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		shortID := strings.TrimPrefix(branch, branchPfx)
		if activeSet[shortID] {
			continue
		}
		m.deleteBranch(branch)
		slog.Info("Pruned orphan branch", "branch", branch)
	}
	return errs
}

// ensureGitExclude appends ".crucible/" to .git/info/exclude if not already present.
func (m *Manager) ensureGitExclude() error {
	excludePath := filepath.Join(m.repoRoot, ".git", "info", "exclude")

	if f, err := os.Open(excludePath); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == ".crucible/" || line == ".crucible" {
				f.Close()
				return nil
			}
		}
		f.Close()
	}

	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(excludePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString("\n# Crucible worktrees\n.crucible/\n")
	return err
}
