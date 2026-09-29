// Package relay is the TCP/IP stack behind the card of the VM. It ends every TCP connection of the
// guest and every DNS query sent to the gateway, and hands each one, unread, to an Exit: the
// stack, the link and the relaying stay the same whatever is behind the exit.
//
// The stack is gVisor's, with one NIC that answers ARP for the gateway and takes the packets of
// every address, since it stands for the whole network: IPv4, ARP, TCP, and UDP for the DNS of the
// gateway alone. Other UDP finds no endpoint, and the guest is told at once by an ICMP port
// unreachable rather than left waiting.
package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sync"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"

	"gitlab.com/hich-hich/cove/cmd/cove-net/internal/card"
)

// Exit is what is behind the stack, the only way out of it.
type Exit interface {
	// DialTCP connects to dst; an error becomes a reset for the guest.
	DialTCP(ctx context.Context, dst netip.AddrPort) (net.Conn, error)
	// DNS answers a query, the bytes as the guest wrote them; an error becomes a SERVFAIL.
	DNS(ctx context.Context, query []byte) ([]byte, error)
}

// GatewayMAC is the address the NIC answers ARP with, the same for every VM: each VM has a link of
// its own.
const GatewayMAC = tcpip.LinkAddress("\x5a\x94\xef\xe4\x0c\xef")

// DNSPort is the port of the gateway the guest sends its queries to.
const DNSPort = 53

// nic is the one NIC of the stack.
const nic tcpip.NICID = 1

// maxHandshakes bounds the connections of the guest whose exit is being dialed; a SYN past it is
// dropped, and the guest sends it again.
const maxHandshakes = 1024

// maxQueries bounds the DNS queries being answered; a query past it is dropped, and the guest asks
// again.
const maxQueries = 256

// Config is the stack of one card.
type Config struct {
	// Gateway is the address of the NIC on the network of the card, with its prefix.
	Gateway netip.Prefix
	// Exit is where the connections and the queries of the guest go.
	Exit Exit
	// Log receives a line for each connection or query that failed, and why.
	Log *log.Logger
}

// Serve runs the stack of cfg on the card of conn until the card ends, and closes conn. It returns
// nil when the other end closed the card, and what ended it otherwise, as card.Link.Run does. The
// connections in progress are cut: there is nothing to flush.
func Serve(ctx context.Context, conn io.ReadWriteCloser, cfg Config) error {
	link := card.NewLink(conn, GatewayMAC)
	s, err := newStack(link.Endpoint(), cfg.Gateway.Addr())
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer s.Destroy()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r := &relay{ctx: ctx, exit: cfg.Exit, log: cfg.Log, queries: make(chan struct{}, maxQueries)}
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, tcp.NewForwarder(s, 0, maxHandshakes, r.tcp).HandlePacket)
	dns, err := gonet.DialUDP(s, &tcpip.FullAddress{
		NIC: nic, Addr: tcpip.AddrFrom4(cfg.Gateway.Addr().As4()), Port: DNSPort,
	}, nil, ipv4.ProtocolNumber)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("listen for the DNS of the gateway: %w", err)
	}
	var wg sync.WaitGroup
	wg.Go(func() { r.serveDNS(dns) })
	err = link.Run(ctx)
	// The dials and queries in progress end with the card.
	cancel()
	_ = dns.Close()
	wg.Wait()
	return err //nolint:wrapcheck // Run names what ended the card.
}

// newStack returns a stack whose one NIC, on ep, is gateway on its link and every other address
// beyond it.
func newStack(ep stack.LinkEndpoint, gateway netip.Addr) (*stack.Stack, error) {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	for _, step := range []struct {
		what string
		do   func() tcpip.Error
	}{
		{"create the NIC", func() tcpip.Error { return s.CreateNIC(nic, ep) }},
		// Promiscuous, the NIC takes the packets of every address; spoofing, it answers from them.
		{"take every address", func() tcpip.Error { return s.SetPromiscuousMode(nic, true) }},
		{"answer from every address", func() tcpip.Error { return s.SetSpoofing(nic, true) }},
		{"give the NIC the gateway", func() tcpip.Error {
			return s.AddProtocolAddress(nic, tcpip.ProtocolAddress{
				Protocol: ipv4.ProtocolNumber, AddressWithPrefix: tcpip.AddrFrom4(gateway.As4()).WithPrefix(),
			}, stack.AddressProperties{})
		}},
	} {
		if err := step.do(); err != nil {
			s.Destroy()
			return nil, fmt.Errorf("%s: %s", step.what, err)
		}
	}
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: nic}})
	return s, nil
}

