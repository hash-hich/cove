package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"gitlab.com/hich-hich/cove/internal/codebase"
	"gitlab.com/hich-hich/cove/internal/image"
)

// SandboxSpec describes the sandbox run is asked for: the options the user may set on top of the
// fixed process. It is the parsed command line, which the backend turns into a VM.
type SandboxSpec struct {
	// Image is the image of the VM; empty means image.DefaultImage.
	Image string
	// Name is the VM name; empty lets the backend generate one.
	Name string
	// Keep leaves the stopped VM in place instead of removing it.
	Keep bool
	// CPUs is the number of vCPUs; zero leaves the default of the backend.
	CPUs int
	// Memory is the memory limit with its suffix (512M, 4G); empty leaves the default of the backend.
	Memory string
	// Env holds the KEY=VALUE or bare KEY (inherited from the host) entries to pass to the VM.
	Env []string
	// Branch is the branch the agent starts from, recorded with the run; empty records nothing.
	Branch string
}

// RunOptions are what run parses: the sandbox to create and the repository to put in it.
type RunOptions struct {
	// Spec is the sandbox; its Branch is the one asked for, empty for the default one.
	Spec SandboxSpec
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

// runCommand parses the arguments of run and returns the process exit code. Nothing is created:
// the backend that would create it is being replaced, so the command stops once its arguments are
// validated.
func runCommand(a *App, args []string) int {
	_, err := parseRun(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, runUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove run: %v\n", err)
		printUsageError(a.Stderr, "run", runUsage)
		return ExitUsage
	}
	return notImplemented(a, "run")
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
	fs.StringVar(&opts.Spec.Image, "image", image.DefaultImage, "")
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

// runUsage is the help of run, in the shape of docker run so that what developers
// already know applies.
var runUsage = `Usage: cove run [OPTIONS] URL

Create a sandbox: a micro-VM from an image carrying the agent, started detached
and kept alive until it is stopped, with the repository at URL in ` + codebase.Work + `.
The whole repository is there, every branch and tag with its history, checked
out on the branch asked for or the default one of the repository, and without a
remote: the agent cannot reach the forge. The repository is read with the access
this machine already has, which does not enter the VM. Prints the name of the VM
once ` + codebase.Work + ` is ready. Every instruction to the agent is a separate command.

The image is ` + image.DefaultImage + `, built from images/sandbox, unless --image names
another one, such as a profile built on it (images/go). A name that carries no
registry is never pulled: the image must be in the local store. One that names
its registry is pulled from it when absent. A sandbox whose image does not
carry the agent is removed.

Options:
  -b, --branch string   Branch to start from; the default branch of the repository otherwise
      --name string     Assign a name to the VM; the backend picks one otherwise
      --image string    Image of the VM (default ` + image.DefaultImage + `)
      --rm              Remove the VM when it stops (default)
      --keep            Keep the stopped VM for inspection instead
      --cpus int        Number of CPUs
  -m, --memory string   Memory limit with a suffix, e.g. 512M or 4G
  -e, --env list        Set environment variables, KEY=VALUE or KEY to inherit from the host

Not implemented yet: the micro-VM backend is being replaced. run validates its
arguments as described above, then exits 125 having created nothing. The exit
codes below are the contract it comes back with.

Exit codes: 0 once ` + codebase.Work + ` is ready; 2 on a usage error; 125 when cove could not
create the sandbox, before the VM or after it; the message of git or of the
backend is on stderr. A sandbox that could not be given its codebase is removed,
and a VM that could not be removed is named in the message.
`
