package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"gitlab.com/hich-hich/cove/internal/receiver"
	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// ExitPreflight is the exit code when cove itself could not run container, as the 125 of docker
// run; container never returns it (every failure of its CLI is 1).
const ExitPreflight = 125

// The identity the agent commits under, written to the repository of the sandbox. The domain is
// reserved by RFC 2606: never resolved, never a mailbox. Nothing of the owner enters the VM,
// and what matters is what cove pushes and the owner reviews.
const (
	agentAuthor = "agent"
	agentEmail  = "agent@cove.invalid"
)

// RunOptions are what run parses: the sandbox to create and the repository to put in it.
type RunOptions struct {
	// Spec is the sandbox; its Branch is the one asked for, empty for the default one.
	Spec sandbox.Spec
	// URL is the repository, on its forge.
	URL string
}

// envFlag accumulates the values of a repeatable -e flag.
type envFlag []string

func (e *envFlag) String() string { return strings.Join(*e, ",") }

func (e *envFlag) Set(value string) error {
	*e = append(*e, value)
	return nil
}

// runCommand creates a sandbox and returns the process exit code.
func runCommand(a *App, args []string) int {
	opts, err := parseRun(args)
	if errors.Is(err, flag.ErrHelp) {
		printRunUsage(a.Stdout)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove run: %v\n", err)
		printHelpHint(a.Stderr, "run")
		return ExitUsage
	}

	// A signal during the creation aborts it and removes what it made, the receiver and the VM if
	// it exists: a sandbox without its repository is not one to keep. Once run has returned, a
	// signal to cove no longer concerns the VM; stopping it is an explicit verb.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Once the first signal has started the teardown, the next one gets its default effect again:
	// someone pressing twice wants out now, not to wait for a delete that hangs.
	context.AfterFunc(ctx, stop)
	name, code := create(ctx, a, opts)
	if code != 0 {
		return code
	}
	// The name comes last, on success only: a name=$(cove run ...) must never hold a VM that was
	// deleted on the way.
	_, _ = fmt.Fprintln(a.Stdout, name)
	return 0
}

// create makes the sandbox of opts and returns the name of its VM, or the exit code of the failure
// it reported on stderr. The repository is reached and fetched before the VM exists, so that a
// failure there leaves nothing (H4), and a VM whose seeding failed is deleted before returning:
// without its repository it is not a sandbox to inspect, --keep or not.
func create(ctx context.Context, a *App, opts RunOptions) (string, int) {
	// Both streams go to stderr: the stdout of run is the name of the VM and nothing else, and a
	// pull or a delete on the way would print there too.
	engine := &sandbox.Engine{Stdout: a.Stderr, Stderr: a.Stderr}
	if err := engine.Preflight(ctx, opts.Spec.Image); err != nil {
		return "", fail(ctx, a, err)
	}
	branch, repo, code := fetch(ctx, a, opts)
	if code != 0 {
		return "", code
	}
	defer func() { _ = repo.Close() }()
	opts.Spec.Branch = branch
	return launch(ctx, a, engine, opts, repo)
}

// fetch brings the repository of opts into a receiver and returns it with the branch to start
// from, or the exit code of the failure it reported. The forge is asked for its default branch
// only when none was given: a branch given is named in the fetch, which git refuses before any
// download when the forge does not have it.
func fetch(ctx context.Context, a *App, opts RunOptions) (string, *receiver.Repo, int) {
	git := &receiver.Git{Stderr: a.Stderr}
	branch := opts.Spec.Branch
	if branch == "" {
		var err error
		if branch, err = git.Resolve(ctx, opts.URL); err != nil {
			return "", nil, fail(ctx, a, err)
		}
		if err := sandbox.CheckBranch(branch); err != nil {
			return "", nil, fail(ctx, a, err)
		}
	}
	repo, err := git.Fetch(ctx, opts.URL, branch)
	if err != nil {
		return "", nil, fail(ctx, a, err)
	}
	return branch, repo, 0
}

// launch creates the VM through engine and seeds it, and returns its name, or the exit code of the
// failure it reported.
func launch(ctx context.Context, a *App, engine *sandbox.Engine, opts RunOptions, repo *receiver.Repo) (string, int) {
	// The creation itself is never interrupted. A signal to the container CLI leaves the VM it was
	// starting behind, and the name that CLI prints when it is done is the only handle on
	// that VM: without it a signal here would leave a micro-VM running with nobody able to name it.
	// The signal is honoured as soon as the name is known, and a second one gets its default effect
	// for whoever will not wait.
	name, code, err := engine.Run(context.WithoutCancel(ctx), opts.Spec)
	if err == nil && code != 0 {
		err = fmt.Errorf("container run exited %d", code)
	}
	if err != nil {
		return "", fail(ctx, a, err)
	}
	if ctx.Err() != nil {
		return "", fail(ctx, a, abort(ctx, engine, name, errors.New("the sandbox was removed")))
	}
	spec := sandbox.SeedSpec{Target: name, Branch: opts.Spec.Branch, Author: agentAuthor, Email: agentEmail}
	if err := seed(ctx, engine, repo, spec); err != nil {
		return "", fail(ctx, a, abort(ctx, engine, name, err))
	}
	return name, 0
}

