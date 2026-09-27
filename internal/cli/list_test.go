package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/cli"
	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// listedRepo is the repository of the sandboxes the list tests print, short for a table to fit.
const listedRepo = "https://git.example/r"

// listed are the sandboxes the list tests print: one that runs, one stopped whose disk is gone,
// one whose record could not be read, and one older than the inventory.
func listed(now time.Time) []cli.Listed {
	used := int64(1288490189)
	return []cli.Listed{
		{
			Entry: inventory.Entry{
				Record: inventory.Record{
					ID: "0123456789abcdef", Name: demo, Image: goImage, Digest: "sha256:aa",
					Repository: listedRepo, Branch: fix, Created: now.Add(-5 * time.Minute),
					CPUs: 4, MemoryMiB: 512, Disk: 64 << 30,
				},
				Dir: "/state/0123456789abcdef", State: inventory.Running,
			},
			DiskUsed: &used,
		},
		{
			Entry: inventory.Entry{
				Record: inventory.Record{
					ID: "fedcba9876543210", Name: "cove-fedcba987654", Image: "cove-sandbox:local",
					Digest: "sha256:bb", Repository: listedRepo, Created: now.Add(-3 * time.Hour),
					CPUs: 2, MemoryMiB: 2048, Disk: 1 << 40,
				},
				Dir: "/state/fedcba9876543210", State: inventory.Stopped,
			},
		},
		{
			Entry: inventory.Entry{
				Record: inventory.Record{ID: "half"}, Dir: "/state/half", State: inventory.Stopped,
				Err: inventory.ErrNoRecord,
			},
			DiskUsed: new(int64(4 << 20)),
		},
		{
			Entry: inventory.Entry{
				Record: inventory.Record{ID: "old"}, Dir: "/state/old", State: inventory.Unknown,
				Err: inventory.ErrNoRecord,
			},
		},
	}
}

func TestPrintListTable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer

	require.NoError(t, cli.PrintList(&out, listed(now), cli.ListOptions{Format: table}, now))

	require.Equal(t, ""+
		"SANDBOX ID     IMAGE                REPOSITORY      CREATED         STATUS    DISK             NAME\n"+
		"0123456789ab   cove-go:local        git.example/r   5 minutes ago   running   1.2G (max 64G)   demo\n"+
		"fedcba987654   cove-sandbox:local   git.example/r   3 hours ago     stopped   max 1T           cove-fedcba987654\n"+
		"half                                                                stopped   4M               \n"+
		"old                                                                 unknown                    \n",
		out.String())
}

func TestPrintListQuietGivesShortIDs(t *testing.T) {
	t.Parallel()

	now := time.Now()
	var out bytes.Buffer

	require.NoError(t, cli.PrintList(&out, listed(now), cli.ListOptions{Quiet: true, Format: table}, now))

	require.Equal(t, "0123456789ab\nfedcba987654\nhalf\nold\n", out.String())
}

func TestPrintListJSONIsTheContract(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer

	// Quiet does not change a format that has its own shape.
	require.NoError(t, cli.PrintList(&out, listed(now), cli.ListOptions{Quiet: true, Format: jsonFormat}, now))

	require.JSONEq(t, `[
		{"id": "0123456789abcdef", "name": "demo", "state": "running", "image": "cove-go:local",
		 "digest": "sha256:aa", "repository": "https://git.example/r", "branch": "fix",
		 "created": "2026-09-27T11:55:00Z", "cpus": 4, "memory": 536870912, "disk": 68719476736,
		 "diskUsed": 1288490189, "dir": "/state/0123456789abcdef"},
		{"id": "fedcba9876543210", "name": "cove-fedcba987654", "state": "stopped",
		 "image": "cove-sandbox:local", "digest": "sha256:bb",
		 "repository": "https://git.example/r", "created": "2026-09-27T09:00:00Z",
		 "cpus": 2, "memory": 2147483648, "disk": 1099511627776, "dir": "/state/fedcba9876543210"},
		{"id": "half", "state": "stopped", "diskUsed": 4194304, "dir": "/state/half",
		 "error": "no record"},
		{"id": "old", "state": "unknown", "dir": "/state/old", "error": "no record"}
	]`, out.String())
}

func TestPrintListJSONOfNothingIsAnEmptyArray(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	require.NoError(t, cli.PrintList(&out, nil, cli.ListOptions{Format: jsonFormat}, time.Now()))

	require.JSONEq(t, `[]`, out.String(), "a program must read an empty list, not null")
}

