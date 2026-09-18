package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/cli"
)

const (
	// unknownFlag is what flag reports for a flag no command defines.
	unknownFlag = "not defined: -bogus"
	// longForms names the case where every flag is given in its long form.
	longForms = "long forms"
	// remote is the image the pull tests name, on its registry.
	remote = "ghcr.io/org/repo:tag"
	// bare is a reference naming no registry, which pull refuses.
	bare = "demo"
	// help is the command that prints the usage of cove on stdout.
	help = "help"
)

// pullArgs prefixes args with the pull command.
func pullArgs(args ...string) []string {
	return append([]string{"pull"}, args...)
}

// refused names the command a usage error is about: the verb when args name one, cove itself
// otherwise, which is what its shape is shown for.
func refused(args []string) string {
	if args[0] == "pull" {
		return "cove pull"
	}
	return "cove"
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
		// The verbs that drove the container CLI are gone; they are unknown commands like any
		// other until a backend brings them back.
		{name: "run is gone", args: []string{"run", "https://forge.example/group/repo.git"}},
		{name: "send is gone", args: []string{"send", bare}},
		{name: "stop is gone", args: []string{"stop", bare}},
		{name: "list is gone", args: []string{"list"}},
		{name: "pull no reference", args: pullArgs(), wantStderr: "requires 1 argument"},
		{name: "pull two references", args: pullArgs(remote, "ghcr.io/org/other:tag"), wantStderr: "one reference"},
		{name: "pull unknown flag", args: pullArgs("--bogus", remote), wantStderr: unknownFlag},
		{
			name:       "pull docker platform",
			args:       pullArgs("--platform", "linux/amd64", remote),
			wantStderr: "not defined: -platform",
		},
		{name: "pull docker all tags", args: pullArgs("-a", remote), wantStderr: "not defined: -a"},
		{name: "pull bare name", args: pullArgs(bare), wantStderr: "names no registry"},
		{name: "pull docker.io implied", args: pullArgs("org/repo:tag"), wantStderr: "names no registry"},
		{name: "pull malformed", args: pullArgs("ghcr.io/Org/repo:tag"), wantStderr: "invalid reference"},
		{name: "pull shows its shape", args: pullArgs(), wantStderr: "Usage: cove pull [OPTIONS] REFERENCE\n"},
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
		{name: "command", args: []string{help}, want: "Usage: cove <command>"},
		{name: "pull flag", args: pullArgs("-h"), want: "Usage: cove pull"},
		{name: "pull listed", args: []string{help}, want: "pull    Pull an image"},
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

func TestHelpListsOnlyWhatRuns(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	app := &cli.App{Stdout: &stdout, Stderr: &stderr}

	require.Equal(t, 0, app.Run([]string{help}))
	require.Empty(t, stderr.String())
	// The usage is the surface cove promises; it must not name a verb whose backend left.
	for _, gone := range []string{"run", "send", "stop", "list", "sandbox"} {
		require.NotContains(t, stdout.String(), gone)
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
