package control_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vminit/control"
)

func TestExchange(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	require.NoError(t, control.Send(&buf, control.Request{Stop: &control.Stop{Grace: 10 * time.Second}}))
	require.NoError(t, control.Send(&buf, control.Step{Name: control.Received}))
	require.NoError(t, control.Send(&buf, control.Step{Name: control.ReadOnly, Error: "busy"}))

	r := control.NewReceiver(&buf)
	var req control.Request
	require.NoError(t, r.Receive(&req))
	require.Nil(t, req.Cancel)
	require.Equal(t, 10*time.Second, req.Stop.Grace)
	for _, want := range []control.Step{{Name: control.Received}, {Name: control.ReadOnly, Error: "busy"}} {
		var step control.Step
		require.NoError(t, r.Receive(&step))
		require.Equal(t, want, step)
	}
	require.ErrorIs(t, r.Receive(&control.Step{}), io.EOF, "the end of the connection is the VM powered off")
}

func TestReceiveRefusesAnUnknownField(t *testing.T) {
	t.Parallel()

	r := control.NewReceiver(strings.NewReader(`{"stop":{"grace":1,"signal":"SIGKILL"}}` + "\n"))

	require.ErrorContains(t, r.Receive(&control.Request{}), "unknown field")
}
