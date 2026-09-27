// Package inventory keeps the set of the sandboxes of this machine: one directory per sandbox under
// a root of cove, with the description of the sandbox and the lock its VM holds by living. It knows
// which sandboxes exist and whether their VM runs, and nothing of what a sandbox holds.
//
// A sandbox is cove's because its directory is under the root, which only cove writes, and its VM
// runs because the cove-vmm cove started holds its lock: neither rests on a name, and neither is a
// state that someone must remember to write. A VM that ends by any means, a crash, a kill -9 or a
// reboot of the host included, leaves its lock free, and the entry reads as stopped, which is true.
//
// The directories of the root appear and go under a lock of the root, which is also what two runs
// that want one name wait on. What runs in them is read without it.
package inventory

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gitlab.com/hich-hich/cove/internal/filelock"
)

// The files the inventory keeps, the lock of the root beside the directories of the sandboxes, and
// the description and the lock of each sandbox in its directory. The description is metadata.json,
// a name that does not invite a person to edit it.
const (
	lockFile     = "lock"
	metadataFile = "metadata.json"
)

// Description is what cove knows of a sandbox: what run asked for and what it got. Only cove writes
// it, when it creates the sandbox and whenever the sandbox changes.
type Description struct {
	// ID names the sandbox and its directory; Name is the name of its VM, unique among the
	// sandboxes of the root, stopped ones included.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Image is the image as it was asked for, Digest its manifest in the store.
	Image  string `json:"image"`
	Digest string `json:"digest"`
	// Repository is the URL of the repository, Branch the one asked for, empty for its default.
	Repository string `json:"repository"`
	Branch     string `json:"branch,omitempty"`
	// Created is when cove began to create the sandbox.
	Created time.Time `json:"created"`
	// CPUs and MemoryMiB are the resources of the VM, Disk the size in bytes its write disk got,
	// which the free space of the host may have made smaller than the one asked for.
	CPUs      uint8  `json:"cpus"`
	MemoryMiB uint32 `json:"memoryMiB"`
	Disk      int64  `json:"disk"`
}

// State is whether the VM of a sandbox runs.
type State string

// The states of a sandbox.
const (
	// Running is a sandbox whose lock is held: by the cove that creates it until its cove-vmm
	// starts, then by that cove-vmm.
	Running State = "running"
	// Stopped is a sandbox whose lock nobody holds, whatever ended its VM or its creation.
	Stopped State = "stopped"
	// Unknown is a sandbox whose directory has no lock, so that nothing says whether a VM runs: one
	// created before cove kept an inventory, whose VM may still run, a creation killed between its
	// directory and its lock, or a directory changed by hand.
	Unknown State = "unknown"
)

// Entry is a sandbox as it is found on the host.
type Entry struct {
	// Description is what its metadata.json says, empty but for the ID when Err is set.
	Description
	// Dir is its directory, State whether its VM runs.
	Dir   string
	State State
	// Err says why its description could not be read, without the path, which Dir gives: ErrNoMetadata
	// for a creation that ended before it was written or a sandbox older than the inventory, and
	// another error for a directory that someone other than cove changed. The entry is reported
	// all the same.
	Err error
}

// ErrNoMetadata is the Err of an entry whose directory holds no metadata.json.
var ErrNoMetadata = errors.New("no " + metadataFile)

// ErrNameInUse is returned by Add when another sandbox of the root carries the name.
var ErrNameInUse = errors.New("name already in use")

// Add creates the directory of the sandbox desc describes under root, writes its description and takes
// its lock, and returns both: the caller hands the lock to the cove-vmm of the sandbox, or removes
// the sandbox with Remove. It returns ErrNameInUse when a sandbox of root, running or not, carries
// the name of desc, since a name is what the other verbs find a sandbox by.
func Add(ctx context.Context, root string, desc Description) (string, *filelock.Lock, error) {
	unlock, err := lockRoot(ctx, root)
	if err != nil {
		return "", nil, err
	}
	defer unlock()
	entries, err := read(root)
	if err != nil {
		return "", nil, err
	}
	for _, e := range entries {
		if e.Err == nil && e.Name == desc.Name {
			return "", nil, fmt.Errorf("%w: %s is the name of sandbox %s", ErrNameInUse, desc.Name, e.ID)
		}
	}
	dir := filepath.Join(root, desc.ID)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", nil, fmt.Errorf("create the directory of the sandbox: %w", err)
	}
	lock, err := enter(dir, desc)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	return dir, lock, nil
}

