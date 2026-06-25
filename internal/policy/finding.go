package policy

type Level string

const (
	PASS Level = "PASS"
	WARN Level = "WARN"
	FAIL Level = "FAIL"
)

type Finding struct {
	Name    string `json:"name" validate:"required"`
	Level   Level  `json:"level" validate:"required,oneof=PASS WARN FAIL"`
	Message string `json:"message" validate:"required"`
}

func DefaultSuspicious() []string {
	return []string{
		"XADD", "XREAD", "XRANGE",
		"LPUSH", "RPUSH", "BLPOP", "BRPOP",
		"PUBLISH", "SUBSCRIBE", "PSUBSCRIBE",
		"PERSIST",
	}
}
