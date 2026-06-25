package fault

import (
	"encoding/binary"
	"hash/fnv"

	"cacheproof/internal/resp"
)

var readCommands = map[string]bool{
	"GET":    true,
	"GETEX":  true,
	"GETDEL": true,
	"HGET":   true,
}

type RandomMiss struct {
	Seed        uint64
	Probability float64
}

func (engine RandomMiss) Decide(cmd *resp.Command) Action {
	if !readCommands[cmd.Name] {
		return Action{Kind: ActionForward}
	}
	key, ok := cmd.Key()
	if !ok {
		return Action{Kind: ActionForward}
	}
	if shouldMiss(engine.Seed, key, engine.Probability) {
		return Action{Kind: ActionReplaceWithMiss}
	}
	return Action{Kind: ActionForward}
}

func (RandomMiss) RefuseConnections() bool {
	return false
}

func (RandomMiss) Name() string {
	return "random-miss"
}

func shouldMiss(seed uint64, key string, probability float64) bool {
	if probability <= 0 {
		return false
	}
	if probability >= 1 {
		return true
	}
	hash := fnv.New64a()
	var seedBytes [8]byte
	binary.LittleEndian.PutUint64(seedBytes[:], seed)
	_, _ = hash.Write(seedBytes[:])
	_, _ = hash.Write([]byte(key))
	fraction := float64(hash.Sum64()%1_000_000) / 1_000_000.0
	return fraction < probability
}
