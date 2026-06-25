package appx

import "errors"

var (
	ErrConfig         = errors.New("config error")
	ErrUsage          = errors.New("usage error")
	ErrInfrastructure = errors.New("infrastructure error")
	ErrProbeFailed    = errors.New("probe failed")
)
