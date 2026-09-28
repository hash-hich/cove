package turn_test

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vminit/turn"
)

func TestFrames(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	req := turn.Request{
		ID: "t1", Args: []string{"claude", "-p"}, TTY: true, Size: turn.Size{Rows: 40, Cols: 120}, Grace: time.Second,
	}
	require.NoError(t, turn.WriteJSON(&buf, turn.Exec, req))
	require.NoError(t, turn.Write(&buf, turn.Stdin, []byte("hi")))
	require.NoError(t, turn.Write(&buf, turn.StdinEOF, nil))
	require.NoError(t, turn.Write(&buf, turn.Resize, turn.SizePayload(turn.Size{Rows: 50, Cols: 200})))

	k, p, err := turn.Read(&buf)
	require.NoError(t, err)
	require.Equal(t, turn.Exec, k)
	var got turn.Request
	require.NoError(t, turn.Decode(p, &got))
	require.Equal(t, req, got)

	k, p, err = turn.Read(&buf)
	require.NoError(t, err)
	require.Equal(t, turn.Stdin, k)
	require.Equal(t, []byte("hi"), p)

	k, p, err = turn.Read(&buf)
	require.NoError(t, err)
	require.Equal(t, turn.StdinEOF, k)
	require.Empty(t, p)

	k, p, err = turn.Read(&buf)
	require.NoError(t, err)
	require.Equal(t, turn.Resize, k)
	size, err := turn.ParseSize(p)
	require.NoError(t, err)
	require.Equal(t, turn.Size{Rows: 50, Cols: 200}, size)

	_, _, err = turn.Read(&buf)
	require.ErrorIs(t, err, io.EOF, "the end between two frames is the end of the turn")
}

func TestReadWithinAFrame(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, turn.Write(&buf, turn.Stdout, []byte("partial")))
	cut := bytes.NewReader(buf.Bytes()[:buf.Len()-2])

	_, _, err := turn.Read(cut)

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestFrameBound(t *testing.T) {
	t.Parallel()

	require.ErrorContains(t, turn.Write(io.Discard, turn.Stdout, make([]byte, turn.MaxPayload+1)), "more than")
	// A header announcing more than the bound is refused before anything is allocated.
	_, _, err := turn.Read(bytes.NewReader([]byte{byte(turn.Stdout), 0xff, 0xff, 0xff, 0xff}))
	require.ErrorContains(t, err, "more than")
}

func TestFailedPayload(t *testing.T) {
	t.Parallel()

	e := &turn.ExecError{Errno: turn.ErrnoNotFound, Message: "claude not found in PATH"}

	got, err := turn.ParseFailed(turn.FailedPayload(e))

	require.NoError(t, err)
	require.Equal(t, e, got)
}

func TestExit(t *testing.T) {
	t.Parallel()

	code := 0
	for want, exit := range map[string]turn.Exit{
		`{"cause":"agent","code":0}`:  {Cause: turn.CauseAgent, Code: &code},
		`{"cause":"stop","signal":1}`: {Cause: turn.CauseStop, Signal: 1},
	} {
		var buf bytes.Buffer
		require.NoError(t, turn.WriteJSON(&buf, turn.Exited, exit))
		_, p, err := turn.Read(&buf)
		require.NoError(t, err)
		require.JSONEq(t, want, string(p))
	}
}

func TestDecodeRefusesAnUnknownField(t *testing.T) {
	t.Parallel()

	require.ErrorContains(t, turn.Decode([]byte(`{"id":"t1","user":"root"}`), &turn.Request{}), "unknown field")
}
