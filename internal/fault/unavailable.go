package fault

import "cacheproof/internal/resp"

type Unavailable struct{}

func (Unavailable) Decide(*resp.Command) Action {
	return Action{Kind: ActionDropConnection}
}

func (Unavailable) RefuseConnections() bool {
	return true
}

func (Unavailable) Name() string {
	return "redis-unavailable"
}
