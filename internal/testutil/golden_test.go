package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssertGolden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.golden")
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}
	AssertGolden(t, path, []byte("ok"))
}

func TestAssertGoldenUpdate(t *testing.T) {
	t.Setenv("UPDATE_GOLDEN", "1")
	path := filepath.Join(t.TempDir(), "out.golden")
	AssertGolden(t, path, []byte("new"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read updated golden: %v", err)
	}
	if string(raw) != "new" {
		t.Fatalf("unexpected golden: %s", raw)
	}
}
