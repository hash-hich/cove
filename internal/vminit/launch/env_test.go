package launch_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vminit/launch"
)

func TestEnvironStartsFromWhatDockerSets(t *testing.T) {
	t.Parallel()
	require.Equal(t, []string{
		"PATH=" + launch.DefaultPath, "HOSTNAME=sandbox", "HOME=/root",
	}, launch.Environ(nil, "/root", "sandbox", false))

	require.Equal(t, []string{
		"PATH=" + launch.DefaultPath, "HOSTNAME=sandbox", "TERM=xterm", "HOME=/root",
	}, launch.Environ(nil, "/root", "sandbox", true))
}

func TestEnvironAppliesTheImageOverIt(t *testing.T) {
	t.Parallel()
	got := launch.Environ([]string{
		"PATH=/opt/bin:/usr/bin",
		"LANG=C.UTF-8",
		"LANG=en_US.UTF-8",
		"PRICE=$5",
		"HOSTNAME",
		"HOME=/home/agent",
	}, "/root", "sandbox", true)
	require.Equal(t, []string{
		"PATH=/opt/bin:/usr/bin", "TERM=xterm", "LANG=en_US.UTF-8", "PRICE=$5", "HOME=/home/agent",
	}, got)
}

func TestGetenv(t *testing.T) {
	t.Parallel()
	env := []string{"PATH=/bin", "PATHS=/nope"}
	require.Equal(t, "/bin", launch.Getenv(env, "PATH"))
	require.Empty(t, launch.Getenv(env, "HOME"))
}

func TestLookPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	for _, d := range []string{first, second} {
		require.NoError(t, os.Mkdir(d, 0o750))
	}
	require.NoError(t, os.WriteFile(filepath.Join(first, "claude"), nil, 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(first, "sh"), 0o750))
	for _, name := range []string{"claude", "sh"} {
		//nolint:gosec // G306: a program, which the lookup tells by its execute bit.
		require.NoError(t, os.WriteFile(filepath.Join(second, name), nil, 0o700))
	}
	path := first + ":" + second

	// Neither a file without an execute bit nor a directory is a program.
	got, err := launch.LookPath("claude", path)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(second, "claude"), got)
	got, err = launch.LookPath("sh", path)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(second, "sh"), got)

	_, err = launch.LookPath("codex", path)
	require.EqualError(t, err, "codex not found in PATH ("+path+")")

	got, err = launch.LookPath(filepath.Join(second, "claude"), "")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(second, "claude"), got)
}
