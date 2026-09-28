package cli_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/agent"
	"gitlab.com/hich-hich/cove/internal/cli"
	"gitlab.com/hich-hich/cove/internal/image"
	"gitlab.com/hich-hich/cove/internal/rwdisk"
	"gitlab.com/hich-hich/cove/internal/sandbox"
	"gitlab.com/hich-hich/cove/internal/vminit/control"
	"gitlab.com/hich-hich/cove/internal/vminit/turn"
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
	// table is the default output format of list, jsonFormat the other.
	table      = "table"
	jsonFormat = "json"
	// jsonFlag asks pull and list for their json output.
	jsonFlag = "--json"
	// review is the display name the send tests give a thread.
	review = "review"
	// repo and fix are the repository and the branch the run tests name.
	repo = "https://forge.example/group/repo.git"
	fix  = "fix"
	// cpusRange is what run says of a number of vCPUs it refuses.
	cpusRange = "takes 1 to 255 vCPUs"
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
		{name: "run keep", args: runArgs("--keep", repo), wantStderr: "not defined: -keep"},
		{name: "run disk off the list", args: runArgs("--disk", "10g", repo), wantStderr: "8g, 16g, 32g, 64g"},
		{name: "run bad cpus", args: runArgs("--cpus", "x", repo), wantStderr: "invalid value"},
		{name: "run empty image", args: runArgs("--image", "", repo), wantStderr: "--image must name an image"},
		{name: "run negative cpus", args: runArgs("--cpus", "-1", repo), wantStderr: cpusRange},
		{name: "run no cpus", args: runArgs("--cpus", "0", repo), wantStderr: cpusRange},
		{name: "run too many cpus", args: runArgs("--cpus", "256", repo), wantStderr: cpusRange},
		{name: "run memory unit", args: runArgs("-m", "4t", repo), wantStderr: "a size such as 512m"},
		{name: "run memory zero", args: runArgs("-m", "0", repo), wantStderr: "a size such as 512m"},
		{name: "run memory in bytes", args: runArgs("-m", "1000000", repo), wantStderr: "a whole number of MiB"},
		{name: "run memory too big", args: runArgs("-m", "4194304g", repo), wantStderr: "a whole number of MiB"},
		{name: "run bad name", args: runArgs("--name", "-x", repo), wantStderr: "--name takes"},
		{name: "run long name", args: runArgs("--name", strings.Repeat("x", 65), repo), wantStderr: "64 characters at most"},
		{name: "run no url", args: runArgs(), wantStderr: "requires 1 argument"},
		{name: "run command", args: runArgs(repo, "bash"), wantStderr: "takes no command"},
		{name: "run git clone depth", args: runArgs("--depth", "1", repo), wantStderr: "not defined: -depth"},
		{name: "stop no target", args: stopArgs(), wantStderr: "requires at least 1 argument"},
		{name: "stop all with target", args: stopArgs("--all", demo), wantStderr: "takes no target"},
		{name: "stop unknown flag", args: stopArgs("--bogus", demo), wantStderr: unknownFlag},
		{name: "stop podman ignore", args: stopArgs("-i", demo), wantStderr: "not defined: -i"},
		{name: "stop signal", args: stopArgs("-s", "SIGKILL", demo), wantStderr: "not defined: -s"},
		{name: "stop bad time", args: stopArgs("-t", "x", demo), wantStderr: "invalid value"},
		{name: "stop negative time", args: stopArgs("-t", "-1", demo), wantStderr: "must be positive"},
		{name: "list unknown flag", args: listArgs("--bogus"), wantStderr: unknownFlag},
		{name: "list unknown format", args: listArgs("--format", "yaml"), wantStderr: "must be table or json"},
		{name: "list docker filter", args: listArgs("--filter", "label=cove"), wantStderr: "not defined: -filter"},
		{name: "list target", args: listArgs(demo), wantStderr: "takes no argument"},
		{name: "list docker all", args: listArgs("-a"), wantStderr: "not defined: -a"},
		{name: "list two formats", args: listArgs(jsonFlag, "--format", table), wantStderr: "two formats"},
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
	base := cli.SandboxSpec{Image: image.DefaultImage, CPUs: 2, MemoryMiB: 2048, Disk: rwdisk.DefaultCap}
	removed := base
	removed.Remove = true
	tests := []struct {
		name string
		args []string
		want cli.RunOptions
	}{
		{name: "url only", args: []string{repo}, want: cli.RunOptions{Spec: base, URL: repo}},
		{name: "rm", args: []string{"--rm", repo}, want: cli.RunOptions{Spec: removed, URL: repo}},
		{
			name: "every flag",
			args: []string{
				"-b", fix, "--name", demo, "--image", goImage, "--rm", "--cpus", "4", "-m", "4G", "--disk", "256g",
				"-e", "FOO=bar", "-e", "TERM", repo,
			},
			want: cli.RunOptions{
				Spec: cli.SandboxSpec{
					Image: goImage, Name: demo, CPUs: 4, MemoryMiB: 4096, Disk: 256 << 30,
					Env: []string{"FOO=bar", "TERM"}, Branch: fix, Remove: true,
				},
				URL: repo,
			},
		},
		{
			name: longForms,
			args: []string{"--branch=fix", "--image=" + goImage, "--memory=512m", "--disk=64G", "--env=BAR=baz", repo},
			want: cli.RunOptions{
				Spec: cli.SandboxSpec{
					Image: goImage, CPUs: 2, MemoryMiB: 512, Disk: rwdisk.DefaultCap, Env: []string{"BAR=baz"},
					Branch: fix,
				},
				URL: repo,
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

	const stopDefault = 15 * time.Second

	tests := []struct {
		name    string
		args    []string
		want    cli.StopSpec
		wantAll bool
	}{
		{name: "one target", args: []string{demo}, want: cli.StopSpec{Targets: []string{demo}, Timeout: stopDefault}},
		{
			name: "several targets", args: []string{"a", "b"},
			want: cli.StopSpec{Targets: []string{"a", "b"}, Timeout: stopDefault},
		},
		{name: "all", args: []string{"--all"}, want: cli.StopSpec{Timeout: stopDefault}, wantAll: true},
		{name: "all short", args: []string{"-a"}, want: cli.StopSpec{Timeout: stopDefault}, wantAll: true},
		{
			name: "time", args: []string{"-t", "30", demo},
			want: cli.StopSpec{Targets: []string{demo}, Timeout: 30 * time.Second},
		},
		{name: longForms, args: []string{"--time=0", demo}, want: cli.StopSpec{Targets: []string{demo}}},
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
		{name: "quiet", args: []string{"-q"}, want: cli.ListOptions{Quiet: true, Format: table}},
		{
			name: longForms,
			args: []string{"--quiet", "--format=json"},
			want: cli.ListOptions{Quiet: true, Format: jsonFormat},
		},
		{name: "table is explicit too", args: []string{"--format", table}, want: cli.ListOptions{Format: table}},
		{name: "json alias", args: []string{jsonFlag}, want: cli.ListOptions{Format: jsonFormat}},
		{
			name: "json alias agrees with format",
			args: []string{"--format", jsonFormat, jsonFlag},
			want: cli.ListOptions{Format: jsonFormat},
		},
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

func TestSendToAnUnknownSandbox(t *testing.T) {
	// The inventory is a fresh directory, on macOS under $HOME and on Linux under $XDG_STATE_HOME.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	for _, args := range [][]string{sendArgs(demo), sendArgs(demo, "fix the ci")} {
		var stdout, stderr bytes.Buffer
		app := &cli.App{Stdout: &stdout, Stderr: &stderr}

		code := app.Run(args)

		// The arguments were valid, so no usage is shown: the caller learns that the command is
		// well formed and, separately, that there is no sandbox to run it in.
		require.Equal(t, cli.ExitPreflight, code, args)
		require.Empty(t, stdout.String())
		require.Contains(t, stderr.String(), "no such sandbox")
		require.NotContains(t, stderr.String(), "Usage:")
	}
}

func TestSendExitCode(t *testing.T) {
	t.Parallel()

	code := func(c int) *int { return &c }
	tests := []struct {
		name       string
		exit       turn.Exit
		err        error
		want       int
		wantStderr string
	}{
		{name: "the agent exited", exit: turn.Exit{Cause: turn.CauseAgent, Code: code(3)}, want: 3},
		{name: "the agent succeeded", exit: turn.Exit{Cause: turn.CauseAgent, Code: code(0)}, want: 0},
		{name: "the agent died of a signal", exit: turn.Exit{Cause: turn.CauseAgent, Signal: 11}, want: 139},
		{
			name: "the agent was not found",
			err:  &turn.ExecError{Errno: turn.ErrnoNotFound, Message: "claude not found in PATH"},
			want: 127, wantStderr: "could not be run: claude not found",
		},
		{
			name: "the agent could not be executed",
			err:  &turn.ExecError{Errno: 13, Message: "permission denied"},
			want: 126, wantStderr: "could not be run",
		},
		{
			name: "the init failed before execve",
			err:  &turn.ExecError{Message: "the VM is stopping"},
			want: cli.ExitPreflight, wantStderr: "the VM is stopping",
		},
		// The cause wins over the signal: a stop hangs an attached agent up, which dies of SIGHUP.
		{name: "stopped", exit: turn.Exit{Cause: turn.CauseStop, Signal: 1}, want: 143, wantStderr: "stopped"},
		{name: "cancelled", exit: turn.Exit{Cause: turn.CauseCancel, Signal: 9}, want: 130, wantStderr: "cancelled"},
		{name: "timed out", exit: turn.Exit{Cause: turn.CauseTimeout, Code: code(0)}, want: 124, wantStderr: "time was up"},
		{name: "abandoned", err: cli.ErrAbandoned, want: 130, wantStderr: "abandoned"},
		{name: "lost", err: errors.New("the connection to the turn ended"), want: cli.ExitPreflight, wantStderr: "ended"},
		{name: "unknown cause", exit: turn.Exit{Cause: "flood"}, want: cli.ExitPreflight, wantStderr: "flood"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			require.Equal(t, tt.want, cli.SendExitCode(&stderr, tt.exit, tt.err))
			if tt.wantStderr == "" {
				require.Empty(t, stderr.String(), "the agent ended the turn, cove has nothing to add")
			} else {
				require.Contains(t, stderr.String(), tt.wantStderr)
			}
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
		{name: "json", args: []string{jsonFlag, remote}, wantRef: remote, want: cli.PullOptions{JSON: true}},
		{
			name:    longForms,
			args:    []string{"--quiet", jsonFlag, remote},
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

	// A pull that never reached the registry says the reason and nothing else: the line that
	// opens a pull waits for the manifest, which never came.
	for _, args := range [][]string{
		pullArgs(unreachable), pullArgs(jsonFlag, unreachable), pullArgs("-q", unreachable),
	} {
		var stdout, stderr bytes.Buffer
		app := &cli.App{Stdout: &stdout, Stderr: &stderr}

		code := app.Run(args)

		require.Equal(t, 1, code, args)
		require.Empty(t, stdout.String())
		lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
		require.Len(t, lines, 1, stderr.String())
		require.Contains(t, lines[0], "cove pull: 127.0.0.1:1: ")
	}
}

func TestStopReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		st   sandbox.Stopped
		want []string
	}{
		{name: "the init carried it out", st: sandbox.Stopped{Steps: []control.Step{
			{Name: control.Received}, {Name: control.ProcessesEnded}, {Name: control.Synced}, {Name: control.ReadOnly},
		}}},
		{
			name: "a step failed",
			st:   sandbox.Stopped{Steps: []control.Step{{Name: control.Received}, {Name: control.ReadOnly, Error: "busy"}}},
			want: []string{"write disk read only failed: busy"},
		},
		{
			name: "killed at work",
			st: sandbox.Stopped{
				Killed: errors.New("the VM did not end within 15s"),
				Steps:  []control.Step{{Name: control.Received}},
			},
			want: []string{"cove-vmm killed: the VM did not end within 15s; the init had reached: received"},
		},
		{
			name: "killed unreached",
			st:   sandbox.Stopped{Killed: errors.New("the init could not be reached")},
			want: []string{"cove-vmm killed: the init could not be reached; the init reported no step"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, cli.StopReport(tt.st))
		})
	}
}
