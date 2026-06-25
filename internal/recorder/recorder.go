package recorder

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"cacheproof/internal/resp"
)

type Recorder struct {
	mu                sync.Mutex
	scenario          string
	commandCounts     map[string]int
	commandByScenario map[string]map[string]int
	seenKeyHashes     map[string]struct{}
	maxValueSeen      int
	bigValueCount     int
	maxValueBytes     int
	observedRows      int
}

type Snapshot struct {
	CommandCounts       map[string]int
	CommandByScenario   map[string]map[string]int
	UniqueKeys          int
	MaxValueSeen        int
	BigValueCount       int
	ObservedCommandRows int
}

func New(maxValueBytes int) *Recorder {
	return &Recorder{
		scenario:          "unknown",
		commandCounts:     make(map[string]int),
		commandByScenario: make(map[string]map[string]int),
		seenKeyHashes:     make(map[string]struct{}),
		maxValueBytes:     maxValueBytes,
	}
}

func (r *Recorder) SetScenario(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scenario = name
	if _, ok := r.commandByScenario[name]; !ok {
		r.commandByScenario[name] = make(map[string]int)
	}
}

func (r *Recorder) Observe(cmd *resp.Command) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.observedRows++
	r.commandCounts[cmd.Name]++
	if _, ok := r.commandByScenario[r.scenario]; !ok {
		r.commandByScenario[r.scenario] = make(map[string]int)
	}
	r.commandByScenario[r.scenario][cmd.Name]++

	if key, ok := cmd.Key(); ok {
		r.seenKeyHashes[hashKey(key)] = struct{}{}
	}
	if size, ok := valueSize(cmd); ok {
		if size > r.maxValueSeen {
			r.maxValueSeen = size
		}
		if r.maxValueBytes > 0 && size > r.maxValueBytes {
			r.bigValueCount++
		}
	}
}

func (r *Recorder) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	commandCounts := make(map[string]int, len(r.commandCounts))
	for cmd, count := range r.commandCounts {
		commandCounts[cmd] = count
	}
	commandByScenario := make(map[string]map[string]int, len(r.commandByScenario))
	for scenario, counts := range r.commandByScenario {
		copied := make(map[string]int, len(counts))
		for cmd, count := range counts {
			copied[cmd] = count
		}
		commandByScenario[scenario] = copied
	}
	return Snapshot{
		CommandCounts:       commandCounts,
		CommandByScenario:   commandByScenario,
		UniqueKeys:          len(r.seenKeyHashes),
		MaxValueSeen:        r.maxValueSeen,
		BigValueCount:       r.bigValueCount,
		ObservedCommandRows: r.observedRows,
	}
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}
