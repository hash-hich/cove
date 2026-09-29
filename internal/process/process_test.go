package process_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/process"
)

func TestKillEndsTheProcessItNames(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "sleep", "60")
	require.NoError(t, cmd.Start())
	started, ok, err := process.StartTime(cmd.Process.Pid)
	require.NoError(t, err)
	require.True(t, ok)

	require.NoError(t, process.Process{PID: cmd.Process.Pid, Started: started + 1}.Kill())
	require.NoError(t, process.Process{PID: cmd.Process.Pid, Started: started}.Kill())

	var exit *exec.ExitError
	require.ErrorAs(t, cmd.Wait(), &exit)
	require.Equal(t, "signal: killed", exit.Error(), "only the pair of the pid and its start names the process")
}

func TestKillLeavesAProcessThatEnded(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "true")
	require.NoError(t, cmd.Run())

	require.NoError(t, process.Process{PID: cmd.Process.Pid, Started: 1}.Kill())
}
