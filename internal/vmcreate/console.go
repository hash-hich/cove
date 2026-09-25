package vmcreate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"gitlab.com/hich-hich/cove/internal/vminit/spec"
	"gitlab.com/hich-hich/cove/internal/vmlaunch"
)

// pollInterval is how often the console is read while the VM boots.
const pollInterval = 50 * time.Millisecond

// tailLines is how much of the console an error carries.
const tailLines = 20

// waitReady returns once the init of vm has said on the console at path that the image is
// mounted, and fails when the VM ends first, when bootTimeout passes, or when ctx ends. The
// console is read as lines of text and nothing else: it is written by the guest.
func waitReady(ctx context.Context, vm *vmlaunch.VM, path string) error {
	ctx, cancel := context.WithTimeout(ctx, bootTimeout)
	defer cancel()
	ready := []byte("\n" + spec.ConsolePrefix + spec.Ready + "\n")
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		// A line at the very start of the file is preceded by nothing, so a newline stands for it.
		//nolint:gosec // G304: the console of the sandbox, in its directory.
		out, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read the console: %w", err)
		}
		if bytes.Contains(append([]byte("\n"), bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))...), ready) {
			return nil
		}
		select {
		case <-vm.Done():
			if vm.Err() == nil {
				return errEnded
			}
			return fmt.Errorf("%w: %w", errEnded, vm.Err())
		case <-ctx.Done():
			if ctx.Err() == context.DeadlineExceeded {
				return fmt.Errorf("the init did not say the image was mounted within %s", bootTimeout)
			}
			return ctx.Err() //nolint:wrapcheck // The cancellation of the caller, as it is.
		case <-tick.C:
		}
	}
}

// withConsole returns err followed by the last lines of the console at path, when it holds any.
func withConsole(err error, path string) error {
	//nolint:gosec // G304: the console of the sandbox, in its directory.
	out, rerr := os.ReadFile(path)
	if rerr != nil || len(bytes.TrimSpace(out)) == 0 {
		return err
	}
	return fmt.Errorf("%w; the console ended with:\n%s", err, tail(out, tailLines))
}

// tail returns the last n lines of out, each one stripped of what a terminal would act on: the
// guest wrote them, and an escape sequence must reach nobody's terminal.
func tail(out []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	lines = lines[max(0, len(lines)-n):]
	for i, l := range lines {
		lines[i] = "  " + strings.Map(printable, strings.TrimRight(l, "\r"))
	}
	return strings.Join(lines, "\n")
}

func printable(r rune) rune {
	if r == '\t' || unicode.IsPrint(r) {
		return r
	}
	return '?'
}
