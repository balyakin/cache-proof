package fault

import "cacheproof/internal/resp"

type ActionKind int

const (
	ActionForward ActionKind = iota
	ActionReplaceWithMiss
	ActionDropConnection
)

type Action struct {
	Kind ActionKind
}

type Engine interface {
	Decide(cmd *resp.Command) Action
	RefuseConnections() bool
	Name() string
}

var _ Engine = PassThrough{}
var _ Engine = RandomMiss{}
var _ Engine = Unavailable{}
