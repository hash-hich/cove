package hostexit

import "net/netip"

// TargetOf exposes target to the tests of the package.
func (h *Host) TargetOf(dst netip.AddrPort) (netip.AddrPort, error) { return h.target(dst) }

// SetHostAddrs gives h the addresses of a host of the tests.
func (h *Host) SetHostAddrs(addrs func() ([]netip.Addr, error)) { h.hostAddrs = addrs }

// InterfaceAddrs exposes interfaceAddrs to the tests of the package.
var InterfaceAddrs = interfaceAddrs
