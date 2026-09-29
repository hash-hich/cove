package hostexit_test

import (
	"bytes"
	"errors"
	"net"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/cmd/cove-net/internal/hostexit"
)

var gateway = netip.MustParsePrefix("10.0.2.1/30")

// host is a host of the tests, on a LAN and a VPN.
func host(nameservers ...netip.AddrPort) *hostexit.Host {
	h := hostexit.New(gateway, nameservers)
	h.SetHostAddrs(func() ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.168.1.5"), netip.MustParseAddr("100.64.0.7")}, nil
	})
	return h
}

// The reasons of the refusals the tests expect more than once.
const (
	loopback = "the loopback of the host"
	hostAddr = "an address of the host"
	notOne   = "not the address of one machine"
	card     = "the network of the card"
)

func TestTargetRefusesTheHostAndWhatIsNotOneMachine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		dst    string
		reason string
	}{
		{"127.0.0.1:3000", loopback},
		{"127.1.2.3:22", loopback},
		{"192.168.1.5:3000", hostAddr},
		{"100.64.0.7:443", hostAddr},
		{"224.0.0.251:5353", notOne},
		{"255.255.255.255:9", notOne},
		{"0.0.0.0:80", notOne},
		{"0.1.2.3:80", notOne},
		{"10.0.2.0:80", card},
		{"10.0.2.1:80", card},
		{"10.0.2.2:22", card},
		{"10.0.2.3:80", card},
		{"192.168.1.254:80", ""},
		{"10.0.2.4:80", ""},
		{"10.0.1.255:80", ""},
		{"160.79.104.10:443", ""},
	}
	h := host(netip.MustParseAddrPort("192.168.1.254:53"))
	for _, tt := range tests {
		dst := netip.MustParseAddrPort(tt.dst)
		got, err := h.TargetOf(dst)
		if tt.reason == "" {
			require.NoError(t, err, tt.dst)
			require.Equal(t, dst, got, tt.dst)
			continue
		}
		refused, ok := errors.AsType[*hostexit.RefusedError](err)
		require.True(t, ok, "%s: %v", tt.dst, err)
		require.Equal(t, tt.reason, refused.Reason, tt.dst)
	}
}

func TestTargetTakesTheDNSOfTheGatewayToTheFirstResolver(t *testing.T) {
	t.Parallel()

	h := host(netip.MustParseAddrPort("192.168.1.254:5353"), netip.MustParseAddrPort("9.9.9.9:53"))

	got, err := h.TargetOf(netip.MustParseAddrPort("10.0.2.1:53"))

	require.NoError(t, err)
	require.Equal(t, netip.MustParseAddrPort("192.168.1.254:53"), got, "the first resolver, on port 53")
}

func TestTargetRefusesTheDNSOfTheGatewayWithoutAResolver(t *testing.T) {
	t.Parallel()

	_, err := host().TargetOf(netip.MustParseAddrPort("10.0.2.1:53"))

	_, ok := errors.AsType[*hostexit.RefusedError](err)
	require.True(t, ok, "%v", err)
}

func TestTargetRefusesWhenTheAddressesOfTheHostCannotBeRead(t *testing.T) {
	t.Parallel()

	h := host()
	h.SetHostAddrs(func() ([]netip.Addr, error) { return nil, errors.New("denied") })

	_, err := h.TargetOf(netip.MustParseAddrPort("160.79.104.10:443"))

	require.ErrorContains(t, err, "read the addresses of the host")
}

func TestInterfaceAddrsHoldsTheLoopback(t *testing.T) {
	t.Parallel()

	addrs, err := hostexit.InterfaceAddrs()

	require.NoError(t, err)
	require.Contains(t, addrs, netip.MustParseAddr("127.0.0.1"))
}

// resolver answers every query it receives with the query followed by answer, until the test ends.
func resolver(t *testing.T, answer string) netip.AddrPort {
	t.Helper()
	var lc net.ListenConfig
	pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(append(bytes.Clone(buf[:n]), answer...), from)
		}
	}()
	return netip.MustParseAddrPort(pc.LocalAddr().String())
}

// closedPort is a UDP port of the loopback nothing listens on: a query there is refused at once.
func closedPort(t *testing.T) netip.AddrPort {
	t.Helper()
	var lc net.ListenConfig
	pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := netip.MustParseAddrPort(pc.LocalAddr().String())
	require.NoError(t, pc.Close())
	return addr
}

func TestDNSRelaysTheQueryAsItIsToTheFirstResolverThatAnswers(t *testing.T) {
	t.Parallel()

	h := host(closedPort(t), resolver(t, "second"), resolver(t, "third"))

	got, err := h.DNS(t.Context(), []byte("query"))

	require.NoError(t, err)
	require.Equal(t, "querysecond", string(got))
}

func TestDNSFailsWhenNoResolverAnswers(t *testing.T) {
	t.Parallel()

	_, err := host().DNS(t.Context(), []byte("query"))
	require.ErrorContains(t, err, "no resolver on the host")

	_, err = host(closedPort(t)).DNS(t.Context(), []byte("query"))
	require.ErrorContains(t, err, "no resolver answered")
}