// abort takes the sandbox away, because one without its codebase is not one to inspect, --keep or
// not, and returns why it was aborted. The delete outlives a cancelled ctx, since it is the very
// thing a signal must not stop, and a VM that could not be removed is named in the error: run
// promises to leave none.
func abort(ctx context.Context, engine *sandbox.Engine, name string, why error) error {
	if err := engine.Delete(context.WithoutCancel(ctx), name); err != nil {
		return errors.Join(why, fmt.Errorf("the sandbox %s is still running: %w", name, err))
	}
	return why
}

// fail reports err as a failure of run on stderr, as an interruption when ctx was cancelled, and
// returns the exit code to end with: cove could not create the sandbox, whatever the reason.
func fail(ctx context.Context, a *App, err error) int {
	if ctx.Err() != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove run: interrupted: %v\n", err)
	} else {
		_, _ = fmt.Fprintf(a.Stderr, "cove run: %v\n", err)
	}
	return ExitPreflight
}

// seed streams the bundle of repo into the sandbox: the receiver writes it on one end of a pipe
// while the steps of spec read the other, so that it never lands on the disk of the host.
func seed(ctx context.Context, engine *sandbox.Engine, repo *receiver.Repo, spec sandbox.SeedSpec) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	pr, pw := io.Pipe()
	bundled := make(chan error, 1)
	go func() {
		err := repo.Bundle(ctx, pw)
		// A nil error is a plain close: the end of the bundle for the reader.
		_ = pw.CloseWithError(err)
		bundled <- err
	}()
	if err := engine.Seed(ctx, spec, pr); err != nil {
		// The bundle may still be compressing for a reader that is gone: its git is stopped rather
		// than waited for, and its error is a consequence, not the cause.
		cancel()
		_ = pr.Close()
		<-bundled
		return fmt.Errorf("seed the sandbox: %w", err)
	}
	// The seeding read the bundle to its end, so its writer has returned: its error, if any, is
	// the one git explained on stderr.
	return <-bundled
}

// parseRun turns the arguments of run into its options. It returns flag.ErrHelp when help was
// asked for, and an error carrying the message to show on any other usage error.
func parseRun(args []string) (RunOptions, error) {
	var (
		opts     RunOptions
		rm, keep bool
		env      envFlag
	)
	fs := flag.NewFlagSet("cove run", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&opts.Spec.Branch, "b", "", "")
	fs.StringVar(&opts.Spec.Branch, "branch", "", "")
	fs.StringVar(&opts.Spec.Name, "name", "", "")
	fs.StringVar(&opts.Spec.Image, "image", sandbox.Image, "")
	fs.BoolVar(&rm, "rm", false, "")
	fs.BoolVar(&keep, "keep", false, "")
	fs.IntVar(&opts.Spec.CPUs, "cpus", 0, "")
	fs.StringVar(&opts.Spec.Memory, "m", "", "")
	fs.StringVar(&opts.Spec.Memory, "memory", "", "")
	fs.Var(&env, "e", "")
	fs.Var(&env, "env", "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return opts, err
	}
	if rm && keep {
		return opts, errors.New("--rm and --keep are mutually exclusive")
	}
	if opts.Spec.CPUs < 0 {
		return opts, errors.New("--cpus must be positive")
	}
	if opts.Spec.Image == "" {
		return opts, errors.New("--image must name an image")
	}
	if opts.Spec.Branch != "" {
		//nolint:wrapcheck // CheckBranch names the branch and what container refuses; a prefix would repeat it.
		if err := sandbox.CheckBranch(opts.Spec.Branch); err != nil {
			return opts, err
		}
	}
	switch fs.NArg() {
	case 0:
		return opts, errors.New("requires 1 argument, the URL of the repository")
	case 1:
	default:
		return opts, fmt.Errorf("takes no command, the sandbox only waits for instructions (got %q)", fs.Arg(1))
	}
	opts.URL = fs.Arg(0)
	opts.Spec.Keep = keep
	opts.Spec.Env = env
	return opts, nil
}

// printRunUsage writes the usage of run to w, in the shape of docker run so that what developers
// already know applies.
func printRunUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, `Usage: cove run [OPTIONS] URL

Create a sandbox: a micro-VM from an image carrying the agent, started detached
and kept alive until it is stopped, with the repository at URL in `+sandbox.Work+`.
The whole repository is there, every branch and tag with its history, checked
out on the branch asked for or the default one of the repository, and without a
remote: the agent cannot reach the forge. The repository is read with the access
this machine already has, which does not enter the VM. Prints the name of the VM
once `+sandbox.Work+` is ready. Every instruction to the agent is a separate command.

The image is `+sandbox.Image+`, built from images/sandbox, unless --image names
another one, such as a profile built on it (images/go). A name that carries no
registry is never pulled: the image must be in the local store. One that names
its registry is pulled from it when absent.

Options:
  -b, --branch string   Branch to start from; the default branch of the repository otherwise
      --name string     Assign a name to the VM; container picks one otherwise
      --image string    Image of the VM (default `+sandbox.Image+`)
      --rm              Remove the VM when it stops (default)
      --keep            Keep the stopped VM for inspection instead
      --cpus int        Number of CPUs
  -m, --memory string   Memory limit with a suffix, e.g. 512M or 4G
  -e, --env list        Set environment variables, KEY=VALUE or KEY to inherit from the host

Exit codes: 0 once `+sandbox.Work+` is ready; 2 on a usage error; 125 when cove could not
create the sandbox, before the VM or after it; the message of git or container
is on stderr. A sandbox that could not be given its codebase is removed, and a
VM that could not be removed is named in the message.
`)
}
