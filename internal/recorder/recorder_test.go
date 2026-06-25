package recorder

import (
	"regexp"
	"testing"

	"cacheproof/internal/resp"

	"github.com/stretchr/testify/require"
)

func TestRecorderDoesNotExposeRawKeysAndCountsValues(t *testing.T) {
	rec := New(3)
	rec.SetScenario("baseline")
	rec.Observe(resp.NewCommand([]string{"SET", "secret_key", "value", "EX", "60"}, nil))
	rec.Observe(resp.NewCommand([]string{"HSET", "hash_key", "f1", "v1", "f2", "value2"}, nil))

	snapshot := rec.Snapshot()
	require.Equal(t, 2, snapshot.UniqueKeys)
	require.Equal(t, 6, snapshot.MaxValueSeen)
	require.Equal(t, 2, snapshot.BigValueCount)
	require.NotContains(t, snapshot.CommandCounts, "secret_key")
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{16}$`), hashKey("secret_key"))
}

func TestRecorderPerScenarioCounts(t *testing.T) {
	rec := New(1024)
	rec.SetScenario("baseline")
	rec.Observe(resp.NewCommand([]string{"GET", "a"}, nil))
	rec.SetScenario("random")
	rec.Observe(resp.NewCommand([]string{"GET", "a"}, nil))
	rec.Observe(resp.NewCommand([]string{"SET", "a", "b"}, nil))

	snapshot := rec.Snapshot()
	require.Equal(t, 1, snapshot.CommandByScenario["baseline"]["GET"])
	require.Equal(t, 1, snapshot.CommandByScenario["random"]["GET"])
	require.Equal(t, 1, snapshot.CommandByScenario["random"]["SET"])
}
