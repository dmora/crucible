package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveWithinRoots(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(sub, "file.md")
	if err := os.WriteFile(inside, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("absolute path inside root", func(t *testing.T) {
		got, err := resolveWithinRoots([]string{root}, inside)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// EvalSymlinks may canonicalize (e.g. /var -> /private/var on macOS).
		if !strings.HasSuffix(got, filepath.Join("sub", "file.md")) {
			t.Fatalf("unexpected resolved path: %q", got)
		}
	})

	t.Run("relative path resolves against first root", func(t *testing.T) {
		got, err := resolveWithinRoots([]string{root}, "sub/file.md")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasSuffix(got, filepath.Join("sub", "file.md")) {
			t.Fatalf("unexpected resolved path: %q", got)
		}
	})

	t.Run("traversal escape is rejected", func(t *testing.T) {
		if _, err := resolveWithinRoots([]string{root}, "../../etc/passwd"); err == nil {
			t.Fatal("expected traversal to be rejected")
		}
	})

	t.Run("absolute path outside roots is rejected", func(t *testing.T) {
		if _, err := resolveWithinRoots([]string{root}, "/etc/hosts"); err == nil {
			t.Fatal("expected outside path to be rejected")
		}
	})

	t.Run("empty path is rejected", func(t *testing.T) {
		if _, err := resolveWithinRoots([]string{root}, "  "); err == nil {
			t.Fatal("expected empty path to be rejected")
		}
	})

	t.Run("multiple roots — second root allowed", func(t *testing.T) {
		other := t.TempDir()
		f := filepath.Join(other, "x.txt")
		if err := os.WriteFile(f, []byte("y"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveWithinRoots([]string{root, other}, f); err != nil {
			t.Fatalf("expected second root to be allowed, got %v", err)
		}
	})

	t.Run("sibling dir sharing a name prefix is rejected", func(t *testing.T) {
		// e.g. root=".../proj" must not match ".../proj-evil/secret".
		sibling := root + "-evil"
		if err := os.MkdirAll(sibling, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(sibling, "secret")
		if err := os.WriteFile(f, []byte("nope"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := resolveWithinRoots([]string{root}, f); err == nil {
			t.Fatal("expected sibling-prefix path to be rejected")
		}
	})

	t.Run("root itself resolves to root", func(t *testing.T) {
		got, err := resolveWithinRoots([]string{root}, root)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want, _ := filepath.EvalSymlinks(filepath.Clean(root))
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

func TestReadCappedFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("reads text content", func(t *testing.T) {
		p := filepath.Join(dir, "a.md")
		if err := os.WriteFile(p, []byte("hello world"), 0o644); err != nil {
			t.Fatal(err)
		}
		content, truncated, err := readCappedFile(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if content != "hello world" || truncated {
			t.Fatalf("got content=%q truncated=%v", content, truncated)
		}
	})

	t.Run("rejects binary file", func(t *testing.T) {
		p := filepath.Join(dir, "bin")
		if err := os.WriteFile(p, []byte{0x01, 0x00, 0x02}, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readCappedFile(p); err == nil {
			t.Fatal("expected binary file to be rejected")
		}
	})

	t.Run("rejects directory", func(t *testing.T) {
		if _, _, err := readCappedFile(dir); err == nil {
			t.Fatal("expected directory to be rejected")
		}
	})

	t.Run("truncates oversized file", func(t *testing.T) {
		p := filepath.Join(dir, "big.txt")
		big := strings.Repeat("a", maxReadFileBytes+100)
		if err := os.WriteFile(p, []byte(big), 0o644); err != nil {
			t.Fatal(err)
		}
		content, truncated, err := readCappedFile(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !truncated {
			t.Fatal("expected truncated=true")
		}
		if len(content) != maxReadFileBytes {
			t.Fatalf("expected content capped at %d, got %d", maxReadFileBytes, len(content))
		}
	})

	t.Run("file exactly at cap is not truncated", func(t *testing.T) {
		p := filepath.Join(dir, "exact.txt")
		if err := os.WriteFile(p, []byte(strings.Repeat("a", maxReadFileBytes)), 0o644); err != nil {
			t.Fatal(err)
		}
		content, truncated, err := readCappedFile(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if truncated {
			t.Fatal("expected truncated=false for file exactly at cap")
		}
		if len(content) != maxReadFileBytes {
			t.Fatalf("expected %d bytes, got %d", maxReadFileBytes, len(content))
		}
	})

	t.Run("file one byte over cap is truncated", func(t *testing.T) {
		p := filepath.Join(dir, "over.txt")
		if err := os.WriteFile(p, []byte(strings.Repeat("a", maxReadFileBytes+1)), 0o644); err != nil {
			t.Fatal(err)
		}
		content, truncated, err := readCappedFile(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !truncated {
			t.Fatal("expected truncated=true for file one byte over cap")
		}
		if len(content) != maxReadFileBytes {
			t.Fatalf("expected content capped at %d, got %d", maxReadFileBytes, len(content))
		}
	})

	t.Run("missing file errors", func(t *testing.T) {
		if _, _, err := readCappedFile(filepath.Join(dir, "nope")); err == nil {
			t.Fatal("expected error for missing file")
		}
	})
}
