package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates a temp git repo with an initial commit and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
		{"git", "-C", dir, "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		// #nosec G204 -- test helper
		cmd := exec.Command(args[0], args[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git setup %v: %s: %v", args, out, err)
		}
	}
	return dir
}

func TestNewManager(t *testing.T) {
	repo := initTestRepo(t)
	// Resolve symlinks to match what NewManager does (macOS /var → /private/var).
	realRepo, _ := filepath.EvalSymlinks(repo)

	m, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}
	if m.repoRoot != realRepo {
		t.Errorf("repoRoot = %q, want %q", m.repoRoot, realRepo)
	}
	if m.subdirRel != "." {
		t.Errorf("subdirRel = %q, want %q", m.subdirRel, ".")
	}
}

func TestNewManagerNonGit(t *testing.T) {
	dir := t.TempDir()
	_, err := NewManager(dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestProvisionAndStatus(t *testing.T) {
	repo := initTestRepo(t)
	m, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}

	sessionID := "abcdef12-3456-7890-abcd-ef1234567890"

	info, err := m.Provision(sessionID)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasSuffix(info.Path, "abcdef12") {
		t.Errorf("Path = %q, want suffix 'abcdef12'", info.Path)
	}
	if info.Branch != "crucible/session-abcdef12" {
		t.Errorf("Branch = %q, want 'crucible/session-abcdef12'", info.Branch)
	}
	if info.ResolvedCWD != info.Path {
		t.Errorf("ResolvedCWD = %q, want %q (no subdir offset)", info.ResolvedCWD, info.Path)
	}

	// Directory should exist.
	if _, err := os.Stat(info.Path); err != nil {
		t.Errorf("worktree dir does not exist: %v", err)
	}

	// Status should find it.
	info2, ok := m.Status(sessionID)
	if !ok {
		t.Fatal("Status returned false for provisioned worktree")
	}
	if info2.Path != info.Path {
		t.Errorf("Status path = %q, want %q", info2.Path, info.Path)
	}
}

func TestProvisionIdempotent(t *testing.T) {
	repo := initTestRepo(t)
	m, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}

	sessionID := "abcdef12-3456-7890-abcd-ef1234567890"

	info1, err := m.Provision(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	info2, err := m.Provision(sessionID)
	if err != nil {
		t.Fatal(err)
	}

	if info1.Path != info2.Path {
		t.Errorf("second Provision returned different path: %q vs %q", info1.Path, info2.Path)
	}
}

func TestProvisionPreExistingBranch(t *testing.T) {
	repo := initTestRepo(t)
	m, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}

	sessionID := "bbbbbbbb-1111-2222-3333-444444444444"
	branch := "crucible/session-bbbbbbbb"

	// Create the branch manually.
	// #nosec G204 -- test code
	if out, err := exec.Command("git", "-C", repo, "branch", branch).CombinedOutput(); err != nil {
		t.Fatalf("creating branch: %s: %v", out, err)
	}

	info, err := m.Provision(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if info.Branch != branch {
		t.Errorf("Branch = %q, want %q", info.Branch, branch)
	}
}

func TestCleanup(t *testing.T) {
	repo := initTestRepo(t)
	m, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}

	sessionID := "cccccccc-1111-2222-3333-444444444444"

	info, err := m.Provision(sessionID)
	if err != nil {
		t.Fatal(err)
	}

	if err := m.Cleanup(sessionID); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Errorf("worktree dir still exists after cleanup")
	}

	_, ok := m.Status(sessionID)
	if ok {
		t.Error("Status returned true after cleanup")
	}
}

func TestSubdirectoryOffset(t *testing.T) {
	repo := initTestRepo(t)

	subdir := filepath.Join(repo, "cmd", "server")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(subdir)
	if err != nil {
		t.Fatal(err)
	}
	if m.subdirRel != filepath.Join("cmd", "server") {
		t.Errorf("subdirRel = %q, want %q", m.subdirRel, filepath.Join("cmd", "server"))
	}

	sessionID := "dddddddd-1111-2222-3333-444444444444"
	info, err := m.Provision(sessionID)
	if err != nil {
		t.Fatal(err)
	}

	wantCWD := filepath.Join(info.Path, "cmd", "server")
	if info.ResolvedCWD != wantCWD {
		t.Errorf("ResolvedCWD = %q, want %q", info.ResolvedCWD, wantCWD)
	}
}

func TestPrune(t *testing.T) {
	repo := initTestRepo(t)
	m, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}

	ids := []string{
		"11111111-aaaa-bbbb-cccc-dddddddddddd",
		"22222222-aaaa-bbbb-cccc-dddddddddddd",
		"33333333-aaaa-bbbb-cccc-dddddddddddd",
	}
	for _, id := range ids {
		if _, err := m.Provision(id); err != nil {
			t.Fatal(err)
		}
	}

	// Prune with only the first ID active.
	if err := m.Prune(ids[:1]); err != nil {
		t.Fatal(err)
	}

	// First should survive.
	if _, ok := m.Status(ids[0]); !ok {
		t.Error("active session worktree was pruned")
	}
	// Others should be gone.
	for _, id := range ids[1:] {
		if _, ok := m.Status(id); ok {
			t.Errorf("orphan worktree %s was not pruned", id[:8])
		}
	}
}

func TestPruneOrphanBranches(t *testing.T) {
	repo := initTestRepo(t)
	m, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}

	// Provision two worktrees, then remove their directories but leave branches.
	orphanID := "eeeeeeee-1111-2222-3333-444444444444"
	activeID := "ffffffff-1111-2222-3333-444444444444"

	for _, id := range []string{orphanID, activeID} {
		if _, err := m.Provision(id); err != nil {
			t.Fatal(err)
		}
	}

	// Manually remove the orphan's worktree directory (simulating a crash).
	orphanPath := filepath.Join(m.baseDir, orphanID[:8])
	// #nosec G204 -- test code
	exec.Command("git", "-C", repo, "worktree", "remove", "--force", orphanPath).Run()

	// Branch should still exist.
	orphanBranch := "crucible/session-" + orphanID[:8]
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", orphanBranch).Run(); err != nil {
		t.Fatal("orphan branch should exist before prune")
	}

	// Prune with only the active ID.
	if err := m.Prune([]string{activeID}); err != nil {
		t.Fatal(err)
	}

	// Orphan branch should be gone.
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", orphanBranch).Run(); err == nil {
		t.Error("orphan branch was not pruned")
	}

	// Active branch should survive.
	activeBranch := "crucible/session-" + activeID[:8]
	if err := exec.Command("git", "-C", repo, "rev-parse", "--verify", activeBranch).Run(); err != nil {
		t.Error("active branch was incorrectly pruned")
	}
}

func TestGitExclude(t *testing.T) {
	repo := initTestRepo(t)

	_, err := NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}

	excludePath := filepath.Join(repo, ".git", "info", "exclude")
	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), ".crucible/") {
		t.Error("exclude file does not contain .crucible/")
	}

	// Second NewManager should not duplicate.
	_, err = NewManager(repo)
	if err != nil {
		t.Fatal(err)
	}
	data2, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data2), ".crucible/") != 1 {
		t.Error("exclude file has duplicate .crucible/ entries")
	}
}
