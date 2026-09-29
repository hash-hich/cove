package netstackproto_test

import (
	"bytes"
	"io"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/netstack/netstackproto"
	"gitlab.com/hich-hich/cove/internal/process"
)

func TestMessagesGoThroughInTheirOrder(t *testing.T) {
	t.Parallel()

	var pipe bytes.Buffer
	hello := netstackproto.Hello{Build: "b"}
	cfg := netstackproto.Config{
		Socket: "/s/card.sock", Gateway: netip.MustParsePrefix("10.0.2.1/30"),
		Nameservers:   []netip.AddrPort{netip.MustParseAddrPort("192.168.1.254:53")},
		AcceptTimeout: time.Minute,
	}
	status := netstackproto.Status{Error: "no"}
	require.NoError(t, process.Send(&pipe, hello))
	require.NoError(t, process.Send(&pipe, cfg))
	require.NoError(t, process.Send(&pipe, status))

	r := process.NewReceiver(&pipe)
	var (
		gotHello  netstackproto.Hello
		gotCfg    netstackproto.Config
		gotStatus netstackproto.Status
	)
	require.NoError(t, r.Receive(&gotHello))
	require.NoError(t, r.Receive(&gotCfg))
	require.NoError(t, r.Receive(&gotStatus))

	require.Equal(t, hello, gotHello)
	require.Equal(t, cfg, gotCfg)
	require.Equal(t, status, gotStatus)
	require.ErrorIs(t, r.Receive(&gotStatus), io.EOF, "the other side closed its end")
}

func TestReceiveRefusesAFieldItDoesNotKnow(t *testing.T) {
	t.Parallel()

	r := process.NewReceiver(strings.NewReader(`{"socket":"/s","policy":"open"}` + "\n"))

	var cfg netstackproto.Config
	err := r.Receive(&cfg)

	require.ErrorContains(t, err, "policy", "the two sides come from one build, a field one lacks is a mismatch")
}
