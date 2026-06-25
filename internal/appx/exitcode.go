package appx

import "errors"

const (
	ExitOK             = 0
	ExitFindings       = 1
	ExitConfigOrUsage  = 2
	ExitInfrastructure = 3
)

func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	if errors.Is(err, ErrProbeFailed) {
		return ExitFindings
	}
	if errors.Is(err, ErrConfig) || errors.Is(err, ErrUsage) {
		return ExitConfigOrUsage
	}
	if errors.Is(err, ErrInfrastructure) {
		return ExitInfrastructure
	}
	return ExitInfrastructure
}
