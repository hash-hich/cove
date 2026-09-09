package cli_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/cli"
	"gitlab.com/hich-hich/cove/internal/sandbox"
)

const (
	// demo is the sandbox the tests name.
	demo = "demo"
	// unknownFlag is what flag reports for a flag no command defines.
	unknownFlag = "not defined: -bogus"
	// listUsage heads the help of list, under which its aliases answer too.
	listUsage = "Usage: cove list"
	// longForms names the case where every flag is given in its long form.
	longForms = "long forms"
	// table is the default output format of list.
	table = "table"
	// review is the display name the send tests give a thread.
	review = "review"
)

// runArgs prefixes args with the run command.
func runArgs(args ...string) []string {
	return append([]string{"run"}, args...)
}

// listArgs prefixes args with the list command.
func listArgs(args ...string) []string {
	return append([]string{"list"}, args...)
}

// stopArgs prefixes args with the stop command.
func stopArgs(args ...string) []string {
	return append([]string{"stop"}, args...)
}

// sendArgs prefixes args with the send command.
func sendArgs(args ...string) []string {
	return append([]string{"send"}, args...)
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
		{name: "run unknown flag", args: runArgs("--bogus"), wantStderr: unknownFlag},
		{name: "run docker session flags", args: runArgs("-it"), wantStderr: "not defined: -it"},
		{name: "run rm and keep", args: runArgs("--rm", "--keep"), wantStderr: "mutually exclusive"},
		{name: "run bad cpus", args: runArgs("--cpus", "x"), wantStderr: "invalid value"},
		{name: "run negative cpus", args: runArgs("--cpus", "-1"), wantStderr: "must be positive"},
		{name: "run command", args: runArgs("bash"), wantStderr: "takes no command"},
		{name: "stop no target", args: stopArgs(), wantStderr: "requires at least 1 argument"},
		{name: "stop all with target", args: stopArgs("--all", demo), wantStderr: "takes no target"},
		{name: "stop unknown flag", args: stopArgs("--bogus", demo), wantStderr: unknownFlag},
		{name: "stop podman ignore", args: stopArgs("-i", demo), wantStderr: "not defined: -i"},
		{name: "stop bad time", args: stopArgs("-t", "x", demo), wantStderr: "invalid value"},
		{name: "stop negative time", args: stopArgs("-t", "-1", demo), wantStderr: "must be positive"},
		{name: "list unknown flag", args: listArgs("--bogus"), wantStderr: unknownFlag},
		{name: "list unknown format", args: listArgs("--format", "yaml"), wantStderr: "must be table or json"},
		{name: "list docker filter", args: listArgs("--filter", "label=cove"), wantStderr: "not defined: -filter"},
		{name: "list target", args: listArgs(demo), wantStderr: "takes no argument"},
		{name: "send no target", args: sendArgs(), wantStderr: "requires at least 1 argument"},
		{name: "send two prompts", args: sendArgs(demo, "a", "b"), wantStderr: "takes one prompt"},
		{name: "send empty prompt", args: sendArgs(demo, ""), wantStderr: "the prompt is empty"},
		{name: "send empty resume", args: sendArgs("-r", "", demo), wantStderr: "--resume requires a thread"},
		{name: "send continue driven", args: sendArgs("-c", demo, "go on"), wantStderr: "--continue takes no prompt"},
		{name: "send continue and resume", args: sendArgs("-c", "-r", review, demo), wantStderr: "mutually exclusive"},
		{name: "send unknown flag", args: sendArgs("--bogus", demo), wantStderr: unknownFlag},
		{name: "send docker detach", args: sendArgs("-d", demo), wantStderr: "not defined: -d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			app := &cli.App{Stdout: &stdout, Stderr: &stderr}
			code := app.Run(tt.args)

			require.Equal(t, cli.ExitUsage, code)
			require.Empty(t, stdout.String())
			require.Contains(t, stderr.String(), tt.wantStderr)
			// A bare cove gets the usage. An error gets the way to it: the usage would drown the
			// error, which is what the caller must read.
			if len(tt.args) == 0 {
				require.Contains(t, stderr.String(), "Usage: cove")
			} else {
				require.Contains(t, stderr.String(), "--help'.")
				require.NotContains(t, stderr.String(), "Usage:")
			}
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
		{name: "stop flag", args: stopArgs("-h"), want: "Usage: cove stop"},
		{name: "send flag", args: sendArgs("-h"), want: "Usage: cove send"},
		{name: "list flag", args: listArgs("-h"), want: listUsage},
		{name: "ls alias", args: []string{"ls", "-h"}, want: listUsage},
		{name: "ps alias", args: []string{"ps", "-h"}, want: listUsage},
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
			args: []string{"--name", demo, "--keep", "--cpus", "2", "-m", "4G", "-e", "FOO=bar", "-e", "TERM"},
			want: sandbox.Spec{Name: demo, Keep: true, CPUs: 2, Memory: "4G", Env: []string{"FOO=bar", "TERM"}},
		},
		{
			name: longForms,
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

func TestParseStop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    sandbox.StopSpec
		wantAll bool
	}{
		{name: "one target", args: []string{demo}, want: sandbox.StopSpec{Targets: []string{demo}}},
		{name: "several targets", args: []string{"a", "b"}, want: sandbox.StopSpec{Targets: []string{"a", "b"}}},
		{name: "all", args: []string{"--all"}, want: sandbox.StopSpec{}, wantAll: true},
		{name: "all short", args: []string{"-a"}, want: sandbox.StopSpec{}, wantAll: true},
		{
			name: "every option",
			args: []string{"-s", "SIGKILL", "-t", "30", demo},
			want: sandbox.StopSpec{Targets: []string{demo}, Signal: "SIGKILL", Timeout: new(30)},
		},
		{
			name: longForms,
			args: []string{"--signal=SIGINT", "--time=0", demo},
			want: sandbox.StopSpec{Targets: []string{demo}, Signal: "SIGINT", Timeout: new(0)},
		},
		{name: "time left to container", args: []string{demo}, want: sandbox.StopSpec{Targets: []string{demo}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec, all, err := cli.ParseStop(tt.args)

			require.NoError(t, err)
			require.Equal(t, tt.want, spec)
			require.Equal(t, tt.wantAll, all)
		})
	}
}

