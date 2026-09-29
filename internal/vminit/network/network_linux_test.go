package network_test

import (
	"encoding/binary"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/internal/vminit/network"
)

// The messages are written in the order of the host, little endian on both architectures of the
// guest, arm64 and x86_64.

func TestAddrMessageGivesTheAddressWithItsPrefix(t *testing.T) {
	t.Parallel()

	got := network.AddrMessage(1, 2, netip.MustParsePrefix("10.0.2.2/30"))

	require.Equal(t, []byte{
		40, 0, 0, 0, // Length.
		20, 0, // RTM_NEWADDR.
		0x05, 0x06, // NLM_F_REQUEST, NLM_F_ACK, NLM_F_EXCL, NLM_F_CREATE.
		1, 0, 0, 0, // Sequence.
		0, 0, 0, 0, // Port.
		2, 30, 0, 0, // AF_INET, /30, no flag, RT_SCOPE_UNIVERSE.
		2, 0, 0, 0, // Index.
		8, 0, 2, 0, 10, 0, 2, 2, // IFA_LOCAL.
		8, 0, 1, 0, 10, 0, 2, 2, // IFA_ADDRESS.
	}, got)
}

func TestLinkUpMessageSetsOnlyTheFlagUp(t *testing.T) {
	t.Parallel()

	got := network.LinkUpMessage(2, 2)

	require.Equal(t, []byte{
		32, 0, 0, 0, // Length.
		16, 0, // RTM_NEWLINK.
		0x05, 0x00, // NLM_F_REQUEST, NLM_F_ACK.
		2, 0, 0, 0, // Sequence.
		0, 0, 0, 0, // Port.
		0, 0, 0, 0, // AF_UNSPEC, padding, type.
		2, 0, 0, 0, // Index.
		1, 0, 0, 0, // IFF_UP.
		1, 0, 0, 0, // The mask of the change, IFF_UP alone.
	}, got)
}

func TestRouteMessageRoutesEverythingThroughTheGateway(t *testing.T) {
	t.Parallel()

	got := network.RouteMessage(3, 2, netip.MustParseAddr("10.0.2.1"))

	require.Equal(t, []byte{
		44, 0, 0, 0, // Length.
		24, 0, // RTM_NEWROUTE.
		0x05, 0x06, // NLM_F_REQUEST, NLM_F_ACK, NLM_F_EXCL, NLM_F_CREATE.
		3, 0, 0, 0, // Sequence.
		0, 0, 0, 0, // Port.
		2, 0, 0, 0, // AF_INET, the default route, any source, any type of service.
		254, 3, 0, 1, // RT_TABLE_MAIN, RTPROT_BOOT, RT_SCOPE_UNIVERSE, RTN_UNICAST.
		0, 0, 0, 0, // Flags.
		8, 0, 5, 0, 10, 0, 2, 1, // RTA_GATEWAY.
		8, 0, 4, 0, 2, 0, 0, 0, // RTA_OIF.
	}, got)
}

func TestAckErrorReadsTheErrorOfTheKernel(t *testing.T) {
	t.Parallel()

	// An NLMSG_ERROR, then the error.
	ack := func(errno int32) []byte {
		b := make([]byte, 0, 20)
		b = append(b, 36, 0, 0, 0, 2, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0)
		return binary.LittleEndian.AppendUint32(b, uint32(errno)) //nolint:gosec // G115: a negated errno.
	}

	require.NoError(t, network.AckError(ack(0)))
	require.ErrorIs(t, network.AckError(ack(-int32(unix.EEXIST))), unix.EEXIST)
	require.ErrorContains(t, network.AckError(ack(0)[:10]), "an answer of 10 bytes")
}
