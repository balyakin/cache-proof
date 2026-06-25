package cacheproof

type ProbeOutcome struct {
	Name      string   `json:"name" validate:"required"`
	Passed    bool     `json:"passed"`
	LatencyMS int64    `json:"latency_ms"`
	Failures  []string `json:"failures"`
}

type ScenarioOutcome struct {
	Name   string         `json:"name" validate:"required"`
	Passed bool           `json:"passed"`
	Probes []ProbeOutcome `json:"probes" validate:"dive"`
}

type Summary struct {
	Failed   int `json:"failed" validate:"min=0"`
	Warnings int `json:"warnings" validate:"min=0"`
	Passed   int `json:"passed" validate:"min=0"`
}

type Finding struct {
	Name    string `json:"name" validate:"required"`
	Level   string `json:"level" validate:"required,oneof=PASS WARN FAIL"`
	Message string `json:"message" validate:"required"`
}

type RunResult struct {
	Disposable bool              `json:"disposable"`
	Summary    Summary           `json:"summary" validate:"required"`
	Scenarios  []ScenarioOutcome `json:"scenarios" validate:"dive"`
	Findings   []Finding         `json:"findings" validate:"dive"`
	LogPath    string            `json:"log_path,omitempty"`
}
