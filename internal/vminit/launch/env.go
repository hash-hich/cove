// Package launch starts the processes of the image in the guest, the agent first, as a runtime
// starts the process of a docker exec: under the identity the image declares, with its
// environment, in the mount namespace of the agent and its root, on a terminal or on pipes. The
// init is PID 1, so it also collects the end of every process of the VM, those it started and the
// orphans the kernel hands it.
//
// What is applied is what runc applies, since images are tested against it. Nothing is taken
// away: no capability dropped, no seccomp filter, no path masked, no cgroup limit. The VM is the
// boundary, and inside it the agent is root on a machine.
package launch

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultPath is the PATH of a process whose image sets none, the one runc gives.
const DefaultPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

// Environ returns the environment of a process of an image whose configuration sets env, for a
// user whose home is home, on the host hostname, with a terminal when tty holds.
//
// It starts from what docker sets before the image, PATH, HOSTNAME and TERM=xterm on a terminal,
// then applies env over it: a later entry replaces an earlier one of the same name, in its place,
// and an entry without = removes the name. Nothing is expanded: a $ in a value is literal, the
// build already expanded what it meant to. HOME is added last, from /etc/passwd, when neither
// set it, as runc adds it.
func Environ(env []string, home, hostname string, tty bool) []string {
	base := []string{"PATH=" + DefaultPath, "HOSTNAME=" + hostname}
	if tty {
		base = append(base, "TERM=xterm")
	}
	out := replaceOrAppend(base, env)
	for _, kv := range out {
		if strings.HasPrefix(kv, "HOME=") {
			return out
		}
	}
	return append(out, "HOME="+home)
}

// replaceOrAppend applies over to env, as docker merges the environment of an image into its own.
func replaceOrAppend(env, over []string) []string {
	out := append([]string(nil), env...)
	for _, kv := range over {
		name, _, hasValue := strings.Cut(kv, "=")
		at := -1
		for i, e := range out {
			if n, _, _ := strings.Cut(e, "="); n == name {
				at = i
				break
			}
		}
		switch {
		case !hasValue && at >= 0:
			out = append(out[:at], out[at+1:]...)
		case !hasValue:
		case at >= 0:
			out[at] = kv
		default:
			out = append(out, kv)
		}
	}
	return out
}

// Getenv returns the value of name in env, the empty string when env does not set it.
func Getenv(env []string, name string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, name+"="); ok {
			return v
		}
	}
	return ""
}

// LookPath finds the executable name in the directories of path, the value of a PATH, as a shell
// does: the first regular file with an execute bit wins, and an empty entry is the current
// directory. It is called in the root of the image, so a symbolic link resolves inside it.
func LookPath(name, path string) (string, error) {
	if strings.Contains(name, "/") {
		if executable(name) {
			return name, nil
		}
		return "", &NotFoundError{Name: name, Path: path}
	}
	for dir := range strings.SplitSeq(path, ":") {
		if dir == "" {
			dir = "."
		}
		if p := filepath.Join(dir, name); executable(p) {
			return p, nil
		}
	}
	return "", &NotFoundError{Name: name, Path: path}
}

func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0
}

// NotFoundError says a program is not in the PATH of the image.
type NotFoundError struct {
	Name, Path string
}

func (e *NotFoundError) Error() string {
	return e.Name + " not found in PATH (" + e.Path + ")"
}
