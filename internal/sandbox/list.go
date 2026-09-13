package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// VM is what cove reads of a container list entry: enough to tell its sandboxes from the other
// VMs of the store, and the cells container list shows for them. The rest of the entry, which
// carries the whole configuration of the VM, is not parsed.
type VM struct {
	// ID is the VM identifier, the name given at run or the one container generated.
	ID string
	// Labels are the labels given at run.
	Labels map[string]string
	// State is the container state, "running" or "stopped".
	State string
	// Image is the image reference the VM was created from.
	Image string
	// OS and Architecture are the platform of the image, "linux" and "arm64" for a sandbox.
	OS           string
	Architecture string
	// IPv4Address is the address of the VM on its network, with its prefix length; empty once the
	// VM is stopped, since container then reports no network.
	IPv4Address string
	// CPUs is the number of vCPUs, and MemoryInBytes the memory limit.
	CPUs          int
	MemoryInBytes int64
	// StartedDate is when the VM was last started, in RFC 3339. Container keeps it on a stopped
	// VM, where it no longer describes anything.
	StartedDate string
}

// IsSandbox reports whether cove launched v: only these VMs may be stopped by cove, since the
// store is shared with VMs that are not its own, such as Apple's builder.
func (v VM) IsSandbox() bool {
	return v.Labels[LabelKey] == LabelValue
}

// Running reports whether v runs.
func (v VM) Running() bool {
	return v.State == "running"
}

// List returns every VM of the store, running or not, as container list reports them. It returns
// ErrNotInstalled, a failure to execute the CLI, its error when it fails, or a parse error.
func List(ctx context.Context) ([]VM, error) {
	bin, err := lookPath()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	//nolint:gosec // G204: bin comes from LookPath and the arguments are fixed.
	cmd := exec.CommandContext(ctx, bin, "list", "--all", "--format", "json")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list VMs: %s: %w", strings.TrimSpace(stderr.String()), err)
	}
	return parseList(out)
}

// listEntry is the shape of a container list entry, restricted to what VM keeps.
type listEntry struct {
	Configuration struct {
		ID     string            `json:"id"`
		Labels map[string]string `json:"labels"`
		Image  struct {
			Reference string `json:"reference"`
		} `json:"image"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
		Resources struct {
			CPUs          int   `json:"cpus"`
			MemoryInBytes int64 `json:"memoryInBytes"`
		} `json:"resources"`
	} `json:"configuration"`
	Status struct {
		State       string `json:"state"`
		StartedDate string `json:"startedDate"`
		// Networks is empty on a stopped VM; container shows the address of the first one.
		Networks []struct {
			IPv4Address string `json:"ipv4Address"`
		} `json:"networks"`
	} `json:"status"`
}

// parseList turns the JSON of container list into VMs.
func parseList(data []byte) ([]VM, error) {
	var entries []listEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse container list: %w", err)
	}
	vms := make([]VM, 0, len(entries))
	for _, entry := range entries {
		vm := VM{
			ID:            entry.Configuration.ID,
			Labels:        entry.Configuration.Labels,
			State:         entry.Status.State,
			Image:         entry.Configuration.Image.Reference,
			OS:            entry.Configuration.Platform.OS,
			Architecture:  entry.Configuration.Platform.Architecture,
			CPUs:          entry.Configuration.Resources.CPUs,
			MemoryInBytes: entry.Configuration.Resources.MemoryInBytes,
			StartedDate:   entry.Status.StartedDate,
		}
		if len(entry.Status.Networks) > 0 {
			vm.IPv4Address = entry.Status.Networks[0].IPv4Address
		}
		vms = append(vms, vm)
	}
	return vms, nil
}

// Screening splits the targets of a verb between those cove may act on and those it refuses.
type Screening struct {
	// Kept are the targets to hand to container, in the order given.
	Kept []string
	// Refused are the targets that name a VM of the store that is not a sandbox of cove.
	Refused []string
}

// Screen sorts names into a Screening: cove never touches a VM it did not launch. A name that
// matches no VM is kept: container resolves IDs itself and reports the unknown ones, so cove does
// not second-guess it.
func Screen(vms []VM, names []string) Screening {
	known := make(map[string]VM, len(vms))
	for _, vm := range vms {
		known[vm.ID] = vm
	}
	var s Screening
	for _, name := range names {
		if vm, ok := known[name]; ok && !vm.IsSandbox() {
			s.Refused = append(s.Refused, name)
			continue
		}
		s.Kept = append(s.Kept, name)
	}
	return s
}

// Sandboxes returns the VMs of vms that cove launched, in the order given: the ones that run,
// and the stopped ones too when all. The store is shared with VMs that are not cove's, and those
// are never its business.
func Sandboxes(vms []VM, all bool) []VM {
	kept := make([]VM, 0, len(vms))
	for _, vm := range vms {
		if vm.IsSandbox() && (all || vm.Running()) {
			kept = append(kept, vm)
		}
	}
	return kept
}

// Running returns the IDs of the sandboxes of cove that run: what "all" means for cove, since the
// --all of container would reach the other VMs of the store.
func Running(vms []VM) []string {
	sandboxes := Sandboxes(vms, false)
	ids := make([]string, 0, len(sandboxes))
	for _, vm := range sandboxes {
		ids = append(ids, vm.ID)
	}
	return ids
}
