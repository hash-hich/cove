package vmmproto_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vmmproto"
)

func TestMessagesGoThroughInTheirOrder(t *testing.T) {
	t.Parallel()

	var pipe bytes.Buffer
	hello := vmmproto.Hello{Build: "b", VMM: "libkrun v1", LibrarySHA256: "ab"}
	boot := vmmproto.Boot{
		CPUs: 2, MemoryMiB: 2048, Kernel: 4, KernelFormat: vmmproto.KernelRaw, Initramfs: 5,
		Cmdline: "console=hvc0", Disks: []vmmproto.Disk{{FD: 7, ReadOnly: true}, {FD: 8}}, Console: 6,
	}
	require.NoError(t, vmmproto.Send(&pipe, hello))
	require.NoError(t, vmmproto.Send(&pipe, boot))

	r := vmmproto.NewReceiver(&pipe)
	var gotHello vmmproto.Hello
	var gotBoot vmmproto.Boot
	require.NoError(t, r.Receive(&gotHello))
	require.NoError(t, r.Receive(&gotBoot))

	require.Equal(t, hello, gotHello)
	require.Equal(t, boot, gotBoot)
	require.ErrorIs(t, r.Receive(&gotBoot), io.EOF, "the other side closed its end")
}

func TestReceiveRefusesAFieldItDoesNotKnow(t *testing.T) {
	t.Parallel()

	r := vmmproto.NewReceiver(strings.NewReader(`{"build":"b","tsi":true}` + "\n"))

	var hello vmmproto.Hello
	err := r.Receive(&hello)

	require.ErrorContains(t, err, "tsi", "the two sides come from one build, a field one lacks is a mismatch")
}
