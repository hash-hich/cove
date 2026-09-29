// Package hostexit is the exit of cove-net to the network of the host, the one it has until the
// cove daemon exists: it connects wherever the guest asks, but for the host itself and what is not
// an address of one machine, and relays the DNS of the guest to the resolvers of the host without
// decoding it.
package hostexit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"time"
)

// dnsPort is the port of the DNS, on the gateway and on the resolvers.
const dnsPort = 53

// resolverWait is how long a resolver is given to answer before the next one is asked.
const resolverWait = 2 * time.Second

// Host connects to the network of the host.
type Host struct {
	network     netip.Prefix
	gateway     netip.AddrPort
	nameservers []netip.AddrPort
	// hostAddrs returns the addresses of the host, read again at each connection.
	hostAddrs func() ([]netip.Addr, error)
}

// New returns the exit of a card whose gateway is gateway, which relays the DNS to nameservers, in
// their order.
func New(gateway netip.Prefix, nameservers []netip.AddrPort) *Host {
	return &Host{
		network: gateway.Masked(), gateway: netip.AddrPortFrom(gateway.Addr(), dnsPort),
		nameservers: nameservers, hostAddrs: interfaceAddrs,
	}
}

// RefusedError is a connection the exit refuses to open.
type RefusedError struct {
	// Reason says why, in a few words.
	Reason string
}

func (e *RefusedError) Error() string { return "refused: " + e.Reason }

// DialTCP connects to dst, or refuses to. The DNS of the gateway is connected to the first resolver.
func (h *Host) DialTCP(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	target, err := h.target(dst)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", target.String())
	if err != nil {
		return nil, err //nolint:wrapcheck // The dialer names the address, and the guest is told a reset.
	}
	return conn, nil
}

// target returns where a connection of the guest to dst goes, or a *RefusedError.
func (h *Host) target(dst netip.AddrPort) (netip.AddrPort, error) {
	if dst == h.gateway {
		if len(h.nameservers) == 0 {
			return netip.AddrPort{}, &RefusedError{Reason: "the DNS of the gateway, with no resolver on the host"}
		}
		return netip.AddrPortFrom(h.nameservers[0].Addr(), dnsPort), nil
	}
	reason, err := h.refusal(dst.Addr())
	if err != nil {
		return netip.AddrPort{}, err
	}
	if reason != "" {
		return netip.AddrPort{}, &RefusedError{Reason: reason}
	}
	return dst, nil
}

// limitedBroadcast is the broadcast of every network.
var limitedBroadcast = netip.AddrFrom4([4]byte{255, 255, 255, 255})

// thisNetwork is 0.0.0.0/8, which names no machine.
var thisNetwork = netip.MustParsePrefix("0.0.0.0/8")

// refusal returns why a connection to a is refused, empty when it is not. The host is refused on
// its loopback and on every address of its interfaces, read now since they change with the
// network the host joins: the host holds the files, the credentials and the services of the user,
// and a service that listens on every address answers on each of them.
func (h *Host) refusal(a netip.Addr) (string, error) {
	switch {
	case a.IsLoopback():
		return "the loopback of the host", nil
	case a.IsMulticast() || a == limitedBroadcast || thisNetwork.Contains(a):
		return "not the address of one machine", nil
	case h.network.Contains(a):
		return "the network of the card", nil
	}
	addrs, err := h.hostAddrs()
	if err != nil {
		// What cannot tell the host from another machine refuses.
		return "", fmt.Errorf("read the addresses of the host: %w", err)
	}
	if slices.Contains(addrs, a) {
		return "an address of the host", nil
	}
	return "", nil
}

// interfaceAddrs returns the IPv4 addresses of the interfaces of the host.
func interfaceAddrs() ([]netip.Addr, error) {
	ifaddrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err //nolint:wrapcheck // The caller names what it read.
	}
	var addrs []netip.Addr
	for _, ifa := range ifaddrs {
		ipnet, ok := ifa.(*net.IPNet)
		if !ok {
			continue
		}
		if a, ok := netip.AddrFromSlice(ipnet.IP); ok && a.Unmap().Is4() {
			addrs = append(addrs, a.Unmap())
		}
	}
	return addrs, nil
}

// DNS relays query to the resolvers, in their order, each given resolverWait, and returns the first
// answer as it came, truncated or not. It fails when there is no resolver, or when none answered.
func (h *Host) DNS(ctx context.Context, query []byte) ([]byte, error) {
	if len(h.nameservers) == 0 {
		return nil, errors.New("no resolver on the host")
	}
	var errs []error
	for _, ns := range h.nameservers {
		ans, err := ask(ctx, ns, query)
		if err == nil {
			return ans, nil
		}
		errs = append(errs, err)
	}
	return nil, fmt.Errorf("no resolver answered: %w", errors.Join(errs...))
}

// ask sends query to the resolver ns and returns its answer.
func ask(ctx context.Context, ns netip.AddrPort, query []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, resolverWait)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", ns.String())
	if err != nil {
		return nil, err //nolint:wrapcheck // The dialer names the resolver.
	}
	defer func() { _ = conn.Close() }()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(query); err != nil {
		return nil, err //nolint:wrapcheck // The socket names the resolver.
	}
	buf := make([]byte, 1<<16)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err //nolint:wrapcheck // The socket names the resolver.
	}
	return buf[:n], nil
}
