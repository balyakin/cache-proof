package probe

import "time"

type Result struct {
	ProbeName string
	Passed    bool
	Status    int
	Body      []byte
	ExitCode  int
	Output    []byte
	Latency   time.Duration
	Failures  []string
}
