package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/go-containerregistry/pkg/logs"
	"github.com/google/go-containerregistry/pkg/name"

	"gitlab.com/hich-hich/cove/internal/erofs"
	"gitlab.com/hich-hich/cove/internal/image"
)

// The exit codes of pull: the 1 of docker pull when what was tried failed, and 128 plus the number
// of the signal, the convention of the shell, so that a script tells an interruption from a
// failure. The 125 of the verbs that need a micro-VM does not apply: pull talks to the registry
// and writes the store, and no sandbox is involved.
const (
	exitFailed     = 1
	exitInterrupt  = 130
	exitTerminated = 143
)

// PullOptions are what pull parses: the image to pull and the shape of the output.
type PullOptions struct {
	// Ref names the image and its registry, by tag or by digest.
	Ref name.Reference
	// Quiet leaves stderr empty, the reference by digest alone on stdout.
	Quiet bool
	// JSON puts one object on stdout instead of the reference, stderr empty; Quiet is then
	// ignored, as on list, where a format that has its own shape wins.
	JSON bool
}

// pullCommand brings an image into the store of cove and returns the process exit code.
func pullCommand(a *App, args []string) int {
	opts, err := parsePull(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, pullUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove pull: %v\n", err)
		printUsageError(a.Stderr, "pull", pullUsage)
		return ExitUsage
	}

	ctx, stop := notify(context.Background())
	defer stop()
	res, code := pull(ctx, a, opts)
	if code != 0 {
		return code
	}
	if opts.JSON {
		image.WriteJSON(a.Stdout, res)
		return 0
	}
	// The reference by digest comes last, on success only: an image=$(cove pull ...) must hold
	// what run can start, or nothing.
	_, _ = fmt.Fprintln(a.Stdout, res.Pinned())
	return 0
}

// pull opens the store and the cache of the rootfs, then pulls the image of opts into them, the
// facts on stderr unless the output is meant for a script. It returns what was pulled, or the
// exit code of the failure it reported on stderr.
func pull(ctx context.Context, a *App, opts PullOptions) (image.Result, int) {
	root, err := image.DefaultRoot()
	if err != nil {
		return image.Result{}, failed(ctx, a, err)
	}
	store, err := image.Open(root)
	if err != nil {
		return image.Result{}, failed(ctx, a, err)
	}
	rootfs, err := erofs.Open(root)
	if err != nil {
		return image.Result{}, failed(ctx, a, err)
	}
	facts := a.Stderr
	if opts.Quiet || opts.JSON {
		facts = io.Discard
	}
	puller := &image.Puller{Store: store, Rootfs: rootfs, Log: facts, Terminal: terminal(a.Stderr)}
	// The library retries a request three times on its own, a second then three of wait, and
	// says nothing by default: a command that takes ten seconds more would look stuck. Its
	// warnings go through the puller, which owns that stream while layers come down.
	logs.Warn.SetOutput(puller.Warnings())
	logs.Warn.SetFlags(0)
	logs.Warn.SetPrefix("cove pull: ")
	res, err := puller.Pull(ctx, opts.Ref)
	if err != nil {
		return image.Result{}, failed(ctx, a, err)
	}
	return res, 0
}

// failed reports err as a failure of pull on stderr, one line, as an interruption when a signal
// cancelled ctx, and returns the exit code to end with. The line is written whatever the output
// mode: a script that reads --json or --quiet still gets the reason on stderr.
func failed(ctx context.Context, a *App, err error) int {
	if sig, ok := errors.AsType[*signalled](context.Cause(ctx)); ok {
		_, _ = fmt.Fprintf(a.Stderr, "cove pull: interrupted: %v\n", err)
		return sig.exitCode()
	}
	_, _ = fmt.Fprintf(a.Stderr, "cove pull: %v\n", err)
	return exitFailed
}

// signalled is why a pull was cancelled: the signal it received.
type signalled struct {
	sig os.Signal
}

func (s *signalled) Error() string {
	return s.sig.String() + " received"
}

// exitCode returns the exit code the signal calls for.
func (s *signalled) exitCode() int {
	if s.sig == syscall.SIGTERM {
		return exitTerminated
	}
	return exitInterrupt
}

// notify returns a context that the first SIGINT or SIGTERM cancels with the signal as cause, and
// the function that stops listening. signal.NotifyContext would keep the signal to itself, and
// the exit code needs it. Once the first signal has started the cleanup, the next one gets its
// default effect again: someone pressing twice wants out now, not to wait for a removal.
func notify(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig, ok := <-ch
		if !ok {
			return
		}
		signal.Stop(ch)
		cancel(&signalled{sig: sig})
	}()
	return ctx, func() {
		signal.Stop(ch)
		close(ch)
		cancel(nil)
	}
}

// parsePull turns the arguments of pull into its options. It returns flag.ErrHelp when help was
// asked for, and an error carrying the message to show on any other usage error, a reference
// without registry or malformed included: nothing was tried, no request was made.
func parsePull(args []string) (PullOptions, error) {
	var opts PullOptions
	fs := flag.NewFlagSet("cove pull", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&opts.Quiet, "q", false, "")
	fs.BoolVar(&opts.Quiet, "quiet", false, "")
	fs.BoolVar(&opts.JSON, "json", false, "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return opts, err
	}
	switch fs.NArg() {
	case 0:
		return opts, errors.New("requires 1 argument, the reference of the image")
	case 1:
	default:
		return opts, fmt.Errorf("takes one reference and one only (got %q too)", fs.Arg(1))
	}
	ref, err := image.Parse(fs.Arg(0))
	if err != nil {
		//nolint:wrapcheck // Parse names the reference and what is expected instead; a prefix would repeat it.
		return opts, err
	}
	opts.Ref = ref
	return opts, nil
}

// pullUsage is the help of pull, in the shape of docker pull so that what
// developers already know applies.
const pullUsage = `Usage: cove pull [OPTIONS] REFERENCE

Pull an image into the store of cove, an OCI layout at ~/.cache/cove/images
($XDG_CACHE_HOME/cove/images when set). The reference names its registry, by
tag or by digest: a bare name (demo, org/repo:tag) is local by definition and
is never looked for on the internet. The manifest pulled is the one of
linux/<architecture of the host>, and an image without one is refused. The
registry is asked what the reference designates on every call, as docker pull
does; only the layers absent from the store are downloaded, each verified
against the digest that names it. The credentials are those docker login or
podman login configured, nothing is asked.

Each layer is turned into the read only disk a sandbox mounts as its blob
lands, and filed under its diff id in ~/.cache/cove/rootfs, where every image
built on that layer finds it. A layer whose decompressed bytes do not match
the diff id the image names fails the pull, and nothing of it is filed. Prints
the reference by digest of the manifest pulled, never of an index: what to
give run.

Options:
  -q, --quiet   Print only the reference by digest, nothing on stderr
      --json    Print one JSON object instead, {ref, digest, platform,
                layers_total, layers_fetched, bytes, cached, blobs_converted,
                source_date_epoch, entries, normalized_entries,
                unknown_xattr_prefixes}, nothing on stderr

Exit codes: 0; 1 when the pull failed, the reason on stderr; 2 on a usage
error, a reference without registry or malformed included; 130 on SIGINT and
143 on SIGTERM, the store left without a partial blob.
`
