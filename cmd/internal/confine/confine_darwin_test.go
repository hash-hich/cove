//go:build cgo

package confine_test

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/cmd/internal/confine"
)

// profile denies everything, the base every binary adds its own rules to.
const profile = "(version 1)\n(deny default)\n"

// childVar makes the test binary the confined process itself: the sandbox cannot be left, so the
// test runs it in a child.
const childVar = "COVE_CONFINE_CHILD"

// The variables that name to the child the file it may read, the file it may write, and a file of
// the same directory that it was not given.
const (
	readVar  = "COVE_CONFINE_READ"
	writeVar = "COVE_CONFINE_WRITE"
	otherVar = "COVE_CONFINE_OTHER"
)

// TestMain runs the confined child when the test re-executes itself, and the tests otherwise.
func TestMain(m *testing.M) {
	if what := os.Getenv(childVar); what != "" {
		os.Exit(child(what))
	}
	os.Exit(m.Run())
}

// child enters the sandbox, then does what, and exits 0 when it succeeded, 1 when it was refused.
func child(what string) int {
	switch what {
	case listenOther, listenGiven:
		return listenChild(what)
	case connectOther, connectGiven:
		return connectChild(what)
	}
	paths := confine.Paths{Read: []string{os.Getenv(readVar)}, Write: []string{os.Getenv(writeVar)}}
	if err := confine.Enter(profile, paths); err != nil {
		return 2
	}
	if !attempts[what]() {
		return 1
	}
	return 0
}

// read and write open the file of a variable, and read it or write on it.
func read(name string) bool {
	//nolint:gosec // G703: a file the test made in its temporary directory.
	_, err := os.ReadFile(os.Getenv(name))
	return err == nil
}

func write(name string) bool {
	//nolint:gosec // G703: a file the test made in its temporary directory.
	f, err := os.OpenFile(os.Getenv(name), os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_, err = f.WriteString("x")
	_ = f.Close()
	return err == nil
}

// writeRead is the attempt to write on the file given to read, which must leave it as it was.
const writeRead = "write-read"

// attempts are what a confined child tries, by the name the test gives it, each reporting whether
// it succeeded.
var attempts = map[string]func() bool{
	"read-read":   func() bool { return read(readVar) },
	"read-write":  func() bool { return read(writeVar) },
	"write-write": func() bool { return write(writeVar) },
	writeRead:     func() bool { return write(readVar) },
	// The owner of a file changes its mode, its times and its attributes whatever it opened it for:
	// only the profile refuses these.
	"chmod-read": func() bool {
		return os.Chmod(os.Getenv(readVar), 0o666) == nil //nolint:gosec // G302: what the profile must refuse.
	},
	"chmod-write": func() bool {
		return os.Chmod(os.Getenv(writeVar), 0o666) == nil //nolint:gosec // G302: what the profile must refuse.
	},
	"utimes-read": func() bool {
		return os.Chtimes(os.Getenv(readVar), time.Unix(1, 0), time.Unix(1, 0)) == nil
	},
	"xattr-read": func() bool {
		return unix.Setxattr(os.Getenv(readVar), "user.cove", []byte("x"), 0) == nil
	},
	"remove-read": func() bool {
		return os.Remove(os.Getenv(readVar)) == nil
	},
	"read-other": func() bool { return read(otherVar) },
	// A link beside the given file, which cove never names: the rule matches the path, not the file.
	"read-link": func() bool {
		_, err := os.ReadFile(os.Getenv(readVar) + ".link")
		return err == nil
	},
	"list-dir": func() bool {
		_, err := os.ReadDir(filepath.Dir(os.Getenv(readVar)))
		return err == nil
	},
	"create-in-dir": func() bool {
		return os.WriteFile(filepath.Dir(os.Getenv(readVar))+"/new", nil, 0o600) == nil
	},
	"read-home": func() bool {
		_, err := os.ReadDir(os.Getenv("HOME"))
		return err == nil
	},
	"dial": func() bool {
		var d net.Dialer
		c, err := d.DialContext(context.Background(), "tcp", os.Getenv("COVE_CONFINE_ADDR"))
		if err == nil {
			_ = c.Close()
		}
		return err == nil
	},
	"exec": func() bool {
		return exec.CommandContext(context.Background(), "/usr/bin/true").Run() == nil
	},
}

// The attempts of a child given a socket to listen on: on that one, and on another of its directory.
const (
	listenGiven = "listen-given"
	listenOther = "listen-other"
)

// otherSocket is a socket of the directory of the given one, which the child was not given.
const otherSocket = "other.sock"

// socketVar names to the child the socket it may listen on.
const socketVar = "COVE_CONFINE_SOCKET"

// listenChild enters the sandbox from the directory of the socket it was given, as cove-vmm does,
// then listens on that socket or on another one by a path relative to it, says so on stdout, and
// accepts one connection.
func listenChild(what string) int {
	socket := os.Getenv(socketVar)
	if err := os.Chdir(filepath.Dir(socket)); err != nil {
		return 2
	}
	if err := confine.Enter(profile, confine.Paths{Listen: []string{socket}}); err != nil {
		return 2
	}
	name := filepath.Base(socket)
	if what == listenOther {
		name = otherSocket
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "unix", name)
	if err != nil {
		return 1
	}
	_, _ = os.Stdout.WriteString("listening\n")
	c, err := ln.Accept()
	if err != nil {
		return 1
	}
	_ = c.Close()
	return 0
}

func TestEnterListen(t *testing.T) {
	t.Parallel()

	for what, allowed := range map[string]bool{listenGiven: true, listenOther: false} {
		t.Run(what, func(t *testing.T) {
			t.Parallel()

			// A Unix socket takes a path of 104 bytes at most, which a temporary directory of the test
			// can pass: the child listens by a path relative to it, as cove-vmm does.
			dir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			socket := filepath.Join(dir, "control.sock")

			//nolint:gosec // G204, G702: the test binary itself, run again as the child.
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), childVar+"="+what, socketVar+"="+socket)
			out, err := cmd.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, cmd.Start())
			line, _ := bufio.NewReader(out).ReadString('\n')
			if line == "listening\n" {
				dialIn(t, dir, filepath.Base(socket))
			}
			err = cmd.Wait()

			if allowed {
				require.NoError(t, err)
				return
			}
			exit, ok := err.(*exec.ExitError) //nolint:errorlint // Wait returns *ExitError itself.
			require.True(t, ok, "%v", err)
			require.Equal(t, 1, exit.ExitCode())
		})
	}
}

