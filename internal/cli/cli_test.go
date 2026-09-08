package cli_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/cli"
	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// runArgs prefixes args with the run command.
func runArgs(args ...string) []string {
	return append([]string{"run"}, args...)
}

func TestRunUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "no arguments", args: nil},
		{name: "unknown command", args: []string{"bogus"}},
		{name: "unknown flag", args: []string{"--bogus"}},
		{name: "run unknown flag", args: runArgs("--bogus"), wantStderr: "not defined: -bogus"},
		{name: "run docker session flags", args: runArgs("-it"), wantStderr: "not defined: -it"},
		{name: "run rm and keep", args: runArgs("--rm", "--keep"), wantStderr: "mutually exclusive"},
		{name: "run bad cpus", args: runArgs("--cpus", "x"), wantStderr: "invalid value"},
		{name: "run negative cpus", args: runArgs("--cpus", "-1"), wantStderr: "must be positive"},
		{name: "run command", args: runArgs("bash"), wantStderr: "takes no command"},
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
			require.Contains(t, stderr.String(), tt.wantStderr)
		})
	}
}

func TestRunHelp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "flag", args: []string{"-h"}, want: "Usage: cove <command>"},
		{name: "command", args: []string{"help"}, want: "Usage: cove <command>"},
		{name: "run flag", args: runArgs("-h"), want: "Usage: cove run"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			app := &cli.App{Stdout: &stdout, Stderr: &stderr}
			code := app.Run(tt.args)

			require.Equal(t, 0, code)
			require.Contains(t, stdout.String(), tt.want)
			require.Empty(t, stderr.String())
		})
	}
}

func TestParseRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want sandbox.Spec
	}{
		{name: "no flags", args: nil, want: sandbox.Spec{}},
		{name: "rm is the default", args: []string{"--rm"}, want: sandbox.Spec{}},
		{
			name: "every flag",
			args: []string{"--name", "demo", "--keep", "--cpus", "2", "-m", "4G", "-e", "FOO=bar", "-e", "TERM"},
			want: sandbox.Spec{Name: "demo", Keep: true, CPUs: 2, Memory: "4G", Env: []string{"FOO=bar", "TERM"}},
		},
		{
			name: "long forms",
			args: []string{"--memory=4G", "--env=BAR=baz"},
			want: sandbox.Spec{Memory: "4G", Env: []string{"BAR=baz"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec, err := cli.ParseRun(tt.args)

			require.NoError(t, err)
			require.Equal(t, tt.want, spec)
		})
	}
}
