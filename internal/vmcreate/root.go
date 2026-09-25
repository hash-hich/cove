package vmcreate

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultRoot returns the directory of the sandboxes, under the state of cove rather than its
// cache: the cache can be emptied, a sandbox that runs cannot. It is the state directory of
// XDG on Linux, $XDG_STATE_HOME or ~/.local/state, and Application Support on macOS.
func DefaultRoot() (string, error) {
	if runtime.GOOS == "linux" {
		if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
			return filepath.Join(dir, "cove", "sandboxes"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find the directory of the sandboxes: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "cove", "sandboxes"), nil
	}
	return filepath.Join(home, ".local", "state", "cove", "sandboxes"), nil
}
