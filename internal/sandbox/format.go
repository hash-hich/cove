package sandbox

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
)

// bytesPerMB is the divisor of the MEMORY cell: container counts a MB as 2^20 bytes.
const bytesPerMB = 1 << 20

// columns are the headers of container list, in its order.
var columns = []string{"ID", "IMAGE", "OS", "ARCH", "STATE", "IP", "CPUS", "MEMORY", "STARTED"}

// WriteTable writes vms to w as container list prints them: the same columns, in the same order,
// each padded to its widest cell plus two spaces. An empty list writes the header alone.
//
// A write error is ignored here as everywhere cove writes to its own streams: a closed pipe
// (cove list | head) is not a failure of the command.
func WriteTable(w io.Writer, vms []VM) {
	var table strings.Builder
	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	writeRow(tw, columns)
	for _, vm := range vms {
		writeRow(tw, vm.cells())
	}
	_ = tw.Flush()
	// The cells of a stopped sandbox end in blanks, which tabwriter pads like any other; the
	// padding is dropped afterwards so that no line ends in spaces. Dropping the tabs instead
	// would end the column block there and align the rest of the table on its own widths.
	for line := range strings.Lines(table.String()) {
		_, _ = fmt.Fprintln(w, strings.TrimRight(line, " \n"))
	}
}

// WriteIDs writes the ID of each VM of vms to w, one per line, ready to be piped into a verb that
// takes targets.
func WriteIDs(w io.Writer, vms []VM) {
	for _, vm := range vms {
		_, _ = fmt.Fprintln(w, vm.ID)
	}
}

// WriteJSON writes vms to w as a JSON array of sandboxes, empty array included. The objects are
// cove's own: a container list entry carries the whole configuration of the VM, which cove does
// not promise and does not parse.
func WriteJSON(w io.Writer, vms []VM) {
	sandboxes := make([]sandboxJSON, 0, len(vms))
	for _, vm := range vms {
		sandboxes = append(sandboxes, sandboxJSON{
			ID:            vm.ID,
			Image:         vm.Image,
			OS:            vm.OS,
			Architecture:  vm.Architecture,
			State:         vm.State,
			IPv4Address:   vm.IPv4Address,
			CPUs:          vm.CPUs,
			MemoryInBytes: vm.MemoryInBytes,
			StartedDate:   started(vm),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(sandboxes)
}

// sandboxJSON is a sandbox as cove reports it. The numbers stay numbers and the sizes stay bytes,
// so that a consumer computes on them instead of parsing the cells of the table.
type sandboxJSON struct {
	ID            string `json:"id"`
	Image         string `json:"image"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	State         string `json:"state"`
	IPv4Address   string `json:"ipv4Address"`
	CPUs          int    `json:"cpus"`
	MemoryInBytes int64  `json:"memoryInBytes"`
	StartedDate   string `json:"startedDate,omitempty"`
}

// cells returns the table cells of v, in the order of columns.
func (v VM) cells() []string {
	return []string{
		v.ID,
		v.Image,
		v.OS,
		v.Architecture,
		v.State,
		v.IPv4Address,
		strconv.Itoa(v.CPUs),
		strconv.FormatInt(v.MemoryInBytes/bytesPerMB, 10) + " MB",
		started(v),
	}
}

// started returns the start date to report for v, empty unless it runs. Container keeps the date
// of the last start on a stopped VM, where it reads as an uptime that no longer exists.
func started(v VM) string {
	if !v.Running() {
		return ""
	}
	return v.StartedDate
}

// writeRow writes one tab-separated row, every cell terminated, so that each line takes part in
// the same column block whatever its last cells hold.
func writeRow(w io.Writer, cells []string) {
	_, _ = fmt.Fprintln(w, strings.Join(cells, "\t"))
}
