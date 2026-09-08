package cli_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/cli"
)

func TestRunUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "no arguments", args: nil},
		{name: "unknown command", args: []string{"bogus"}},
		{name: "unknown flag", args: []string{"--bogus"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			app := &cli.App{Stdout: &stdout, Stderr: &stderr}
			code := app.Run(tt.args)

			require.Equal(t, cli.ExitUsage, code)
			require.Empty(t, stdout.String())
			require.Contains(t, stderr.String(), "Usage")
		})
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "flag", args: []string{"-h"}},
		{name: "command", args: []string{"help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			app := &cli.App{Stdout: &stdout, Stderr: &stderr}
			code := app.Run(tt.args)

			require.Equal(t, 0, code)
			require.Contains(t, stdout.String(), "Usage")
			require.Empty(t, stderr.String())
		})
	}
}
