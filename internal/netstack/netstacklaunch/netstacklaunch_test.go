package netstacklaunch_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/netstack/netstacklaunch"
)

func TestParseResolvConfKeepsTheNameserversInOrder(t *testing.T) {
	t.Parallel()

	conf := `#
# macOS Notice
#
search home
nameserver 192.168.1.254
nameserver fe80::1%en0
nameserver not-an-address
  nameserver   9.9.9.9  
options ndots:1
`

	got, err := netstacklaunch.ParseResolvConf(strings.NewReader(conf))

	require.NoError(t, err)
	require.Equal(t, []netip.AddrPort{
		netip.MustParseAddrPort("192.168.1.254:53"),
		netip.MustParseAddrPort("[fe80::1%en0]:53"),
		netip.MustParseAddrPort("9.9.9.9:53"),
	}, got)
}
