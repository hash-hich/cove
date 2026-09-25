//go:build cgo

package confine_test

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/confine"
)

// childVar makes the test binary the confined process itself: the sandbox cannot be left, so the
// test runs it in a child.
const childVar = "COVE_CONFINE_CHILD"

// TestMain runs the confined child when the test re-executes itself, and the tests otherwise.
func TestMain(m *testing.M) {
	if what := os.Getenv(childVar); what != "" {
		os.Exit(child(what))
	}
	os.Exit(m.Run())
}

// child enters the sandbox, then does what, and exits 0 when it succeeded, 1 when it was refused.
func child(what string) int {
	if err := confine.Enter(); err != nil {
		return 2
	}
	if !attempts[what]() {
		return 1
	}
	return 0
}

// attempts are what a confined child tries, by the name the test gives it, each reporting whether
// it succeeded. Descriptor 3 is the file the test handed over.
var attempts = map[string]func() bool{
	"read-given": func() bool {
		f, err := os.Open("/dev/fd/3")
		if err != nil {
			return false
		}
		_, err = io.ReadAll(f)
		return err == nil
	},
	"write-given": func() bool {
		f, err := os.OpenFile("/dev/fd/3", os.O_WRONLY, 0)
		if err != nil {
			return false
		}
		_, err = f.WriteString("x")
		return err == nil
	},
	"read-path": func() bool {
		_, err := os.ReadFile(os.Getenv("COVE_CONFINE_GIVEN"))
		return err == nil
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
		{what: "read-given", allowed: true},
		{what: "write-given", allowed: true},
		{what: "read-path"},
		{what: "read-home"},
		{what: "dial"},
		{what: "exec"},
	}
	for _, tt := range tests {
		t.Run(tt.what, func(t *testing.T) {
			t.Parallel()

			given := t.TempDir() + "/given"
			require.NoError(t, os.WriteFile(given, []byte("given"), 0o600))
			//nolint:gosec // G304: a file of the temporary directory of the test.
			f, err := os.OpenFile(given, os.O_RDWR, 0)
			require.NoError(t, err)
			t.Cleanup(func() { _ = f.Close() })

			//nolint:gosec // G204, G702: the test binary itself, run again as the child.
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
			// The child is told the path of the file it was given, which it must not be able to
			// open again by that path.
			cmd.Env = append(os.Environ(), childVar+"="+tt.what, "COVE_CONFINE_ADDR="+ln.Addr().String(),
				"COVE_CONFINE_GIVEN="+given)
			cmd.ExtraFiles = []*os.File{f}
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
		})
	}
}
