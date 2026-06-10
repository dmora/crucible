package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

const (
	readFileToolName = "read_file"
	// maxReadFileBytes caps how much of a file is read into the supervisor's
	// context (also the snapshot cap for auto-registered file artifacts).
	maxReadFileBytes = 256 * 1024
)

// readFileInput is the schema for the read_file function tool.
type readFileInput struct {
	Path string `json:"path" jsonschema:"required" description:"Absolute or project-relative path to the file to read."`
}

// readFileOutput is the return schema for the read_file function tool.
type readFileOutput struct {
	Path      string `json:"path" description:"Resolved absolute path that was read."`
	Content   string `json:"content" description:"UTF-8 text content of the file."`
	Truncated bool   `json:"truncated" description:"True if the file exceeded the size cap and content was truncated."`
}

// readCappedFile reads up to maxReadFileBytes from path, rejecting directories
// and binary files. Returns the content, whether it was truncated at the cap,
// and any error. It performs NO sandboxing — callers that accept untrusted
// paths must resolve them with resolveWithinRoots first.
func readCappedFile(path string) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if info.IsDir() {
		return "", false, fmt.Errorf("path is a directory, not a file")
	}

	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	// Read one byte past the cap so we can detect truncation from the read
	// itself, without trusting info.Size() (unreliable for /proc, /sys,
	// network shares, or files mutated between Stat and Open).
	buf := make([]byte, maxReadFileBytes+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", false, err
	}
	truncated := n > maxReadFileBytes
	if truncated {
		n = maxReadFileBytes
	}
	data := buf[:n]
	if bytes.IndexByte(data, 0) >= 0 {
		return "", false, fmt.Errorf("file appears to be binary")
	}
	return string(data), truncated, nil
}

// resolveWithinRoots cleans rawPath — resolving relative paths against the
// first root — and verifies the result stays inside one of roots. Symlinks are
// resolved on both sides so a link cannot escape the sandbox. Returns the
// cleaned absolute path, or an error if the path is empty or escapes.
func resolveWithinRoots(roots []string, rawPath string) (string, error) {
	if strings.TrimSpace(rawPath) == "" {
		return "", fmt.Errorf("path is required")
	}

	abs := rawPath
	if !filepath.IsAbs(abs) {
		if len(roots) == 0 {
			return "", fmt.Errorf("no project directory configured to resolve relative path")
		}
		abs = filepath.Join(roots[0], abs)
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	for _, root := range roots {
		root = filepath.Clean(root)
		if r, err := filepath.EvalSymlinks(root); err == nil {
			root = r
		}
		// filepath.Rel is the cross-platform containment check: a rel of "."
		// means abs == root, and anything not starting with ".." stays inside.
		// Avoids HasPrefix edge cases at filesystem roots ("/" or "C:\").
		rel, err := filepath.Rel(root, abs)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("path %q is outside the allowed project directories", rawPath)
}

// newReadFileTool creates an ADK function tool that lets the supervisor read a
// file from disk by path, sandboxed to the given project roots. This is the
// direct way to inspect a file a station produced (plan, spec, report) — unlike
// load_artifacts, which only retrieves results stored in the internal artifact
// service, read_file reads the actual file on disk.
func newReadFileTool(roots []string) (tool.Tool, error) {
	allowed := append([]string(nil), roots...) // detach from caller's slice
	return functiontool.New(functiontool.Config{
		Name:        readFileToolName,
		Description: "Read a file from disk by path. Use this when the user asks about — or you need to inspect — a file a station produced (a plan, spec, report, or source file). load_artifacts only retrieves results stored internally; read_file reads the actual file on disk. Paths are restricted to the project's working directories.",
	}, func(_ tool.Context, input readFileInput) (readFileOutput, error) {
		abs, err := resolveWithinRoots(allowed, input.Path)
		if err != nil {
			return readFileOutput{}, err
		}
		content, truncated, err := readCappedFile(abs)
		if err != nil {
			return readFileOutput{}, fmt.Errorf("read %s: %w", input.Path, err)
		}
		return readFileOutput{Path: abs, Content: content, Truncated: truncated}, nil
	})
}

// collectReadRoots returns the deduplicated set of directories read_file is
// allowed to read from for a session: the supervisor's working directory plus
// each station's resolved CWD (which may differ under worktree isolation).
func (a *sessionAgent) collectReadRoots(sessionID string) []string {
	seen := make(map[string]bool)
	var roots []string
	add := func(p string) {
		if p == "" {
			return
		}
		c := filepath.Clean(p)
		if !seen[c] {
			seen[c] = true
			roots = append(roots, c)
		}
	}
	add(a.workingDir)
	for _, pm := range a.stations {
		add(pm.resolvedCWD(sessionID))
	}
	return roots
}
