// Package sandbox launches the cove micro-VM through the container CLI.
package sandbox

import "strconv"

// Image is the sandbox image, built from images/sandbox and stored locally only (D9).
const Image = "cove-sandbox:local"

// LabelKey and LabelValue mark the VMs cove launched, so that cove can tell them apart from the
// other VMs of the host (Apple's builder VM carries com.apple.container.plugin=builder the same
// way). The key carries a role rather than "true" so that a future receiver or broker VM can use
// another value.
const (
	LabelKey   = "cove"
	LabelValue = "sandbox"
)

// Label is the label as container run takes it.
const Label = LabelKey + "=" + LabelValue

// Spec describes a sandbox to launch: the options the user may set on top of the fixed image and
// process.
type Spec struct {
	// Name is the VM name; empty lets container generate one.
	Name string
	// Keep leaves the stopped VM in place instead of removing it.
	Keep bool
	// CPUs is the number of vCPUs; zero leaves the container default.
	CPUs int
	// Memory is the memory limit with its suffix (512M, 4G); empty leaves the container default.
	Memory string
	// Env holds the KEY=VALUE or bare KEY (inherited from the host) entries to pass to the VM.
	Env []string
}

// Args returns the container run argument array for s.
//
// run creates the sandbox and nothing else: the VM starts detached and lives until stop, and the
// agent is driven by separate invocations, one claude process per turn. Process 1 of the VM is
// therefore a process that only waits, behind the init of container (--init). The init matters
// because Linux treats process 1 apart: a signal with a default action is not delivered to it, so
// sleep alone would ignore the SIGTERM of stop until the kill delay (5 s, then 137), and the
// orphans an exec session leaves behind are reparented to it and stay zombies unless it reaps
// them. The init forwards signals and reaps; sleep infinity, which GNU sleep accepts because it
// parses its argument as a float, is the placeholder child until cove has a process of its own in
// the VM. Observations and rationale: docs/cadrage/07-implementation.md, Pilotage.
//
// The array is where R1 and R2 hold: no volume, no mount, no user override, no working directory,
// no SSH agent, no network option can come from a Spec (README of the image).
func (s Spec) Args() []string {
	args := []string{"run", "-d"}
	if !s.Keep {
		args = append(args, "--rm")
	}
	args = append(args, "--init")
	if s.Name != "" {
		args = append(args, "--name", s.Name)
	}
	args = append(args, "--label", Label)
	if s.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(s.CPUs))
	}
	if s.Memory != "" {
		args = append(args, "--memory", s.Memory)
	}
	for _, env := range s.Env {
		args = append(args, "-e", env)
	}
	return append(args, Image, "sleep", "infinity")
}
