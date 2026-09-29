package hostexit

import "net/netip"

// TargetOf exposes target to the tests of the package.
func (h *Host) TargetOf(dst netip.AddrPort) (netip.AddrPort, error) { return h.target(dst) }
