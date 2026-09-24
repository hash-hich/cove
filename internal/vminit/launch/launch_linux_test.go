package launch_test

import (
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vminit/launch"
)

func TestParseSignal(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]syscall.Signal{
		"":        syscall.SIGTERM,
		"SIGINT":  syscall.SIGINT,
		"sigquit": syscall.SIGQUIT,
		"USR1":    syscall.SIGUSR1,
		"9":       syscall.SIGKILL,
	} {
		got, err := launch.ParseSignal(s)
		require.NoError(t, err, s)
		require.Equal(t, want, got, s)
	}
	for _, s := range []string{"SIGNOPE", "0", "65", "-1"} {
		_, err := launch.ParseSignal(s)
		require.EqualError(t, err, "image sets StopSignal "+s+", not a signal")
	}
}
