// Package card is the link between the network card of the VM and the stack of cove-net: the
// ethernet frames the card exchanges on its Unix stream socket, each preceded by its length, and
// the endpoint that presents them to gVisor as an ethernet link.
//
// The format is the one of libkrun v1.19.5 (src/devices/src/virtio/net/unixstream.rs), which qemu
// and passt share: a length of 4 bytes in big endian, then the frame. The card offers no offload,
// so a frame is always whole and carries its checksums.
package card

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/ethernet"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// MaxFrame is the longest frame of the socket: MAX_BUFFER_SIZE of libkrun less the virtio-net
// header it drops.
const MaxFrame = 65550

// MTU is the largest IP packet the link carries, the MTU of the card in the guest.
const MTU = 1500

// queueLen is how many frames the stack may queue for the card before it drops the next ones, as
// a full transmit ring would.
const queueLen = 1024

// ErrFrame reports a frame of a length the format refuses, zero or past MaxFrame: the writer does
// not follow the format any more.
var ErrFrame = errors.New("a frame of a length the card does not carry")

// ReadFrame reads the next frame of r into buf, which is MaxFrame long at least, and returns it.
// It returns io.EOF when r ends between two frames.
func ReadFrame(r io.Reader, buf []byte) ([]byte, error) {
	var n [4]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return nil, err //nolint:wrapcheck // io.EOF is the end of the card, as it is.
	}
	size := binary.BigEndian.Uint32(n[:])
	if size == 0 || size > MaxFrame {
		return nil, fmt.Errorf("%w: %d bytes", ErrFrame, size)
	}
	if _, err := io.ReadFull(r, buf[:size]); err != nil {
		return nil, fmt.Errorf("read a frame of %d bytes: %w", size, err)
	}
	return buf[:size], nil
}

// WriteFrame writes frame on w, preceded by its length.
func WriteFrame(w io.Writer, frame []byte) error {
	if len(frame) == 0 || len(frame) > MaxFrame {
		return fmt.Errorf("%w: %d bytes", ErrFrame, len(frame))
	}
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(frame))) //nolint:gosec // G115: bounded by MaxFrame.
	if _, err := w.Write(n[:]); err != nil {
		return fmt.Errorf("write a frame: %w", err)
	}
	if _, err := w.Write(frame); err != nil {
		return fmt.Errorf("write a frame: %w", err)
	}
	return nil
}

// Link carries the frames of one socket to and from one NIC of a gVisor stack.
type Link struct {
	conn io.ReadWriteCloser
	ch   *channel.Endpoint
	eth  *ethernet.Endpoint
}

// NewLink returns the link of conn, whose NIC answers as mac.
func NewLink(conn io.ReadWriteCloser, mac tcpip.LinkAddress) *Link {
	ch := channel.New(queueLen, MTU+header.EthernetMinimumSize, mac)
	return &Link{conn: conn, ch: ch, eth: ethernet.New(ch)}
}

// Endpoint returns what the NIC is created with.
func (l *Link) Endpoint() stack.LinkEndpoint { return l.eth }

// Run carries frames both ways until the socket ends, then closes it. It returns nil when the other
// end closed the socket between two frames, and what ended it otherwise: a frame the format
// refuses, a socket that fails, or the end of ctx.
func (l *Link) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancelCause(ctx)
	// Closing the socket is what stops a read, whichever side ended first.
	stop := context.AfterFunc(ctx, func() { _ = l.conn.Close() })
	defer stop()
	var wg sync.WaitGroup
	wg.Go(func() { cancel(l.send(ctx)) })
	cancel(l.receive())
	wg.Wait()
	l.ch.Close()
	if err := context.Cause(ctx); !errors.Is(err, io.EOF) {
		return err //nolint:wrapcheck // What receive or send returned, named there, or the end of ctx.
	}
	return nil
}

// receive hands the stack each frame of the socket, until it fails or ends.
func (l *Link) receive() error {
	r := bufio.NewReaderSize(l.conn, 2*MaxFrame)
	buf := make([]byte, MaxFrame)
	for {
		frame, err := ReadFrame(r, buf)
		if err != nil {
			return err
		}
		// MakeWithData copies the frame, which the stack keeps while buf takes the next one.
		pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(frame)})
		l.ch.InjectInbound(0, pkt)
		pkt.DecRef()
	}
}

// send writes on the socket each frame the stack queues, flushing once the queue is empty, until
// ctx ends.
func (l *Link) send(ctx context.Context) error {
	w := bufio.NewWriterSize(l.conn, 2*MaxFrame)
	for {
		pkt := l.ch.ReadContext(ctx)
		if pkt == nil {
			return context.Cause(ctx) //nolint:wrapcheck // What ended the card, named where it ended.
		}
		v := pkt.ToView()
		pkt.DecRef()
		err := WriteFrame(w, v.AsSlice())
		v.Release()
		if err == nil && l.ch.NumQueued() == 0 {
			if err = w.Flush(); err != nil {
				err = fmt.Errorf("write a frame: %w", err)
			}
		}
		if err != nil {
			return err
		}
	}
}