// enter takes the lock of the sandbox in dir, then writes its description: a sandbox that has one is
// running from the moment it is seen.
func enter(dir string, desc Description) (*filelock.Lock, error) {
	lock, ok, err := filelock.Try(filepath.Join(dir, lockFile))
	if err != nil {
		return nil, err //nolint:wrapcheck // Try names the lock.
	}
	if !ok {
		return nil, fmt.Errorf("the lock of %s is held by another process", dir)
	}
	out, err := json.Marshal(desc)
	if err != nil {
		lock.Release()
		return nil, fmt.Errorf("encode the description of the sandbox: %w", err)
	}
	if err := writeFile(filepath.Join(dir, metadataFile), out); err != nil {
		lock.Release()
		return nil, err
	}
	return lock, nil
}

// Remove removes the sandbox id of root, its directory and all it holds, under the lock of the
// root so that no one lists it half removed. It is for a sandbox whose VM does not run.
func Remove(ctx context.Context, root, id string) error {
	unlock, err := lockRoot(ctx, root)
	if err != nil {
		return err
	}
	defer unlock()
	if err := os.RemoveAll(filepath.Join(root, id)); err != nil {
		return fmt.Errorf("remove sandbox %s: %w", id, err)
	}
	return nil
}

// List returns every sandbox of root, the latest created first, and those whose description cannot be
// read last. A root that does not exist holds no sandbox, and List does not create it.
func List(ctx context.Context, root string) ([]Entry, error) {
	if _, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	unlock, err := lockRoot(ctx, root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	entries, err := read(root)
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(entries, func(a, b Entry) int {
		if (a.Err == nil) != (b.Err == nil) {
			return cmp.Compare(boolInt(a.Err != nil), boolInt(b.Err != nil))
		}
		return cmp.Or(b.Created.Compare(a.Created), cmp.Compare(a.ID, b.ID))
	})
	return entries, nil
}

// read returns the sandboxes of root, each directory as it is found. The caller holds the lock of
// the root.
func read(root string) ([]Entry, error) {
	dirents, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read the sandboxes: %w", err)
	}
	var entries []Entry
	for _, d := range dirents {
		if !d.IsDir() {
			continue
		}
		e := Entry{Dir: filepath.Join(root, d.Name())}
		e.State, err = state(filepath.Join(e.Dir, lockFile))
		if err != nil {
			return nil, fmt.Errorf("look at sandbox %s: %w", d.Name(), err)
		}
		e.Description, e.Err = readDescription(e.Dir)
		// The directory names the sandbox whatever its description says, so that it can be found.
		e.ID = d.Name()
		entries = append(entries, e)
	}
	return entries, nil
}

// state returns the state of the sandbox whose lock is at path. The lock is created before
// anything else, under the lock of the root that the caller holds, so a missing one is not a VM
// that stopped: nothing says what runs.
func state(path string) (State, error) {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return Unknown, nil
	} else if err != nil {
		return "", fmt.Errorf("look at the lock: %w", err)
	}
	held, err := filelock.Held(path)
	if err != nil {
		return "", err //nolint:wrapcheck // Held names the lock.
	}
	if held {
		return Running, nil
	}
	return Stopped, nil
}

// readDescription reads the description of the sandbox in dir, which must name the directory it is
// in.
func readDescription(dir string) (Description, error) {
	var desc Description
	//nolint:gosec // G304: the description of a sandbox, in a directory of the root.
	out, err := os.ReadFile(filepath.Join(dir, metadataFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Description{}, ErrNoMetadata
	}
	// The entry carries its directory, so the error names the cause and not the path again.
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return Description{}, fmt.Errorf("unreadable %s: %w", metadataFile, pe.Err)
	}
	if err := json.Unmarshal(out, &desc); err != nil {
		return Description{}, fmt.Errorf("unreadable %s: %w", metadataFile, err)
	}
	if desc.ID != filepath.Base(dir) {
		return Description{}, fmt.Errorf("%s names sandbox %q, not the one of its directory", metadataFile, desc.ID)
	}
	return desc, nil
}

// lockRoot creates root when it does not exist, takes its lock, and returns what releases it.
func lockRoot(ctx context.Context, root string) (func(), error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create the directory of the sandboxes: %w", err)
	}
	lock, err := filelock.Take(ctx, filepath.Join(root, lockFile), nil)
	if err != nil {
		return nil, err //nolint:wrapcheck // Take names the lock and why it gave up.
	}
	return lock.Release, nil
}

// writeFile writes data at path through a temporary and a rename, so that a description is whole or
// absent whatever interrupts it.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write the description: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write the description: %w", err)
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
