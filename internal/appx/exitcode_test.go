package appx

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExitCode(t *testing.T) {
	require.Equal(t, ExitOK, ExitCode(nil))
	require.Equal(t, ExitFindings, ExitCode(fmt.Errorf("%w: failed", ErrProbeFailed)))
	require.Equal(t, ExitConfigOrUsage, ExitCode(fmt.Errorf("%w: invalid", ErrConfig)))
	require.Equal(t, ExitConfigOrUsage, ExitCode(fmt.Errorf("%w: invalid", ErrUsage)))
	require.Equal(t, ExitInfrastructure, ExitCode(fmt.Errorf("%w: down", ErrInfrastructure)))
	require.Equal(t, ExitInfrastructure, ExitCode(fmt.Errorf("unknown")))
}
