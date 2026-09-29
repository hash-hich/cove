package relay_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"

	"gitlab.com/hich-hich/cove/cmd/cove-net/internal/card"
	"gitlab.com/hich-hich/cove/cmd/cove-net/internal/relay"
)

// The addresses of the card, as cove gives them.
var (
	gateway = netip.MustParsePrefix("10.0.2.1/30")
	guestIP = netip.MustParseAddr("10.0.2.2")
)

// guestMAC is the address of the card in the guest.
const guestMAC = tcpip.LinkAddress("\x5a\x94\xef\xe4\x0c\xee")

// exit is the exit of a test: it connects every destination to the listener of the test and
// relays every query to the DNS server of the test, and records what it was asked. A destination
// in refuse is refused.
type exit struct {
	listener string
	dns      string
	refuse   netip.AddrPort

	mu     sync.Mutex
	dialed []netip.AddrPort
}

func (e *exit) DialTCP(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	e.mu.Lock()
	e.dialed = append(e.dialed, dst)
	e.mu.Unlock()
	if dst == e.refuse {
		return nil, errors.New("refused by the test")
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", e.listener)
	if err != nil {
		return nil, fmt.Errorf("dial the listener of the test: %w", err)
	}
	return c, nil
}

func (e *exit) DNS(ctx context.Context, query []byte) ([]byte, error) {
	if e.dns == "" {
		return nil, errors.New("no resolver")
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "udp", e.dns)
	if err != nil {
		return nil, fmt.Errorf("dial the resolver of the test: %w", err)
	}
	defer func() { _ = c.Close() }()
	if _, err := c.Write(query); err != nil {
		return nil, fmt.Errorf("ask the resolver of the test: %w", err)
	}
	buf := make([]byte, 512)
	n, err := c.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read the resolver of the test: %w", err)
	}
	return buf[:n], nil
}

// socketpair returns the two ends of a card, a pair of connected Unix stream sockets as the one of
// libkrun.
func socketpair(t *testing.T) [2]net.Conn {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	require.NoError(t, err)
	var ends [2]net.Conn
	for i, fd := range fds {
		f := os.NewFile(uintptr(fd), "card")
		ends[i], err = net.FileConn(f)
		_ = f.Close()
		require.NoError(t, err)
	}
	return ends
}

// serve runs cove-net on conn, e behind it, and returns what Serve returns once it did.
func serve(t *testing.T, conn net.Conn, e relay.Exit) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- relay.Serve(t.Context(), conn, relay.Config{Gateway: gateway, Exit: e, Log: log.New(io.Discard, "", 0)})
	}()
	return done
}

// run starts cove-net on one end of a card, e behind it, and returns the stack of a guest on the
// other end, its address and route set as the init sets them.
func run(t *testing.T, e relay.Exit) *stack.Stack {
	t.Helper()
	pair := socketpair(t)
	ours := pair[0]
	done := serve(t, pair[1], e)

	link := card.NewLink(ours, guestMAC)
	guest := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	require.Nil(t, guest.CreateNIC(1, link.Endpoint()))
	require.Nil(t, guest.AddProtocolAddress(1, tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{Address: tcpip.AddrFrom4(guestIP.As4()), PrefixLen: 30},
	}, stack.AddressProperties{}))
	guest.SetRouteTable([]tcpip.Route{{
		Destination: header.IPv4EmptySubnet, Gateway: tcpip.AddrFrom4(gateway.Addr().As4()), NIC: 1,
	}})
	ctx, cancel := context.WithCancel(context.Background())
	linkDone := make(chan struct{})
	go func() {
		_ = link.Run(ctx)
		close(linkDone)
	}()
	t.Cleanup(func() {
		cancel()
		<-linkDone
		guest.Destroy()
		<-done
	})
	return guest
}

func full(a netip.AddrPort) tcpip.FullAddress {
	return tcpip.FullAddress{Addr: tcpip.AddrFrom4(a.Addr().As4()), Port: a.Port()}
}

func TestRelayCarriesAConnectionBothWaysAndItsHalfClose(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	server := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		got, _ := io.ReadAll(c)
		server <- string(got)
		_, _ = c.Write([]byte("pong"))
	}()
	e := &exit{listener: ln.Addr().String()}
	guest := run(t, e)
	dst := netip.MustParseAddrPort("203.0.113.7:443")

	c, err := gonet.DialContextTCP(t.Context(), guest, full(dst), ipv4.ProtocolNumber)
	require.NoError(t, err)
	_, err = c.Write([]byte("ping"))
	require.NoError(t, err)
	require.NoError(t, c.CloseWrite())

	require.Equal(t, "ping", <-server, "the server read up to the half close of the guest")
	got, err := io.ReadAll(c)
	require.NoError(t, err)
	require.Equal(t, "pong", string(got), "the guest read what the server wrote after, up to its close")
	require.Equal(t, []netip.AddrPort{dst}, e.dialed, "the exit is asked for the destination of the guest")
}

