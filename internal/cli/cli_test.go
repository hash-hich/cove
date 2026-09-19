package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/agent"
	"gitlab.com/hich-hich/cove/internal/cli"
	"gitlab.com/hich-hich/cove/internal/image"
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
	// goImage is the profile the run tests name.
	goImage = "cove-go:local"
	// remote is the image the pull tests name, on its registry.
	remote = "ghcr.io/org/repo:tag"
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

// pullArgs prefixes args with the pull command.
func pullArgs(args ...string) []string {
	return append([]string{"pull"}, args...)
}

// refused names the command a usage error is about: the verb when args name one, cove itself
// otherwise, which is what its shape is shown for.
func refused(args []string) string {
	switch args[0] {
	case "list", "pull", "run", "send", "stop":
		return "cove " + args[0]
	default:
		return "cove"
	}
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
		{name: "run empty image", args: runArgs("--image", "", repo), wantStderr: "--image must name an image"},
		{name: "run negative cpus", args: runArgs("--cpus", "-1", repo), wantStderr: "must be positive"},
		{name: "run no url", args: runArgs(), wantStderr: "requires 1 argument"},
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
		{name: "pull no reference", args: pullArgs(), wantStderr: "requires 1 argument"},
		{name: "pull two references", args: pullArgs(remote, "ghcr.io/org/other:tag"), wantStderr: "one reference"},
		{name: "pull unknown flag", args: pullArgs("--bogus", remote), wantStderr: unknownFlag},
		{
			name:       "pull docker platform",
			args:       pullArgs("--platform", "linux/amd64", remote),
			wantStderr: "not defined: -platform",
		},
		{name: "pull docker all tags", args: pullArgs("-a", remote), wantStderr: "not defined: -a"},
		{name: "pull bare name", args: pullArgs(demo), wantStderr: "names no registry"},
		{name: "pull docker.io implied", args: pullArgs("org/repo:tag"), wantStderr: "names no registry"},
		{name: "pull malformed", args: pullArgs("ghcr.io/Org/repo:tag"), wantStderr: "invalid reference"},
		{name: "stop shows both its shapes", args: stopArgs(), wantStderr: "Usage: cove stop [OPTIONS] SANDBOX" +
			" [SANDBOX...]\n       cove stop --all\n"},
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
			// A bare cove gets the whole usage. An error gets the message, the shapes of the
			// command it names and the way to the help: the options and the prose would drown
			// the message, which is what the caller must read.
			if len(tt.args) == 0 {
				require.Contains(t, stderr.String(), "Usage: cove <command>")
				require.Contains(t, stderr.String(), "Commands:")
				return
			}
			require.Contains(t, stderr.String(), "\nUsage: "+refused(tt.args)+" ")
			require.Contains(t, stderr.String(), "See '"+refused(tt.args)+" --help'.\n")
			require.NotContains(t, stderr.String(), "Options:")
			require.NotContains(t, stderr.String(), "Exit codes:")
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
		{name: "pull flag", args: pullArgs("-h"), want: "Usage: cove pull"},
		{name: "pull listed", args: []string{"help"}, want: "pull    Pull an image"},
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

	// base is what run parses when no flag is given: the default image and nothing else.
	base := cli.SandboxSpec{Image: image.DefaultImage}
	tests := []struct {
		name string
		args []string
		want cli.RunOptions
	}{
		{name: "url only", args: []string{repo}, want: cli.RunOptions{Spec: base, URL: repo}},
		{name: "rm is the default", args: []string{"--rm", repo}, want: cli.RunOptions{Spec: base, URL: repo}},
		{
			name: "every flag",
			args: []string{
				"-b", fix, "--name", demo, "--image", goImage, "--keep", "--cpus", "2", "-m", "4G",
				"-e", "FOO=bar", "-e", "TERM", repo,
			},
			want: cli.RunOptions{
				Spec: cli.SandboxSpec{
					Image: goImage, Name: demo, Keep: true, CPUs: 2, Memory: "4G", Env: []string{"FOO=bar", "TERM"}, Branch: fix,
				},
				URL: repo,
			},
		},
		{
			name: longForms,
			args: []string{"--branch=fix", "--image=" + goImage, "--memory=4G", "--env=BAR=baz", repo},
			want: cli.RunOptions{
				Spec: cli.SandboxSpec{Image: goImage, Memory: "4G", Env: []string{"BAR=baz"}, Branch: fix},
				URL:  repo,
			},
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
		want    cli.StopSpec
		wantAll bool
	}{
		{name: "one target", args: []string{demo}, want: cli.StopSpec{Targets: []string{demo}}},
		{name: "several targets", args: []string{"a", "b"}, want: cli.StopSpec{Targets: []string{"a", "b"}}},
		{name: "all", args: []string{"--all"}, want: cli.StopSpec{}, wantAll: true},
		{name: "all short", args: []string{"-a"}, want: cli.StopSpec{}, wantAll: true},
		{
			name: "every option",
			args: []string{"-s", "SIGKILL", "-t", "30", demo},
			want: cli.StopSpec{Targets: []string{demo}, Signal: "SIGKILL", Timeout: new(30)},
		},
		{
			name: longForms,
			args: []string{"--signal=SIGINT", "--time=0", demo},
			want: cli.StopSpec{Targets: []string{demo}, Signal: "SIGINT", Timeout: new(0)},
		},
		{name: "time left to the backend", args: []string{demo}, want: cli.StopSpec{Targets: []string{demo}}},
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
		want agent.Turn
	}{
		{name: "attached", args: []string{demo}, want: agent.Turn{Target: demo}},
		{
			name: "driven",
			args: []string{demo, "fix the build"},
			want: agent.Turn{Target: demo, Prompt: "fix the build"},
		},
		{
			name: "resume",
			args: []string{"-r", thread, demo, "go on"},
			want: agent.Turn{Target: demo, Prompt: "go on", Thread: thread, Resume: true},
		},
		{
			name: longForms,
			args: []string{"--resume=" + review, "--name=ignored", demo},
			want: agent.Turn{Target: demo, Thread: review, Resume: true, Name: "ignored"},
		},
		{name: "name", args: []string{"-n", review, demo}, want: agent.Turn{Target: demo, Name: review}},
		{name: "continue", args: []string{"-c", demo}, want: agent.Turn{Target: demo, Continue: true}},
		{
			name: "continue, long form and named",
			args: []string{"--continue", "--name", review, demo},
			want: agent.Turn{Target: demo, Continue: true, Name: review},
		},
		{
			name: "a prompt is not parsed for flags",
			args: []string{demo, "--help me"},
			want: agent.Turn{Target: demo, Prompt: "--help me"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			turn, err := cli.ParseSend(tt.args)

			require.NoError(t, err)
			require.Equal(t, tt.want, turn)
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

func TestVerbsWithoutABackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "run creates nothing", args: runArgs(repo)},
		{name: "send attached", args: sendArgs(demo)},
		{name: "send driven", args: sendArgs(demo, "fix the ci")},
		{name: "stop one target", args: stopArgs(demo)},
		{name: "stop all", args: stopArgs("--all")},
		{name: "list what runs", args: listArgs()},
		{name: "ls alias", args: []string{"ls"}},
		{name: "ps alias", args: []string{"ps"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			app := &cli.App{Stdout: &stdout, Stderr: &stderr}

			code := app.Run(tt.args)

			// The arguments were valid, so no usage is shown: the caller learns that the command
			// is well formed and, separately, that cove has nothing to run it with.
			require.Equal(t, cli.ExitPreflight, code)
			require.Empty(t, stdout.String())
			require.Contains(t, stderr.String(), "not implemented yet")
			require.NotContains(t, stderr.String(), "Usage:")
		})
	}
}

func TestParsePull(t *testing.T) {
	t.Parallel()

	const digest = "ghcr.io/org/repo@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name    string
		args    []string
		wantRef string
		want    cli.PullOptions
	}{
		{name: "by tag", args: []string{remote}, wantRef: remote},
		{name: "tag left out", args: []string{"ghcr.io/org/repo"}, wantRef: "ghcr.io/org/repo:latest"},
		{name: "by digest", args: []string{digest}, wantRef: digest},
		{name: "quiet", args: []string{"-q", remote}, wantRef: remote, want: cli.PullOptions{Quiet: true}},
		{name: "json", args: []string{"--json", remote}, wantRef: remote, want: cli.PullOptions{JSON: true}},
		{
			name:    longForms,
			args:    []string{"--quiet", "--json", remote},
			wantRef: remote,
			want:    cli.PullOptions{Quiet: true, JSON: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts, err := cli.ParsePull(tt.args)

			require.NoError(t, err)
			require.Equal(t, tt.wantRef, opts.Ref.Name())
			opts.Ref = nil
			require.Equal(t, tt.want, opts)
		})
	}
}

func TestPullFailsWhenTheRegistryCannotBeReached(t *testing.T) {
	// The store goes to a fresh cache directory; port 1 answers nothing on the loopback, so no
	// request leaves the machine.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	const unreachable = "127.0.0.1:1/org/repo:tag"

	tests := []struct {
		args []string
		// wantLines counts the lines of stderr: the facts and the reason, or the reason alone.
		wantLines int
	}{
		{args: pullArgs(unreachable), wantLines: 2},
		{args: pullArgs("--json", unreachable), wantLines: 1},
		{args: pullArgs("-q", unreachable), wantLines: 1},
	}

	for _, tt := range tests {
		var stdout, stderr bytes.Buffer
		app := &cli.App{Stdout: &stdout, Stderr: &stderr}

		code := app.Run(tt.args)

		require.Equal(t, 1, code, tt.args)
		require.Empty(t, stdout.String())
		lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
		require.Len(t, lines, tt.wantLines, stderr.String())
		require.Contains(t, lines[len(lines)-1], "cove pull: 127.0.0.1:1: ")
	}
}
