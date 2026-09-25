package cli

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	"gitlab.com/hich-hich/cove/internal/agent"
	"gitlab.com/hich-hich/cove/internal/codebase"
	"gitlab.com/hich-hich/cove/internal/erofs"
	"gitlab.com/hich-hich/cove/internal/image"
	"gitlab.com/hich-hich/cove/internal/vmcreate"
	"gitlab.com/hich-hich/cove/internal/vminit/spec"
	"gitlab.com/hich-hich/cove/internal/vmlaunch"
	"gitlab.com/hich-hich/cove/internal/writedisk"
)

// SandboxSpec describes the sandbox run is asked for: the options the user may set on top of the
// fixed process. It is the parsed command line, which the backend turns into a VM.
type SandboxSpec struct {
	// Image is the image of the VM; empty means image.DefaultImage.
	Image string
	// Name is the VM name; empty lets the backend generate one.
	Name string
	// CPUs is the number of vCPUs of the VM.
	CPUs int
	// MemoryMiB is the memory of the VM in MiB.
	MemoryMiB uint32
	// Disk is the largest the disk the VM writes on may be; the host lowers it when it lacks room.
	Disk writedisk.Size
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

// The resources of a VM when run names none. The memory is the floor of a sandbox on a Mac plus
// room for an agent and a container engine; two vCPUs keep a build of the agent from being
// serial.
const (
	defaultCPUs      = 2
	defaultMemoryMiB = 2048
)

// maxCPUs is the most vCPUs libkrun is asked for, a byte in its API.
const maxCPUs = 255

// namePattern is what docker accepts as the name of a container, which the VM also takes as its
// hostname.
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`)

// runCommand creates the sandbox the arguments of run describe and returns the process exit code.
func runCommand(a *App, args []string) int {
	opts, err := parseRun(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, runUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove run: %v\n", err)
		printUsageError(a.Stderr, "run", runUsage)
		return ExitUsage
	}
	ctx, stop := notify(context.Background())
	defer stop()
	vm, err := run(ctx, a, opts)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove run: %v\n", err)
		return ExitPreflight
	}
	_, _ = fmt.Fprintln(a.Stdout, vm)
	return 0
}

// run creates the sandbox of opts and returns the name of its VM once the VM has mounted its
// image. What it did is said on stderr.
func run(ctx context.Context, a *App, opts RunOptions) (string, error) {
	libexec, err := vmlaunch.Dir()
	if err != nil {
		return "", err //nolint:wrapcheck // Dir names what it could not find.
	}
	store, rootfs, err := openStore()
	if err != nil {
		return "", err
	}
	digest, err := resolveImage(ctx, a, store, rootfs, opts.Spec.Image)
	if err != nil {
		return "", err
	}
	_, config, err := store.Image(digest)
	if err != nil {
		return "", err //nolint:wrapcheck // Image names the manifest.
	}
	plan, err := planOf(rootfs, opts.Spec.Image, digest, config)
	if err != nil {
		return "", err
	}
	root, err := vmcreate.DefaultRoot()
	if err != nil {
		return "", err //nolint:wrapcheck // DefaultRoot names what it could not find.
	}
	id := hex.EncodeToString(randomBytes(32))
	vm := cmp.Or(opts.Spec.Name, "cove-"+id[:12])
	sb, err := vmcreate.Create(ctx, libexec, root, vmcreate.Request{
		ID: id, Name: vm,
		CPUs: uint8(opts.Spec.CPUs), MemoryMiB: opts.Spec.MemoryMiB, //nolint:gosec // G115: parseRun bounds CPUs.
		Disk: opts.Spec.Disk, Plan: plan,
		Run: spec.Run{
			User: config.Config.User, Env: append(slices.Clone(config.Config.Env), environ(opts.Spec.Env)...),
			WorkingDir: config.Config.WorkingDir, Volumes: slices.Sorted(maps.Keys(config.Config.Volumes)),
			StopSignal: config.Config.StopSignal, Project: projectOf(opts.URL), Agent: agent.Program,
		},
	})
	if err != nil {
		return "", err //nolint:wrapcheck // Create names what failed and carries the console.
	}
	_, _ = fmt.Fprintf(a.Stderr, "%s: %s in cove-vmm %d, %d vCPUs, %d MiB, a write disk of %s; console in %s\n",
		vm, sb.VM.Hello.VMM, sb.VM.Pid(), opts.Spec.CPUs, opts.Spec.MemoryMiB, sb.WriteDisk, sb.Console)
	return vm, nil
}

// resolveImage returns the digest of the manifest of ref in the store, pulling ref first when the
// store lacks it and it names its registry.
func resolveImage(ctx context.Context, a *App, store *image.Store, rootfs *erofs.Cache, ref string) (v1.Hash, error) {
	key := ref
	var parsed name.Reference
	if image.NamesRegistry(ref) {
		var err error
		if parsed, err = image.Parse(ref); err != nil {
			return v1.Hash{}, err //nolint:wrapcheck // Parse names the reference.
		}
		key = parsed.Name()
	}
	digest, ok, err := store.Find(key)
	if err != nil || ok {
		return digest, err //nolint:wrapcheck // Find names the index.
	}
	if parsed == nil {
		return v1.Hash{}, fmt.Errorf("%s is not in the local store, and names no registry to pull it from", ref)
	}
	res, err := newPuller(a, store, rootfs, a.Stderr, "cove run: ").Pull(ctx, parsed)
	if err != nil {
		return v1.Hash{}, err //nolint:wrapcheck // Pull names the image and the registry.
	}
	return res.Digest, nil
}

// planOf returns the plan of the disks of the image digest, whose config is config: each layer
// must have been converted by the pull that brought it.
func planOf(rootfs *erofs.Cache, ref string, digest v1.Hash, config *v1.ConfigFile) (erofs.Plan, error) {
	blobs := make([]erofs.Blob, 0, len(config.RootFS.DiffIDs))
	for _, id := range config.RootFS.DiffIDs {
		b, ok, err := rootfs.Lookup(id.String())
		if err != nil {
			return erofs.Plan{}, err //nolint:wrapcheck // Lookup names the layer.
		}
		if !ok {
			return erofs.Plan{}, fmt.Errorf("layer %s of %s has no disk in the cache: pull the image again", id, ref)
		}
		blobs = append(blobs, b)
	}
	//nolint:wrapcheck // Plan names the image and the ceiling.
	return rootfs.Plan(erofs.Image{Ref: ref, Digest: digest.String()}, blobs)
}

// environ returns the entries of -e as the VM takes them: KEY=VALUE as written, and a bare KEY with
// the value it has on the host, left out when the host does not set it, as docker does.
func environ(entries []string) []string {
	var out []string
	for _, e := range entries {
		if strings.Contains(e, "=") {
			out = append(out, e)
		} else if v, ok := os.LookupEnv(e); ok {
			out = append(out, e+"="+v)
		}
	}
	return out
}

// projectOf returns the name of the repository at url, the last element of its path without
// .git: the directory the project lands in.
func projectOf(url string) string {
	url = strings.TrimSuffix(strings.TrimRight(url, "/"), ".git")
	return url[strings.LastIndexAny(url, "/:")+1:]
}

// randomBytes returns n bytes of crypto/rand, which never fails.
func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// parseMemory reads a memory size as docker run -m does: a number of bytes, or of k, m or g of
// them in powers of two, in either case. The VM takes whole MiB, so a size that is not one is
// refused rather than rounded.
func parseMemory(v string) (uint32, error) {
	units := map[byte]uint64{'b': 1, 'k': 1 << 10, 'm': 1 << 20, 'g': 1 << 30}
	num, unit := strings.ToLower(v), uint64(1)
	if n := len(num); n > 0 {
		if u, ok := units[num[n-1]]; ok {
			num, unit = num[:n-1], u
		}
	}
	n, err := strconv.ParseUint(num, 10, 64)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("--memory takes a size such as 512m or 4g, not %q", v)
	}
	bytes := n * unit
	if bytes%(1<<20) != 0 || bytes/unit != n || bytes>>20 > math.MaxUint32 {
		return 0, fmt.Errorf("--memory takes a whole number of MiB, not %q", v)
	}
	return uint32(bytes >> 20), nil
}

// parseRun turns the arguments of run into its options. It returns flag.ErrHelp when help was
// asked for, and an error carrying the message to show on any other usage error.
func parseRun(args []string) (RunOptions, error) {
	var (
		opts RunOptions
		env  envFlag
	)
	opts.Spec.Disk = writedisk.DefaultCap
	opts.Spec.CPUs, opts.Spec.MemoryMiB = defaultCPUs, defaultMemoryMiB
	fs := flag.NewFlagSet("cove run", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&opts.Spec.Branch, "b", "", "")
	fs.StringVar(&opts.Spec.Branch, "branch", "", "")
	fs.StringVar(&opts.Spec.Name, "name", "", "")
	fs.StringVar(&opts.Spec.Image, "image", image.DefaultImage, "")
	// A sandbox is always removed when it stops; --rm is taken because docker users type it.
	fs.Bool("rm", false, "")
	fs.IntVar(&opts.Spec.CPUs, "cpus", defaultCPUs, "")
	memory := func(v string) error {
		var err error
		opts.Spec.MemoryMiB, err = parseMemory(v)
		return err
	}
	fs.Func("m", "", memory)
	fs.Func("memory", "", memory)
	fs.Func("disk", "", func(v string) error {
		var err error
		opts.Spec.Disk, err = writedisk.ParseSize(v)
		return err //nolint:wrapcheck // flag names the flag, ParseSize the sizes.
	})
	fs.Var(&env, "e", "")
	fs.Var(&env, "env", "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return opts, err
	}
	if opts.Spec.CPUs < 1 || opts.Spec.CPUs > maxCPUs {
		return opts, fmt.Errorf("--cpus takes 1 to %d vCPUs, not %d", maxCPUs, opts.Spec.CPUs)
	}
	if opts.Spec.Name != "" && !namePattern.MatchString(opts.Spec.Name) {
		return opts, fmt.Errorf("--name takes %s, not %q", namePattern, opts.Spec.Name)
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
      --rm              Remove the VM when it stops, which it always is
      --cpus int        Number of vCPUs (default 2)
  -m, --memory string   Memory of the VM with a suffix, e.g. 512m or 4g
                        (default 2g)
      --disk size       Disk the VM writes on: 8g, 16g, 32g, 64g, 128g, 256g,
                        512g or 1t (default 64g), smaller when the host has
                        less free; a full disk fails the writes of the VM only
  -e, --env list        Set environment variables, KEY=VALUE or KEY to inherit from the host

Not complete yet: run creates the VM and boots it on the image, then prints its
name, but the repository does not reach ` + codebase.Work + ` and no instruction reaches
the agent. The VM runs until its cove-vmm, named on stderr, is killed.

Exit codes: 0 once ` + codebase.Work + ` is ready; 2 on a usage error; 125 when cove could not
create the sandbox, before the VM or after it; the message of git or of the
backend is on stderr. A sandbox that could not be given its codebase is removed,
and a VM that could not be removed is named in the message.
`
