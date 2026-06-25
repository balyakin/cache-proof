package proxy

var unsupportedUnderFault = map[string]bool{
	"SUBSCRIBE":  true,
	"PSUBSCRIBE": true,
	"PUBLISH":    true,
	"BLPOP":      true,
	"BRPOP":      true,
	"BZPOPMIN":   true,
	"BZPOPMAX":   true,
	"MONITOR":    true,
	"MULTI":      true,
	"EXEC":       true,
	"WATCH":      true,
	"UNWATCH":    true,
	"EVAL":       true,
	"EVALSHA":    true,
}