// The attempts of a child given a socket to connect to: to that one, and to another of its
// directory.
const (
	connectGiven = "connect-given"
	connectOther = "connect-other"
)

// connectChild enters the sandbox from the directory of the socket it was given, as cove-vmm does,
// then connects to that socket or to another one of the directory, by a path relative to it.
func connectChild(what string) int {
	socket := os.Getenv(socketVar)
	if err := os.Chdir(filepath.Dir(socket)); err != nil {
		return 2
	}
	if err := confine.Enter(profile, confine.Paths{Connect: []string{socket}}); err != nil {
		return 2
	}
	name := filepath.Base(socket)
	if what == connectOther {
		name = otherSocket
	}
	var d net.Dialer
	c, err := d.DialContext(context.Background(), "unix", name)
	if err != nil {
		return 1
	}
	_ = c.Close()
	return 0
}

func TestEnterConnect(t *testing.T) {
	t.Parallel()

	for what, allowed := range map[string]bool{connectGiven: true, connectOther: false} {
		t.Run(what, func(t *testing.T) {
			t.Parallel()

			dir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			for _, name := range []string{"card.sock", otherSocket} {
				listenIn(t, dir, name)
			}

			//nolint:gosec // G204, G702: the test binary itself, run again as the child.
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), childVar+"="+what, socketVar+"="+filepath.Join(dir, "card.sock"))
			err = cmd.Run()

			if allowed {
				require.NoError(t, err)
				return
			}
			exit, ok := err.(*exec.ExitError) //nolint:errorlint // Run returns *ExitError itself.
			require.True(t, ok, "%v", err)
			require.Equal(t, 1, exit.ExitCode())
		})
	}
}

// listenIn listens on the socket name of dir, through a link in a directory whose path is short
// enough for a Unix socket, and accepts its connections until the test ends.
func listenIn(t *testing.T, dir, name string) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "unix", filepath.Join(shortLink(t, dir), name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
}

// shortLink returns a link to dir in a directory whose path is short enough for a Unix socket.
func shortLink(t *testing.T, dir string) string {
	t.Helper()
	//nolint:usetesting // The temporary directory of the test is what makes the path too long.
	short, err := os.MkdirTemp("/tmp", "cc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(short) })
	link := filepath.Join(short, "d")
	require.NoError(t, os.Symlink(dir, link))
	return link
}

// dialIn connects to the socket name of dir, through a link in a directory whose path is short
// enough for a Unix socket.
func dialIn(t *testing.T, dir, name string) {
	t.Helper()
	var d net.Dialer
	c, err := d.DialContext(t.Context(), "unix", filepath.Join(shortLink(t, dir), name))
	require.NoError(t, err)
	_ = c.Close()
}

func TestEnter(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	tests := []struct {
		what    string
		allowed bool
	}{
		{what: "read-read", allowed: true},
		{what: "read-write", allowed: true},
		{what: "write-write", allowed: true},
		{what: writeRead},
		{what: "chmod-read"},
		{what: "chmod-write"},
		{what: "utimes-read"},
		{what: "xattr-read"},
		{what: "remove-read"},
		{what: "read-other"},
		{what: "read-link"},
		{what: "list-dir"},
		{what: "create-in-dir"},
		{what: "read-home"},
		{what: "dial"},
		{what: "exec"},
	}
	for _, tt := range tests {
		t.Run(tt.what, func(t *testing.T) {
			t.Parallel()

			// The temporary directory is under /var, a link to /private/var: cove resolves the paths
			// it hands, and so does the test.
			dir, err := filepath.EvalSymlinks(t.TempDir())
			require.NoError(t, err)
			given := map[string]string{readVar: dir + "/read", writeVar: dir + "/write", otherVar: dir + "/other"}
			for _, path := range given {
				require.NoError(t, os.WriteFile(path, []byte("given"), 0o600))
			}
			require.NoError(t, os.Symlink(given[readVar], given[readVar]+".link"))

			//nolint:gosec // G204, G702: the test binary itself, run again as the child.
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
			cmd.Env = append(os.Environ(), childVar+"="+tt.what, "COVE_CONFINE_ADDR="+ln.Addr().String())
			for name, path := range given {
				cmd.Env = append(cmd.Env, name+"="+path)
			}
			err = cmd.Run()

			code := 0
			if exit, ok := err.(*exec.ExitError); ok { //nolint:errorlint // Run returns *ExitError itself.
				code = exit.ExitCode()
			} else {
				require.NoError(t, err)
			}
			want := 1
			if tt.allowed {
				want = 0
			}
			require.Equal(t, want, code, tt.what)
			if tt.what == writeRead {
				got, err := os.ReadFile(given[readVar])
				require.NoError(t, err)
				require.Equal(t, "given", string(got), "a file given for reading stays as it was")
			}
		})
	}
}
