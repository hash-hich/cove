package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// id is the sandbox the tests remove.
const id = "a1"

// added adds sandbox id to root and returns its entry as list finds it, its lock held while it runs.
func added(t *testing.T, root string, running bool) inventory.Entry {
	t.Helper()
	desc := inventory.Description{ID: id, Name: "sandbox-" + id, Created: time.Now()}
	_, lock, err := inventory.Add(t.Context(), root, desc)
	require.NoError(t, err)
	if running {
		t.Cleanup(lock.Release)
	} else {
		lock.Release()
	}
	entries, err := inventory.List(t.Context(), root)
	require.NoError(t, err)
	e, err := inventory.Find(entries, id)
	require.NoError(t, err)
	return e
}

func TestRemoveTakesAStoppedSandboxWithItsDisk(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	e := added(t, root, false)
	require.NoError(t, os.WriteFile(filepath.Join(e.Dir, "rw.ext4"), nil, 0o600))

	require.NoError(t, sandbox.Remove(t.Context(), root, e, false))

	require.NoDirExists(t, e.Dir)
}

func TestRemoveRefusesASandboxWhoseLockIsHeld(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	e := added(t, root, true)

	require.ErrorIs(t, sandbox.Remove(t.Context(), root, e, false), sandbox.ErrRunning)
	require.DirExists(t, e.Dir)
}

func TestRemoveGoesByTheLockNotByTheListedState(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	e := added(t, root, false)
	// A VM started between the list and the removal.
	lock, ok, err := inventory.TryHold(e.Dir)
	require.NoError(t, err)
	require.True(t, ok)
	t.Cleanup(lock.Release)

	require.ErrorIs(t, sandbox.Remove(t.Context(), root, e, false), sandbox.ErrRunning)
	require.DirExists(t, e.Dir)
}

func TestRemoveRefusesASandboxWithoutALock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	e := added(t, root, false)
	require.NoError(t, os.Remove(filepath.Join(e.Dir, "lock")))
	e.State = inventory.Unknown

	require.ErrorIs(t, sandbox.Remove(t.Context(), root, e, true), sandbox.ErrStateUnknown,
		"nothing says no VM writes on its disk, forced or not")
	require.DirExists(t, e.Dir)
}