func TestParseSend(t *testing.T) {
	t.Parallel()

	const thread = "0b7f1a3c-9e42-4d51-8a6b-2f0c5d7e1934"

	tests := []struct {
		name string
		args []string
		want sandbox.SendSpec
	}{
		{name: "attached", args: []string{demo}, want: sandbox.SendSpec{Target: demo}},
		{
			name: "driven",
			args: []string{demo, "fix the build"},
			want: sandbox.SendSpec{Target: demo, Prompt: "fix the build"},
		},
		{
			name: "resume",
			args: []string{"-r", thread, demo, "go on"},
			want: sandbox.SendSpec{Target: demo, Prompt: "go on", Thread: thread, Resume: true},
		},
		{
			name: longForms,
			args: []string{"--resume=" + review, "--name=ignored", demo},
			want: sandbox.SendSpec{Target: demo, Thread: review, Resume: true, Name: "ignored"},
		},
		{name: "name", args: []string{"-n", review, demo}, want: sandbox.SendSpec{Target: demo, Name: review}},
		{name: "continue", args: []string{"-c", demo}, want: sandbox.SendSpec{Target: demo, Continue: true}},
		{
			name: "continue, long form and named",
			args: []string{"--continue", "--name", review, demo},
			want: sandbox.SendSpec{Target: demo, Continue: true, Name: review},
		},
		{
			name: "a prompt is not parsed for flags",
			args: []string{demo, "--help me"},
			want: sandbox.SendSpec{Target: demo, Prompt: "--help me"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec, err := cli.ParseSend(tt.args)

			require.NoError(t, err)
			require.Equal(t, tt.want, spec)
		})
	}
}

func TestParseList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want cli.ListOptions
	}{
		{name: "no flags", args: nil, want: cli.ListOptions{Format: table}},
		{name: "all", args: []string{"-a"}, want: cli.ListOptions{All: true, Format: table}},
		{name: "quiet", args: []string{"-q"}, want: cli.ListOptions{Quiet: true, Format: table}},
		{
			name: longForms,
			args: []string{"--all", "--quiet", "--format=json"},
			want: cli.ListOptions{All: true, Quiet: true, Format: "json"},
		},
		{name: "table is explicit too", args: []string{"--format", table}, want: cli.ListOptions{Format: table}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := cli.ParseList(tt.args)

			require.NoError(t, err)
			require.Equal(t, tt.want, opts)
		})
	}
}
