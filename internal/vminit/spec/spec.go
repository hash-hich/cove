// Package spec describes one boot of a sandbox: what the host decided for a run and what the
// init in the guest needs to make the VM habitable. The host writes it into the initramfs of the
// run, next to the init, and the init reads it before anything else is mounted: no disk, no
// channel and no command line carries it.
package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

// Path is where the description lies in the initramfs, and where the init reads it.
const Path = "/cove/run.json"

// Run is one boot of a sandbox. The fields of the image are copied from its configuration as the
// image declares them, the defaults left to the init: the rules on what cove does with them are
// stated once, where they are applied.
type Run struct {
	// Hostname is the name of the VM, given to sethostname and written in /etc/hostname.
	Hostname string `json:"hostname"`
	// Nameservers are the resolvers written in /etc/resolv.conf, none when empty.
	Nameservers []string `json:"nameservers,omitempty"`

	// Layers are the read only disks of the image, the highest first, attached in this order.
	Layers []Layer `json:"layers"`
	// MountOptions are the options the overlay of the layers must carry.
	MountOptions []string `json:"mountOptions"`
	// Write is the disk the run writes on, attached after the layers.
	Write Disk `json:"write"`

	// User, Env, WorkingDir, Volumes and StopSignal are the fields of the image configuration.
	User       string   `json:"user,omitempty"`
	Env        []string `json:"env,omitempty"`
	WorkingDir string   `json:"workingDir,omitempty"`
	Volumes    []string `json:"volumes,omitempty"`
	StopSignal string   `json:"stopSignal,omitempty"`

	// Project is the name of the repository, the directory of the project under the working
	// directory.
	Project string `json:"project"`
	// Agent is the program the agent is started with, looked up in the PATH of the image.
	Agent string `json:"agent"`
	// Command, when set, is started as the agent would be once the VM is ready, and the VM is
	// powered off when it ends, its exit code written on the console: the way to exercise the
	// init before a channel carries the turns.
	Command []string `json:"command,omitempty"`
}

// Disk is a disk attached to the VM. Disks carry no name the guest could read on every backend, so
// the init finds each one by its order of attachment and checks its size against Size.
type Disk struct {
	// Size is the size of the disk in bytes.
	Size int64 `json:"size"`
}

// Layer is a read only disk of the image.
type Layer struct {
	Disk
	// DiffID names the layer the disk holds, in what the init reports about it.
	DiffID string `json:"diffid"`
	// Mountpoint is where the init mounts the disk, outside the root of the image.
	Mountpoint string `json:"mountpoint"`
}

// Encode writes s as JSON to w.
func (s *Run) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return fmt.Errorf("write the boot description: %w", err)
	}
	return nil
}

// Read reads the description at path and returns it once it holds what a boot needs. An unknown
// field is refused: a host newer than its init must not have a field silently dropped.
func Read(file string) (*Run, error) {
	//nolint:gosec // G304: file is Path in the guest, a file the tests write otherwise.
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("open the boot description: %w", err)
	}
	defer func() { _ = f.Close() }()
	return Decode(f)
}

// Decode reads a description from r and checks it as Read does.
func Decode(r io.Reader) (*Run, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var s Run
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("read the boot description: %w", err)
	}
	if err := s.check(); err != nil {
		return nil, fmt.Errorf("the boot description %w", err)
	}
	return &s, nil
}

// check refuses what the init could not act on, or would act on outside the places it owns.
func (s *Run) check() error {
	if err := s.checkDisks(); err != nil {
		return err
	}
	if s.Hostname == "" {
		return errors.New("names no host")
	}
	if s.Agent == "" || strings.Contains(s.Agent, "/") {
		return fmt.Errorf("names the agent %q, not a program to look up in PATH", s.Agent)
	}
	if s.Project == "" || s.Project == "." || s.Project == ".." || strings.Contains(s.Project, "/") {
		return fmt.Errorf("names the project %q, not one segment of a path", s.Project)
	}
	return nil
}

func (s *Run) checkDisks() error {
	if len(s.Layers) == 0 {
		return errors.New("names no layer")
	}
	for _, l := range s.Layers {
		if l.Size <= 0 {
			return fmt.Errorf("gives layer %s no size", l.DiffID)
		}
		if !path.IsAbs(l.Mountpoint) || path.Clean(l.Mountpoint) != l.Mountpoint || l.Mountpoint == "/" {
			return fmt.Errorf("mounts layer %s on %q, not a clean absolute path", l.DiffID, l.Mountpoint)
		}
	}
	if s.Write.Size <= 0 {
		return errors.New("gives the write disk no size")
	}
	return nil
}
