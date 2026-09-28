package sandbox_test

import (
	"bytes"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
	"gitlab.com/hich-hich/cove/internal/vminit/turn"
)

// initSide plays the init on the other end of a connection: it writes frames, then closes.
func initSide(t *testing.T, write func(c net.Conn)) net.Conn {
	t.Helper()
	host, guest := net.Pipe()
	go func() {
		write(guest)
		_ = guest.Close()
	}()
	t.Cleanup(func() { _ = host.Close() })
	return host
}

func TestReadTurn(t *testing.T) {
	t.Parallel()

	code := 0
	conn := initSide(t, func(c net.Conn) {
		_ = turn.Write(c, turn.Started, turn.PutUint32(42))
		_ = turn.Write(c, turn.Stdout, []byte(`{"type":"result",`))
		_ = turn.Write(c, turn.Stderr, []byte("warning\n"))
		_ = turn.Write(c, turn.Stdout, []byte(`"session_id":"x"}`))
		_ = turn.WriteJSON(c, turn.Exited, turn.Exit{Cause: turn.CauseAgent, Code: &code})
	})
	var stdout, stderr bytes.Buffer

	exit, err := sandbox.ReadTurn(conn, sandbox.Streams{Stdout: &stdout, Stderr: &stderr})

	require.NoError(t, err)
	require.Equal(t, turn.Exit{Cause: turn.CauseAgent, Code: &code}, exit)
	require.JSONEq(t, `{"type":"result","session_id":"x"}`, stdout.String(), "stdout is the bytes of the agent, whole")
	require.Equal(t, "warning\n", stderr.String())
}

func TestReadTurnThatFailed(t *testing.T) {
	t.Parallel()

	conn := initSide(t, func(c net.Conn) {
		_ = turn.Write(c, turn.Failed, turn.FailedPayload(&turn.ExecError{Errno: 13, Message: "permission denied"}))
	})

	exit, err := sandbox.ReadTurn(conn, sandbox.Streams{})

	require.Equal(t, turn.CauseExec, exit.Cause)
	e, ok := err.(*turn.ExecError) //nolint:errorlint // ReadTurn returns it as is.
	require.True(t, ok, "%v", err)
	require.Equal(t, uint32(13), e.Errno)
}

func TestReadTurnCutShort(t *testing.T) {
	t.Parallel()

	conn := initSide(t, func(c net.Conn) {
		_ = turn.Write(c, turn.Started, turn.PutUint32(42))
	})

	_, err := sandbox.ReadTurn(conn, sandbox.Streams{})

	require.ErrorContains(t, err, "ended before the turn did")
}
