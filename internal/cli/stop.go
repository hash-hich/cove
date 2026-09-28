package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"sync"
	"time"

	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// StopSpec describes a stop: the parsed command line of the verb, the VMs to stop and the options
// the user may set.
type StopSpec struct {
	// Targets are the sandboxes to stop, by name, ID or prefix of an ID.
	Targets []string
	// Timeout is how long the VM has to end before cove-vmm is killed; zero kills it at once.
	Timeout time.Duration
}

// stopCommand stops the sandboxes the arguments of stop name, and returns the process exit code.
func stopCommand(a *App, args []string) int {
	spec, all, err := parseStop(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, stopUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove stop: %v\n", err)
		printUsageError(a.Stderr, "stop", stopUsage)
		return ExitUsage
	}
	ctx, stop := notify(context.Background())
	defer stop()
	root, err := sandbox.DefaultRoot()
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove stop: %v\n", err)
		return ExitPreflight
	}
	entries, err := inventory.List(ctx, root)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove stop: %v\n", err)
		return ExitPreflight
	}
	targets, ok := resolve(a, "stop", entries, spec.Targets, all)
	if !stopAll(ctx, a, root, targets, spec.Timeout) {
		ok = false
	}
	if sig, interrupted := errors.AsType[*signalled](context.Cause(ctx)); interrupted {
		return sig.exitCode()
	}
	if !ok {
		return exitFailed
	}
	return 0
}

// target is a sandbox a verb acts on, and how the command line named it, which is what the verb
// prints.
type target struct {
	named string
	entry inventory.Entry
}

// resolve returns the sandboxes of entries that names designate, or the running ones with all, and
// whether every name was found. What a name does not find is said on stderr, as an error of verb. A
// sandbox named twice is returned once.
func resolve(a *App, verb string, entries []inventory.Entry, names []string, all bool) ([]target, bool) {
	var targets []target
	if all {
		for _, e := range entries {
			if e.State == inventory.Running {
				targets = append(targets, target{named: nameOf(e), entry: e})
			}
		}
		return targets, true
	}
	ok := true
	seen := make(map[string]bool)
	for _, n := range names {
		e, err := inventory.Find(entries, n)
		if err != nil {
			_, _ = fmt.Fprintf(a.Stderr, "cove %s: %v\n", verb, err)
			ok = false
			continue
		}
		if !seen[e.ID] {
			seen[e.ID] = true
			targets = append(targets, target{named: n, entry: e})
		}
	}
	return targets, ok
}

// nameOf returns the name of the sandbox e, or its ID when its description could not be read.
func nameOf(e inventory.Entry) string {
	if e.Name != "" {
		return e.Name
	}
	return e.ID
}

// stopAll stops targets side by side, each in timeout, prints each one on stdout once stopped and
// what went wrong on stderr, and reports whether all of them stopped.
func stopAll(ctx context.Context, a *App, root string, targets []target, timeout time.Duration) bool {
	var (
		wg sync.WaitGroup
		mu sync.Mutex
		ok = true
	)
	for _, t := range targets {
		wg.Go(func() {
			st, err := sandbox.Stop(ctx, root, t.entry, timeout)
			mu.Lock()
			defer mu.Unlock()
			for _, line := range stopReport(st) {
				_, _ = fmt.Fprintf(a.Stderr, "cove stop: %s: %s\n", t.named, line)
			}
			if err != nil {
				_, _ = fmt.Fprintf(a.Stderr, "cove stop: %s: %v\n", t.named, err)
				ok = false
				return
			}
			_, _ = fmt.Fprintln(a.Stdout, t.named)
		})
	}
	wg.Wait()
	return ok
}

// stopReport returns what stop says of st on stderr: the steps the init could not carry out, and
// why cove-vmm was killed with how far the init had gone, which tells an init at work from one
// that got nothing. A stop the init carried out whole says nothing, as docker stop does.
func stopReport(st sandbox.Stopped) []string {
	var lines []string
	for _, s := range st.Steps {
		if s.Error != "" {
			lines = append(lines, s.Name+" failed: "+s.Error)
		}
	}
	if st.Killed != nil {
		reached := "the init reported no step"
		if n := len(st.Steps); n > 0 {
			reached = "the init had reached: " + st.Steps[n-1].Name
		}
		lines = append(lines, fmt.Sprintf("cove-vmm killed: %v; %s", st.Killed, reached))
	}
	return lines
}

// parseStop turns the arguments of stop into a stop spec and whether --all was given. It returns
// flag.ErrHelp when help was asked for, and an error carrying the message to show on any other
// usage error.
func parseStop(args []string) (StopSpec, bool, error) {
	var (
		spec StopSpec
		all  bool
	)
	timeout := int(sandbox.DefaultStopTimeout / time.Second)
	fs := flag.NewFlagSet("cove stop", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&all, "a", false, "")
	fs.BoolVar(&all, "all", false, "")
	fs.IntVar(&timeout, "t", timeout, "")
	fs.IntVar(&timeout, "time", timeout, "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return spec, false, err
	}
	if timeout < 0 {
		return spec, false, errors.New("--time must be positive")
	}
	spec.Timeout = time.Duration(timeout) * time.Second
	if all && fs.NArg() > 0 {
		return spec, false, fmt.Errorf("--all takes no target (got %q)", fs.Arg(0))
	}
	if all {
		return spec, true, nil
	}
	if fs.NArg() == 0 {
		return spec, false, errors.New("requires at least 1 argument")
	}
	spec.Targets = fs.Args()
	return spec, false, nil
}

// seconds says d as the help says a delay, in whole seconds.
func seconds(d time.Duration) string {
	return strconv.Itoa(int(d/time.Second)) + "s"
}

// stopUsage is the help of stop, in the shape of docker stop so that what
// developers already know applies.
var stopUsage = `Usage: cove stop [OPTIONS] SANDBOX [SANDBOX...]
       cove stop --all

Stop one or more sandboxes, given by name, by ID or by a prefix of an ID that
starts no other, as list shows them. Prints each sandbox once stopped, as it
was named. Only the sandboxes of cove are stopped: a name that is not one is
refused, and no other VM of the host is ever touched.

The init of the VM is asked to stop: it sends the stop signal of the image to
every process, kills those still running once their grace has passed, flushes
the write disk and makes it read only, then powers the VM off. The grace is
the time given with -t, less ` + seconds(sandbox.FlushTime) + ` kept for the disk. A VM still running
at the end of -t, or whose init cannot be reached, has its cove-vmm killed,
and stop says why on stderr, with the last step the init had reached: what the
processes had not written to the disk by then is lost.

A stopped sandbox keeps its write disk, to be started again, unless run was
given --rm: it is then removed with its disk. A sandbox that already stopped
is not an error, and an --rm one is removed.

Options:
  -a, --all             Stop every running sandbox of cove
  -t, --time int        Seconds the VM has to end before it is killed
                        (default ` + seconds(sandbox.DefaultStopTimeout) + `)

Exit codes: 0 when every target stopped; 1 when one was unknown or could not be
stopped; 2 on a usage error; 125 when cove could not carry the command out;
130 on SIGINT and 143 on SIGTERM, the VMs left to end as they were asked.
`
