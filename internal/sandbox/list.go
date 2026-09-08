package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// VM is what cove reads of a container list entry: enough to tell its sandboxes from the other
// VMs of the store and the running ones from the stopped ones. Nothing else is parsed (D10).
type VM struct {
	// ID is the VM identifier, the name given at run or the one container generated.
	ID string
	// Labels are the labels given at run.
	Labels map[string]string
	// State is the container state, "running" or "stopped".
	State string
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
	} `json:"configuration"`
	Status struct {
		State string `json:"state"`
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
		vms = append(vms, VM{
			ID:     entry.Configuration.ID,
			Labels: entry.Configuration.Labels,
			State:  entry.Status.State,
		})
	}
	return vms, nil
}
