package noclobber

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestMoveRefusesOccupiedDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "inner", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("empty destination dir refused", func(t *testing.T) {
		dst := filepath.Join(dir, "dst-empty")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := Move(src, dst); !IsExist(err) {
			t.Fatalf("want ErrExist, got %v", err)
		}
		// Source untouched.
		if _, err := os.Stat(filepath.Join(src, "inner", "f.txt")); err != nil {
			t.Fatalf("source disturbed: %v", err)
		}
	})

	t.Run("occupied destination refused", func(t *testing.T) {
		dst := filepath.Join(dir, "dst-full")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, "occupant"), []byte("y"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Move(src, dst); !IsExist(err) {
			t.Fatalf("want ErrExist, got %v", err)
		}
	})

	t.Run("file destination refused", func(t *testing.T) {
		dst := filepath.Join(dir, "dst-file")
		if err := os.WriteFile(dst, []byte("z"), 0o644); err != nil {
			t.Fatal(err)
		}
		srcFile := filepath.Join(dir, "src-file")
		if err := os.WriteFile(srcFile, []byte("w"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Move(srcFile, dst); !IsExist(err) {
			t.Fatalf("want ErrExist, got %v", err)
		}
	})

	t.Run("free destination moves atomically", func(t *testing.T) {
		dst := filepath.Join(dir, "dst-free")
		if err := Move(src, dst); err != nil {
			t.Fatalf("Move: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dst, "inner", "f.txt")); err != nil {
			t.Fatalf("moved content missing: %v", err)
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Fatal("source must be gone after the move")
		}
	})

	var _ = errors.Is // keep errors if future asserts use it
}
