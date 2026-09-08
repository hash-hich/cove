package sandbox_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// sandboxes are the VMs of cove in store: the running one, then the one kept after a stop.
var sandboxes = store[1:]

func TestWriteTable(t *testing.T) {
	t.Parallel()

	// Every column is padded to its widest cell plus two spaces, as container list does. The
	// stopped sandbox has neither address nor start date, and its line ends where its cells do.
	want := `ID            IMAGE               OS     ARCH   STATE    IP                 CPUS  MEMORY   STARTED
cove-fx-run   cove-sandbox:local  linux  arm64  running  192.168.64.152/24  4     1024 MB  2026-09-08T15:13:38Z
cove-fx-keep  cove-sandbox:local  linux  arm64  stopped                     4     1024 MB
`

	var buf bytes.Buffer
	sandbox.WriteTable(&buf, sandboxes)

	require.Equal(t, want, buf.String())
}

func TestWriteTableStoppedFirst(t *testing.T) {
	t.Parallel()

	// The columns are those of the whole table, not those of the rows above a stopped sandbox.
	want := `ID            IMAGE               OS     ARCH   STATE    IP                 CPUS  MEMORY   STARTED
cove-fx-keep  cove-sandbox:local  linux  arm64  stopped                     4     1024 MB
cove-fx-run   cove-sandbox:local  linux  arm64  running  192.168.64.152/24  4     1024 MB  2026-09-08T15:13:38Z
`

	var buf bytes.Buffer
	sandbox.WriteTable(&buf, []sandbox.VM{store[2], store[1]})

	require.Equal(t, want, buf.String())
}

func TestWriteTableEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sandbox.WriteTable(&buf, nil)

	require.Equal(t, "ID  IMAGE  OS  ARCH  STATE  IP  CPUS  MEMORY  STARTED\n", buf.String())
}

func TestWriteIDs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sandbox.WriteIDs(&buf, sandboxes)

	require.Equal(t, runSB+"\n"+keepSB+"\n", buf.String())
}

func TestWriteIDsEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sandbox.WriteIDs(&buf, nil)

	require.Empty(t, buf.String())
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	// The start date is absent from the stopped sandbox, and the sizes stay in bytes.
	want := `[
  {
    "id": "cove-fx-run",
    "image": "cove-sandbox:local",
    "os": "linux",
    "architecture": "arm64",
    "state": "running",
    "ipv4Address": "192.168.64.152/24",
    "cpus": 4,
    "memoryInBytes": 1073741824,
    "startedDate": "2026-09-08T15:13:38Z"
  },
  {
    "id": "cove-fx-keep",
    "image": "cove-sandbox:local",
    "os": "linux",
    "architecture": "arm64",
    "state": "stopped",
    "ipv4Address": "",
    "cpus": 4,
    "memoryInBytes": 1073741824
  }
]
`

	var buf bytes.Buffer
	sandbox.WriteJSON(&buf, sandboxes)

	require.Equal(t, want, buf.String())
}

func TestWriteJSONEmpty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	sandbox.WriteJSON(&buf, nil)

	// An empty array, never a null: the output is a list even when there is nothing in it.
	require.Equal(t, "[]\n", buf.String())
}
