package card_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/cmd/cove-net/internal/card"
)

func TestFramesGoThroughInTheirOrder(t *testing.T) {
	t.Parallel()

	var sock bytes.Buffer
	frames := [][]byte{[]byte("first"), bytes.Repeat([]byte{7}, card.MaxFrame)}
	for _, f := range frames {
		require.NoError(t, card.WriteFrame(&sock, f))
	}
	require.Equal(t, []byte{0, 0, 0, 5}, sock.Bytes()[:4], "the length in big endian, then the frame")

	// A socket gives what it has, a byte at a time at worst.
	r := iotest.OneByteReader(&sock)
	buf := make([]byte, card.MaxFrame)
	for _, want := range frames {
		got, err := card.ReadFrame(r, buf)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	_, err := card.ReadFrame(r, buf)
	require.ErrorIs(t, err, io.EOF, "the card closed between two frames")
}

func TestReadFrameRefusesALengthTheCardDoesNotCarry(t *testing.T) {
	t.Parallel()

	for _, size := range []uint32{0, card.MaxFrame + 1, 1 << 31} {
		sock := binary.BigEndian.AppendUint32(nil, size)
		_, err := card.ReadFrame(bytes.NewReader(sock), make([]byte, card.MaxFrame))
		require.ErrorIs(t, err, card.ErrFrame, "%d bytes", size)
	}
}

func TestReadFrameReportsAFrameCutShort(t *testing.T) {
	t.Parallel()

	sock := append(binary.BigEndian.AppendUint32(nil, 10), "short"...)

	_, err := card.ReadFrame(bytes.NewReader(sock), make([]byte, card.MaxFrame))

	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestWriteFrameRefusesALengthTheCardDoesNotCarry(t *testing.T) {
	t.Parallel()

	for _, frame := range [][]byte{nil, make([]byte, card.MaxFrame+1)} {
		require.ErrorIs(t, card.WriteFrame(io.Discard, frame), card.ErrFrame)
	}
}
