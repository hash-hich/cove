package inventory_test

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/filelock"
	"gitlab.com/hich-hich/cove/internal/inventory"
)

// demo is the name the tests give a sandbox.
const demo = "demo"

// description returns the description of a sandbox id named name, created at created.
func description(id, name string, created time.Time) inventory.Description {
	return inventory.Description{
		ID: id, Name: name, Image: "cove-sandbox:local", Digest: "sha256:aa",
		Repository: "https://git.example/r", Created: created.UTC(),
	}
}

func TestASandboxRunsWhileItsLockIsHeld(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	desc := description("a1", demo, time.Now())

	dir, lock, err := inventory.Add(t.Context(), root, desc)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, "a1"), dir)

	got, err := inventory.List(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, []inventory.Entry{{Description: desc, Dir: dir, State: inventory.Running}}, got)

	// The holder ends: nothing has to write that the VM stopped for the entry to say so.
	lock.Release()

	got, err = inventory.List(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, []inventory.Entry{{Description: desc, Dir: dir, State: inventory.Stopped}}, got)
}

func TestANameIsTakenByAStoppedSandboxToo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, lock, err := inventory.Add(t.Context(), root, description("a1", demo, time.Now()))
	require.NoError(t, err)
	lock.Release()

	_, _, err = inventory.Add(t.Context(), root, description("b2", demo, time.Now()))

	require.ErrorIs(t, err, inventory.ErrNameInUse)
	require.ErrorContains(t, err, "a1")
	require.NoDirExists(t, filepath.Join(root, "b2"))
}

func TestTwoRunsThatWantOneNameGetItOnce(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const runs = 8
	var (
		wg   sync.WaitGroup
		errs = make([]error, runs)
	)
	for i := range runs {
		wg.Go(func() {
			var lock *filelock.Lock
			_, lock, errs[i] = inventory.Add(t.Context(), root, description("id"+strconv.Itoa(i), demo, time.Now()))
			if errs[i] == nil {
				lock.Release()
			}
		})
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		} else {
			require.ErrorIs(t, err, inventory.ErrNameInUse)
		}
	}
	require.Equal(t, 1, won)
	got, err := inventory.List(t.Context(), root)
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestListShowsWhatIsOnTheHostEvenWithoutADescription(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// A run killed between its directory and its description, and a description someone changed by
	// hand.
	require.NoError(t, os.Mkdir(filepath.Join(root, "half"), 0o700))
	require.NoError(t, os.Mkdir(filepath.Join(root, "moved"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "moved", "metadata.json"), []byte(`{"id":"other"}`), 0o600))

	got, err := inventory.List(t.Context(), root)

	require.NoError(t, err)
	require.Len(t, got, 2)
	for i, id := range []string{"half", "moved"} {
		require.Equal(t, id, got[i].ID)
		require.Equal(t, filepath.Join(root, id), got[i].Dir)
		require.Equal(t, inventory.Unknown, got[i].State, "a directory without a lock says nothing of a VM")
		require.Error(t, got[i].Err)
		require.Empty(t, got[i].Name)
	}
	require.ErrorIs(t, got[0].Err, inventory.ErrNoMetadata)
	require.ErrorContains(t, got[1].Err, `names sandbox "other"`)
	require.NoFileExists(t, filepath.Join(root, "half", "lock"), "a look must not create the lock of a sandbox")
}

func TestListPutsTheLatestFirstAndTheUnreadableLast(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	now := time.Now()
	for i, id := range []string{"old", "new", "mid"} {
		created := map[int]time.Duration{0: -time.Hour, 1: 0, 2: -time.Minute}[i]
		_, lock, err := inventory.Add(t.Context(), root, description(id, id, now.Add(created)))
		require.NoError(t, err)
		lock.Release()
	}
	require.NoError(t, os.Mkdir(filepath.Join(root, "broken"), 0o700))

	got, err := inventory.List(t.Context(), root)

	require.NoError(t, err)
	ids := make([]string, 0, len(got))
	for _, e := range got {
		ids = append(ids, e.ID)
	}
	require.Equal(t, []string{"new", "mid", "old", "broken"}, ids)
}

func TestListDoesNotCreateTheRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sandboxes")

	got, err := inventory.List(t.Context(), root)

	require.NoError(t, err)
	require.Empty(t, got)
	require.NoDirExists(t, root, "no run ever created a sandbox, and a list changes nothing")
}

func TestRemoveTakesTheSandboxOutOfTheInventory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	_, lock, err := inventory.Add(t.Context(), root, description("a1", demo, time.Now()))
	require.NoError(t, err)

	require.NoError(t, inventory.Remove(t.Context(), root, "a1"))
	lock.Release()

	got, err := inventory.List(t.Context(), root)
	require.NoError(t, err)
	require.Empty(t, got)
	// The name is free again.
	_, lock, err = inventory.Add(t.Context(), root, description("b2", demo, time.Now()))
	require.NoError(t, err)
	lock.Release()
}

func TestASandboxWhoseLockIsGoneIsUnknownNotStopped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	desc := description("a1", demo, time.Now())
	dir, lock, err := inventory.Add(t.Context(), root, desc)
	require.NoError(t, err)
	lock.Release()
	// A sandbox created before the inventory, or whose lock someone removed: its VM may run.
	require.NoError(t, os.Remove(filepath.Join(dir, "lock")))

	got, err := inventory.List(t.Context(), root)

	require.NoError(t, err)
	require.Equal(t, []inventory.Entry{{Description: desc, Dir: dir, State: inventory.Unknown}}, got)
}

func TestFind(t *testing.T) {
	t.Parallel()

	const front = "a1b2c3"
	entries := []inventory.Entry{
		{Description: inventory.Description{ID: front, Name: "front"}},
		{Description: inventory.Description{ID: "a1f0", Name: "a1b2c3d"}},
		{Description: inventory.Description{ID: "b9"}, Err: inventory.ErrNoMetadata},
	}
	tests := []struct {
		name, target, want string
		wantErr            error
	}{
		{name: "whole ID", target: front, want: front},
		{name: "name", target: "front", want: front},
		{name: "name before a prefix", target: "a1b2c3d", want: "a1f0"},
		{name: "prefix", target: "a1b", want: front},
		{name: "ID of an unreadable description", target: "b9", want: "b9"},
		{name: "prefix of several", target: "a1", wantErr: inventory.ErrAmbiguous},
		{name: "prefix of a name", target: "fro", wantErr: inventory.ErrNotFound},
		{name: "unknown", target: "back", wantErr: inventory.ErrNotFound},
		{name: "empty", target: "", wantErr: inventory.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := inventory.Find(entries, tt.target)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got.ID)
		})
	}
}

func TestHoldWaitsForTheVMToEnd(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir, lock, err := inventory.Add(t.Context(), root, description("a1", demo, time.Now()))
	require.NoError(t, err)
	time.AfterFunc(2*filelock.Poll, lock.Release)

	held, err := inventory.Hold(t.Context(), dir)
	require.NoError(t, err)
	t.Cleanup(held.Release)

	got, err := inventory.List(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, inventory.Running, got[0].State, "a held sandbox starts no VM, and reads as running")
}

func TestTryHoldRefusesARunningSandbox(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir, lock, err := inventory.Add(t.Context(), root, description("a1", demo, time.Now()))
	require.NoError(t, err)

	_, ok, err := inventory.TryHold(dir)
	require.NoError(t, err)
	require.False(t, ok, "the lock of a running sandbox is not taken")

	lock.Release()
	held, ok, err := inventory.TryHold(dir)
	require.NoError(t, err)
	require.True(t, ok)
	t.Cleanup(held.Release)

	got, err := inventory.List(t.Context(), root)
	require.NoError(t, err)
	require.Equal(t, inventory.Running, got[0].State, "a held sandbox starts no VM, and reads as running")
}