type relay struct {
	ctx  context.Context //nolint:containedctx // The life of the card, which the forwarder has no way to pass.
	exit Exit
	log  *log.Logger
	// queries holds a place for each query being answered.
	queries chan struct{}
}

// tcp dials the exit of the connection req asks for, then completes the handshake with the guest,
// or resets it when the exit could not connect, as a real network does, and copies both ways until
// both sides closed. The forwarder calls it on a goroutine of its own.
func (r *relay) tcp(req *tcp.ForwarderRequest) {
	id := req.ID()
	src := netip.AddrPortFrom(netip.AddrFrom4(id.RemoteAddress.As4()), id.RemotePort)
	dst := netip.AddrPortFrom(netip.AddrFrom4(id.LocalAddress.As4()), id.LocalPort)
	out, err := r.exit.DialTCP(r.ctx, dst)
	if err != nil {
		r.log.Printf("tcp %s -> %s: %v", src, dst, err)
		req.Complete(true)
		return
	}
	var wq waiter.Queue
	ep, terr := req.CreateEndpoint(&wq)
	if terr != nil {
		r.log.Printf("tcp %s -> %s: accept: %s", src, dst, terr)
		req.Complete(true)
		_ = out.Close()
		return
	}
	req.Complete(false)
	splice(gonet.NewTCPConn(&wq, ep), out)
}

// splice copies a to b and b to a. A side that closes its write half has the other side's write
// half closed in turn; a side that fails ends both.
func splice(a, b net.Conn) {
	var wg sync.WaitGroup
	cp := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		if err != nil {
			_ = a.Close()
			_ = b.Close()
			return
		}
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}
	wg.Go(func() { cp(a, b) })
	wg.Go(func() { cp(b, a) })
	wg.Wait()
	_ = a.Close()
	_ = b.Close()
}

// serveDNS answers each query conn receives, until it is closed.
func (r *relay) serveDNS(conn *gonet.UDPConn) {
	var wg sync.WaitGroup
	defer wg.Wait()
	buf := make([]byte, header.UDPMaximumPacketSize)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		select {
		case r.queries <- struct{}{}:
		default:
			r.log.Printf("dns %s: %d queries already waiting, dropped", from, maxQueries)
			continue
		}
		query := bytes.Clone(buf[:n])
		wg.Go(func() {
			defer func() { <-r.queries }()
			r.answer(conn, from, query)
		})
	}
}

// answer sends from the answer of the exit to query, or a SERVFAIL when the exit has none.
func (r *relay) answer(conn *gonet.UDPConn, from net.Addr, query []byte) {
	ans, err := r.exit.DNS(r.ctx, query)
	if err != nil {
		r.log.Printf("dns %s: %v", from, err)
		if ans, err = ServFail(query); err != nil {
			return
		}
	}
	_, _ = conn.WriteTo(ans, from)
}

// dnsHeader is the size of the header of a DNS message.
const dnsHeader = 12

// ServFail returns the answer SERVFAIL to query, the query itself with two bytes of its header
// rewritten: the flag of an answer set, and the code of the answer 2. It fails on what is too
// short to be a query.
func ServFail(query []byte) ([]byte, error) {
	if len(query) < dnsHeader {
		return nil, errors.New("a query shorter than a header")
	}
	ans := bytes.Clone(query)
	ans[2] |= 0x80
	ans[3] = ans[3]&0xf0 | 2
	return ans, nil
}
