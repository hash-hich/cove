// Package netstackproto is how cove starts cove-net, the process that holds the TCP/IP stack
// behind the network card of one VM: the card it serves and the exchange that carries it.
//
// cove starts cove-net as the package process describes, before cove-vmm: libkrun connects to the
// socket of the card when the driver of the guest starts, and fails when nothing listens. Three
// JSON messages follow, one each way then one back: cove-net says who it is in a Hello, cove gives
// it the card in a Config, and cove-net answers with a Status once it is confined and listens, or
// with why it could not. A cove-net of another build than cove is refused on its Hello.
package netstackproto

import (
	"net/netip"
	"time"
)

// Hello is what cove-net says first.
type Hello struct {
	// Build is the build of cove-net, which must be the build of cove.
	Build string `json:"build"`
}

// Config is the card cove-net serves.
type Config struct {
	// Socket is the path of the Unix stream socket of the card, absolute and resolved, where
	// cove-net listens and cove-vmm connects. Nothing may be there yet.
	Socket string `json:"socket"`
	// Gateway is the address of cove-net on the network of the card, with its prefix.
	Gateway netip.Prefix `json:"gateway"`
	// Nameservers are the resolvers of the host the DNS of the gateway is relayed to, in order.
	Nameservers []netip.AddrPort `json:"nameservers,omitempty"`
	// AcceptTimeout is how long cove-net waits for cove-vmm to connect before it ends.
	AcceptTimeout time.Duration `json:"acceptTimeout"`
}

// Status is the answer of cove-net to a Config: it listens on the card, or it could not.
type Status struct {
	// Error says why cove-net could not listen; empty when it does.
	Error string `json:"error,omitempty"`
}
