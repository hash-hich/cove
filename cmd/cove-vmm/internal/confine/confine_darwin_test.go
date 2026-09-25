//go:build cgo

package confine_test

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/cmd/cove-vmm/internal/confine"
)

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
	if err := confine.Enter([]string{os.Getenv(readVar)}, []string{os.Getenv(writeVar)}); err != nil {
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