func TestRelayResetsAConnectionTheExitRefuses(t *testing.T) {
	t.Parallel()

	dst := netip.MustParseAddrPort("203.0.113.7:80")
	guest := run(t, &exit{refuse: dst})

	_, err := gonet.DialContextTCP(t.Context(), guest, full(dst), ipv4.ProtocolNumber)

	require.ErrorContains(t, err, "connection was refused", "a reset, as a real network gives")
}

// query is a DNS query for example.com, type A.
var query = []byte{
	0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0,
	7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0, 1, 0, 1,
}

// ask sends query from the guest to dst over UDP, and returns what came back.
func ask(t *testing.T, guest *stack.Stack, dst netip.AddrPort) ([]byte, error) {
	t.Helper()
	remote := full(dst)
	c, err := gonet.DialUDP(guest, nil, &remote, ipv4.ProtocolNumber)
	require.NoError(t, err)
	defer func() { _ = c.Close() }()
	require.NoError(t, c.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = c.Write(query)
	require.NoError(t, err)
	buf := make([]byte, 512)
	n, err := c.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read the answer: %w", err)
	}
	return buf[:n], nil
}

func TestRelayCarriesAQueryToTheGatewayAsItIs(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = pc.Close() })
	answer := append(bytes.Clone(query), "answer"...)
	received := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 512)
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		received <- bytes.Clone(buf[:n])
		_, _ = pc.WriteTo(answer, from)
	}()
	guest := run(t, &exit{dns: pc.LocalAddr().String()})

	got, err := ask(t, guest, netip.AddrPortFrom(gateway.Addr(), relay.DNSPort))

	require.NoError(t, err)
	require.Equal(t, query, <-received, "the resolver received the query of the guest, byte for byte")
	require.Equal(t, answer, got, "the guest received the answer of the resolver, byte for byte")
}

func TestRelayAnswersServFailWithoutAResolver(t *testing.T) {
	t.Parallel()

	guest := run(t, &exit{})

	got, err := ask(t, guest, netip.AddrPortFrom(gateway.Addr(), relay.DNSPort))

	require.NoError(t, err)
	want := bytes.Clone(query)
	want[2], want[3] = 0x81, 0x02
	require.Equal(t, want, got, "the query, flagged an answer, code SERVFAIL")
}

func TestRelayRefusesUDPOtherThanTheDNSOfTheGateway(t *testing.T) {
	t.Parallel()

	guest := run(t, &exit{dns: "127.0.0.1:1"})
	unreachable := guest.Stats().ICMP.V4.PacketsReceived.DstUnreachable

	for _, dst := range []string{"8.8.8.8:53", "10.0.2.1:5353"} {
		before := unreachable.Value()
		remote := full(netip.MustParseAddrPort(dst))
		c, err := gonet.DialUDP(guest, nil, &remote, ipv4.ProtocolNumber)
		require.NoError(t, err)
		_, err = c.Write(query)
		require.NoError(t, err)
		// gonet does not report the error a Linux guest reads as ECONNREFUSED: the ICMP is counted.
		require.Eventually(t, func() bool { return unreachable.Value() == before+1 }, time.Second,
			10*time.Millisecond, "%s: a port unreachable, at once", dst)
		_ = c.Close()
	}
}

func TestServeEndsWhenTheCardCloses(t *testing.T) {
	t.Parallel()

	pair := socketpair(t)
	ours := pair[0]
	done := serve(t, pair[1], &exit{})

	require.NoError(t, ours.Close())

	require.NoError(t, <-done, "the VM went, cove-net ends without an error")
}

func TestServeFailsOnAFrameTheCardDoesNotCarry(t *testing.T) {
	t.Parallel()

	pair := socketpair(t)
	ours := pair[0]
	done := serve(t, pair[1], &exit{})

	_, _ = ours.Write([]byte{0, 0, 0, 0})

	require.ErrorIs(t, <-done, card.ErrFrame)
	_ = ours.Close()
}

func TestServFailRewritesTwoBytesOfTheHeader(t *testing.T) {
	t.Parallel()

	got, err := relay.ServFail(query)
	require.NoError(t, err)
	require.Equal(t, query[:2], got[:2])
	require.Equal(t, []byte{0x81, 0x02}, got[2:4])
	require.Equal(t, query[4:], got[4:])

	_, err = relay.ServFail(query[:11])
	require.Error(t, err)
}
