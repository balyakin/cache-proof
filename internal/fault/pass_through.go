package fault

import "cacheproof/internal/resp"

type PassThrough struct{}

func (PassThrough) Decide(*resp.Command) Action {
	return Action{Kind: ActionForward}
}

func (PassThrough) RefuseConnections() bool {
	return false
}

func (PassThrough) Name() string {
	return "pass-through"
}
