package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInitCommandWritesStarterAndRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(previous))
	})
	var out bytes.Buffer
	cmd := newInitCommand(&out)
	require.NoError(t, cmd.Execute())
	raw, err := os.ReadFile(filepath.Join(dir, "cacheproof.yml"))
	require.NoError(t, err)
	require.Contains(t, string(raw), "random_miss")
	require.NotContains(t, string(raw), "cold_cache")

	cmd = newInitCommand(&out)
	err = cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}
