package fault

import (
	"fmt"
	"testing"

	"cacheproof/internal/resp"

	"github.com/stretchr/testify/require"
)

func TestRandomMissDeterministic(t *testing.T) {
	cmd := resp.NewCommand([]string{"GET", "product:42"}, nil)
	engine := RandomMiss{Seed: 1, Probability: 0.3}
	first := engine.Decide(cmd)
	for i := 0; i < 1000; i++ {
		require.Equal(t, first, engine.Decide(cmd))
	}
}

func TestRandomMissProbabilityBounds(t *testing.T) {
	cmd := resp.NewCommand([]string{"GET", "k"}, nil)
	require.Equal(t, ActionForward, RandomMiss{Probability: 0}.Decide(cmd).Kind)
	require.Equal(t, ActionReplaceWithMiss, RandomMiss{Probability: 1}.Decide(cmd).Kind)
}

func TestRandomMissDistribution(t *testing.T) {
	engine := RandomMiss{Seed: 7, Probability: 0.3}
	misses := 0
	for i := 0; i < 10000; i++ {
		cmd := resp.NewCommand([]string{"GET", fmt.Sprintf("key:%d", i)}, nil)
		if engine.Decide(cmd).Kind == ActionReplaceWithMiss {
			misses++
		}
	}
	ratio := float64(misses) / 10000
	require.InDelta(t, 0.3, ratio, 0.05)
}

func TestRandomMissIgnoresUnsupportedReads(t *testing.T) {
	cmd := resp.NewCommand([]string{"MGET", "a", "b"}, nil)
	require.Equal(t, ActionForward, RandomMiss{Probability: 1}.Decide(cmd).Kind)
}

func TestPassThroughAndUnavailable(t *testing.T) {
	cmd := resp.NewCommand([]string{"GET", "a"}, nil)
	pass := PassThrough{}
	require.Equal(t, "pass-through", pass.Name())
	require.False(t, pass.RefuseConnections())
	require.Equal(t, ActionForward, pass.Decide(cmd).Kind)

	down := Unavailable{}
	require.Equal(t, "redis-unavailable", down.Name())
	require.True(t, down.RefuseConnections())
	require.Equal(t, ActionDropConnection, down.Decide(cmd).Kind)
	require.Equal(t, "random-miss", RandomMiss{}.Name())
	require.False(t, RandomMiss{}.RefuseConnections())
}
