package image_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/image"
)

// holderEnv names the lock the helper process takes, and holderReady is what it says once it
// holds it.
const (
	holderEnv   = "COVE_TEST_LOCK"
	holderReady = "locked"
)

func TestTryLockExcludesTheSamePathOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "blob.lock")
	held, ok, err := image.TryLock(path)
	require.NoError(t, err)
	require.True(t, ok)

	// The same blob is refused, and an absence of error is how a caller tells it apart from a
	// store it cannot write.
	_, ok, err = image.TryLock(path)
	require.NoError(t, err)
	require.False(t, ok)

	// Another blob is free: two pulls wait on each other only for a layer both want.
	other, ok, err := image.TryLock(filepath.Join(dir, "other.lock"))
	require.NoError(t, err)
	require.True(t, ok)
	image.Unlock(other)

	image.Unlock(held)
	again, ok, err := image.TryLock(path)
	require.NoError(t, err)
	require.True(t, ok)
	image.Unlock(again)
}

func TestLockWaitsForTheHolderAndSaysSoOnce(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "blob.lock")
	held, ok, err := image.TryLock(path)
	require.NoError(t, err)
	require.True(t, ok)
	var waits atomic.Int64
	type taken struct {
		lock *image.FileLock
		err  error
	}
	got := make(chan taken, 1)
	go func() {
		l, err := image.Lock(t.Context(), path, func() { waits.Add(1) })
		got <- taken{lock: l, err: err}
	}()

	// Several rounds of the wait pass, announced once and only once, and nothing is taken while
	// the lock is held.
	time.Sleep(3 * image.LockPoll)
	require.Equal(t, int64(1), waits.Load())
	require.Empty(t, got)

	image.Unlock(held)

	select {
	case res := <-got:
		require.NoError(t, res.err)
		image.Unlock(res.lock)
	case <-time.After(10 * image.LockPoll):
		t.Fatal("the lock was not taken once its holder released it")
	}
	require.Equal(t, int64(1), waits.Load())
}

func TestLockStopsWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "blob.lock")
	held, ok, err := image.TryLock(path)
	require.NoError(t, err)
	require.True(t, ok)
	t.Cleanup(func() { image.Unlock(held) })
	ctx, cancel := context.WithCancelCause(t.Context())
	interrupted := errors.New("interrupt received")

	// A signal reaches a pull that waits: the wait ends with the reason, and nothing else does.
	_, err = image.Lock(ctx, path, func() { cancel(interrupted) })

	require.ErrorIs(t, err, interrupted)
	require.ErrorContains(t, err, path)
}

func TestLockReportsAPathItCannotOpen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", "blob.lock")

	_, ok, err := image.TryLock(path)

	require.ErrorContains(t, err, "open the lock")
	require.False(t, ok)

	_, err = image.Lock(t.Context(), path, nil)

	require.ErrorContains(t, err, "open the lock")
}

func TestLockIsReleasedWhenItsHolderIsKilled(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "blob.lock")
	// The holder is this test binary run again on the test below: only a real process killed
	// outright shows what the kernel does for a pull that never gets to clean up, which is why
	// the lock is one of the system and not a file cove writes.
	//nolint:gosec // G204: the program is the test binary itself and the arguments are fixed.
	holder := exec.CommandContext(t.Context(), os.Args[0],
		"-test.run=TestHolderOfALockThatIsKilled", "-test.timeout=1m")
	holder.Env = append(os.Environ(), holderEnv+"="+path)
	out, err := holder.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, holder.Start())
	t.Cleanup(func() { _ = holder.Process.Kill() })
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, holderReady+"\n", line)
	_, ok, err := image.TryLock(path)
	require.NoError(t, err)
	require.False(t, ok, "the holder is running and holds the lock")

	require.NoError(t, holder.Process.Kill())
	_ = holder.Wait()

	l, ok, err := image.TryLock(path)
	require.NoError(t, err)
	require.True(t, ok, "a killed holder left its lock taken")
	image.Unlock(l)
}

// TestHolderOfALockThatIsKilled is the process the test above starts and kills, not a test of its
// own: it takes the lock the environment names, says so, and waits. It does nothing in a plain
// run of the suite.
func TestHolderOfALockThatIsKilled(t *testing.T) {
	t.Parallel()

	path := os.Getenv(holderEnv)
	if path == "" {
		t.Skip("this process was not started by TestLockIsReleasedWhenItsHolderIsKilled")
	}
	_, ok, err := image.TryLock(path)
	require.NoError(t, err)
	require.True(t, ok)
	_, _ = fmt.Println(holderReady)
	time.Sleep(time.Minute)
}
