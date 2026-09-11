package cli_test

import (
	"bytes"
	"io"
	"os"
	"strings"
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
	// repo and fix are the repository and the branch the run tests name.
	repo = "https://forge.example/group/repo.git"
	fix  = "fix"
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
		{name: "run unknown flag", args: runArgs("--bogus", repo), wantStderr: unknownFlag},
		{name: "run docker session flags", args: runArgs("-it", repo), wantStderr: "not defined: -it"},
		{name: "run rm and keep", args: runArgs("--rm", "--keep", repo), wantStderr: "mutually exclusive"},
		{name: "run bad cpus", args: runArgs("--cpus", "x", repo), wantStderr: "invalid value"},
		{name: "run negative cpus", args: runArgs("--cpus", "-1", repo), wantStderr: "must be positive"},
		{name: "run no url", args: runArgs(), wantStderr: "requires 1 argument"},
		{name: "run branch with =", args: runArgs("-b", "x=1", repo), wantStderr: "cannot label the branch"},
		{name: "run command", args: runArgs(repo, "bash"), wantStderr: "takes no command"},
		{name: "run git clone depth", args: runArgs("--depth", "1", repo), wantStderr: "not defined: -depth"},
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
		want cli.RunOptions
	}{
		{name: "url only", args: []string{repo}, want: cli.RunOptions{URL: repo}},
		{name: "rm is the default", args: []string{"--rm", repo}, want: cli.RunOptions{URL: repo}},
		{
			name: "every flag",
			args: []string{
				"-b", fix, "--name", demo, "--keep", "--cpus", "2", "-m", "4G", "-e", "FOO=bar", "-e", "TERM", repo,
			},
			want: cli.RunOptions{
				Spec: sandbox.Spec{Name: demo, Keep: true, CPUs: 2, Memory: "4G", Env: []string{"FOO=bar", "TERM"}, Branch: fix},
				URL:  repo,
			},
		},
		{
			name: longForms,
			args: []string{"--branch=fix", "--memory=4G", "--env=BAR=baz", repo},
			want: cli.RunOptions{Spec: sandbox.Spec{Memory: "4G", Env: []string{"BAR=baz"}, Branch: fix}, URL: repo},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := cli.ParseRun(tt.args)

			require.NoError(t, err)
			require.Equal(t, tt.want, opts)
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

func TestAnnounced(t *testing.T) {
	t.Parallel()

	// A character device stands for the terminal a test has no way to open.
	tty, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tty.Close()) })
	running := []sandbox.VM{{ID: demo, State: "running", Labels: map[string]string{sandbox.LabelKey: sandbox.LabelValue}}}
	stopped := []sandbox.VM{{ID: demo, State: "stopped", Labels: map[string]string{sandbox.LabelKey: sandbox.LabelValue}}}

	tests := []struct {
		name  string
		stdin io.Reader
		spec  sandbox.SendSpec
		vms   []sandbox.VM
		want  bool
	}{
		{name: "attached to a running sandbox", stdin: tty, spec: sandbox.SendSpec{Target: demo}, vms: running, want: true},
		{
			name:  "driven",
			stdin: tty,
			spec:  sandbox.SendSpec{Target: demo, Prompt: "go"},
			vms:   running,
		},
		{name: "stopped sandbox", stdin: tty, spec: sandbox.SendSpec{Target: demo}, vms: stopped},
		{name: "unknown sandbox", stdin: tty, spec: sandbox.SendSpec{Target: "other"}, vms: running},
		{name: "stdin is not a terminal", stdin: strings.NewReader(""), spec: sandbox.SendSpec{Target: demo}, vms: running},
		{name: "no stdin at all", spec: sandbox.SendSpec{Target: demo}, vms: running},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := &cli.App{Stdin: tt.stdin, Stdout: io.Discard, Stderr: io.Discard}

			require.Equal(t, tt.want, cli.Announced(app, tt.spec, tt.vms))
		})
	}
}