func TestHumanDuration(t *testing.T) {
	t.Parallel()

	for d, want := range map[time.Duration]string{
		0:                    "Less than a second",
		time.Second:          "1 second",
		45 * time.Second:     "45 seconds",
		90 * time.Second:     "About a minute",
		59 * time.Minute:     "59 minutes",
		90 * time.Minute:     "About an hour",
		47 * time.Hour:       "47 hours",
		3 * 24 * time.Hour:   "3 days",
		20 * 24 * time.Hour:  "2 weeks",
		60 * 24 * time.Hour:  "8 weeks",
		95 * 24 * time.Hour:  "3 months",
		800 * 24 * time.Hour: "2 years",
	} {
		require.Equal(t, want, cli.HumanDuration(d), d)
	}
}

func TestListReportsTheInventory(t *testing.T) {
	// The sandboxes go to a fresh state directory, the one of XDG on Linux and of the home on
	// macOS.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", home)

	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		app := &cli.App{Stdout: &stdout, Stderr: &stderr}
		code := app.Run(args)
		return code, stdout.String(), stderr.String()
	}

	// Before any run there is no directory of the sandboxes, and list does not create one.
	code, stdout, stderr := run(listArgs("--format", jsonFormat)...)
	require.Equal(t, 0, code, stderr)
	require.JSONEq(t, `[]`, stdout)

	root := sandboxRoot(t)
	_, lock, err := inventory.Add(t.Context(), root, inventory.Record{ID: "0123456789abcdef", Name: demo})
	require.NoError(t, err)

	for _, args := range [][]string{listArgs("-q"), {"ls", "-q"}, {"ps", "-q"}} {
		code, stdout, stderr = run(args...)
		require.Equal(t, 0, code, stderr)
		require.Equal(t, "0123456789ab\n", stdout, args[0])
	}

	lock.Release()

	code, stdout, stderr = run(listArgs("--format", jsonFormat)...)
	require.Equal(t, 0, code, stderr)
	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(stdout), &got))
	require.Len(t, got, 1)
	require.Equal(t, "stopped", got[0]["state"], "a stopped sandbox is listed: its disk is still on the host")
	require.Equal(t, demo, got[0]["name"])
	require.NotContains(t, got[0], "diskUsed", "no disk was written, and none is measured as empty")
}

// sandboxRoot returns the directory of the sandboxes the environment of the test gives.
func sandboxRoot(t *testing.T) string {
	t.Helper()
	root, err := sandbox.DefaultRoot()
	require.NoError(t, err)
	return root
}

func TestListWarnsInOneShortLinePerSandbox(t *testing.T) {
	t.Parallel()

	denied := &fs.PathError{Op: "stat", Path: "/state/x/rw.ext4", Err: fs.ErrPermission}
	tests := []struct {
		name string
		l    cli.Listed
		want []string
	}{
		{name: "a sandbox as run leaves it", l: listed(time.Now())[0]},
		{
			name: "older than the inventory",
			l:    listed(time.Now())[3],
			want: []string{"no lock, state unknown", "no record"},
		},
		{
			name: "a disk that cannot be measured says the cause, not the path",
			l:    cli.Listed{Entry: inventory.Entry{State: inventory.Stopped}, DiskErr: denied},
			want: []string{"cannot measure its disk: permission denied"},
		},
		{
			name: "a record changed by hand",
			l: cli.Listed{Entry: inventory.Entry{
				State: inventory.Stopped, Err: errors.New("unreadable record: unexpected end of JSON input"),
			}},
			want: []string{"unreadable record: unexpected end of JSON input"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, cli.Warnings(tt.l))
		})
	}
}

func TestRepositoryColumnSaysForgeGroupAndName(t *testing.T) {
	t.Parallel()

	const cove = "gitlab.com/hich-hich/cove"
	for url, want := range map[string]string{
		"https://gitlab.com/hich-hich/cove":          cove,
		"https://gitlab.com/hich-hich/cove.git":      cove,
		"https://gitlab.com/hich-hich/cove/":         cove,
		"https://oauth2@gitlab.com/group/sub/repo":   "gitlab.com/group/sub/repo",
		"git@gitlab.com:hich-hich/cove.git":          cove,
		"ssh://git@gitlab.com/hich-hich/cove.git":    cove,
		"ssh://git@forge.example:2222/team/repo.git": "forge.example:2222/team/repo",
		"forge.example:repo.git":                     "forge.example/repo",
	} {
		require.Equal(t, want, cli.RepositoryColumn(url), url)
	}
}
