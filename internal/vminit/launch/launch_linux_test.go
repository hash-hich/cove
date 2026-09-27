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

func TestCounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		stat string
		want bool
	}{
		{name: "process", stat: "42 (node) S 1 42 42 0 -1 4194560 100 0 0", want: true},
		{name: "name with spaces and parentheses", stat: "42 (a) b (c) R 1 42 42 0 -1 4194560 1 0", want: true},
		{name: "zombie", stat: "42 (node) Z 1 42 42 0 -1 4194560 100 0 0"},
		{name: "kernel thread", stat: "2 (kthreadd) S 0 0 0 0 -1 2129984 0 0 0"},
		{name: "truncated", stat: "42 (node) S 1"},
		{name: "no name", stat: "42 node S 1 42 42 0 -1 4194560"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, launch.Counts(tt.stat))
		})
	}
}
