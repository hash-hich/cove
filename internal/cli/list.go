package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"gitlab.com/hich-hich/cove/internal/inventory"
	"gitlab.com/hich-hich/cove/internal/rwdisk"
	"gitlab.com/hich-hich/cove/internal/sandbox"
)

// The output formats of list. Table is the default, as in docker and podman.
const (
	formatTable = "table"
	formatJSON  = "json"
)

// ListOptions are the options of list: how to report the sandboxes.
type ListOptions struct {
	// Quiet reduces the output to one ID per line, ignored by a format that has its own shape.
	Quiet bool
	// Format is formatTable or formatJSON.
	Format string
}

// listCommand reports every sandbox of the inventory as the arguments of list ask, and returns the
// process exit code. A sandbox whose description cannot be read, or whose state cannot be told, is
// reported all the same, and why on stderr: it is on the host, and whoever cleans up must know it
// is there.
func listCommand(a *App, args []string) int {
	opts, err := parseList(args)
	if errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprint(a.Stdout, listUsage)
		return 0
	}
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove list: %v\n", err)
		printUsageError(a.Stderr, "list", listUsage)
		return ExitUsage
	}
	ctx, stop := notify(context.Background())
	defer stop()
	entries, err := listed(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove list: %v\n", err)
		return ExitPreflight
	}
	sandboxes := measure(entries)
	for _, l := range sandboxes {
		if w := warnings(l); len(w) > 0 {
			_, _ = fmt.Fprintf(a.Stderr, "cove list: %s: %s\n", l.ID[:min(len(l.ID), shortID)], strings.Join(w, "; "))
		}
	}
	if err := printList(a.Stdout, sandboxes, opts, time.Now()); err != nil {
		_, _ = fmt.Fprintf(a.Stderr, "cove list: %v\n", err)
		return ExitPreflight
	}
	return 0
}

// listed returns the sandboxes of the inventory. The stopped ones are listed with those that run,
// where docker ps hides them: a stopped sandbox keeps its write disk, which may take gigabytes, and
// hiding it would hide what fills the host.
func listed(ctx context.Context) ([]inventory.Entry, error) {
	root, err := sandbox.DefaultRoot()
	if err != nil {
		return nil, err //nolint:wrapcheck // DefaultRoot names what it could not find.
	}
	//nolint:wrapcheck // List names the root or the sandbox.
	return inventory.List(ctx, root)
}

// Listed is a sandbox as list reports it: what the inventory knows of it, and the space its write
// disk takes on the host when that was measured.
type Listed struct {
	inventory.Entry
	// DiskUsed is nil when the sandbox has no write disk, or when it could not be measured.
	DiskUsed *int64
	// DiskErr says why a write disk that is there could not be measured.
	DiskErr error
}

// measure returns entries with the space each write disk takes, measured now since it grows as the
// agent writes. A missing disk is no error: a run killed before it wrote it, or a sandbox created
// before cove kept an inventory.
func measure(entries []inventory.Entry) []Listed {
	out := make([]Listed, len(entries))
	for i, e := range entries {
		out[i].Entry = e
		used, err := rwdisk.Usage(sandbox.WriteDisk(e.Dir))
		switch {
		case err == nil:
			out[i].DiskUsed = &used
		case !errors.Is(err, os.ErrNotExist):
			out[i].DiskErr = err
		}
	}
	return out
}

// warnings returns what list says of l on stderr, in few words: the ID and the directory are
// elsewhere, in the line that opens the warnings and in --format json.
func warnings(l Listed) []string {
	var w []string
	if l.State == inventory.Unknown {
		w = append(w, "no lock, state unknown")
	}
	if l.Err != nil {
		w = append(w, l.Err.Error())
	}
	if l.DiskErr != nil {
		cause := l.DiskErr
		if pe, ok := errors.AsType[*os.PathError](cause); ok {
			cause = pe.Err
		}
		w = append(w, "cannot measure its disk: "+cause.Error())
	}
	return w
}

// shortID is the length of an ID in the table and under --quiet, as docker shortens its own.
const shortID = 12

// listedSandbox is a sandbox as list --format json reports it, a contract for the programs that
// read it: a field is added, never renamed or removed. Disk is the size the write disk may reach,
// DiskUsed the space it takes on the host. Dir is where the files of the sandbox are, for whoever
// cleans up; Error says why its metadata.json could not be read, the fields it holds empty.
type listedSandbox struct {
	ID         string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	State      string    `json:"state"`
	Image      string    `json:"image,omitempty"`
	Digest     string    `json:"digest,omitempty"`
	Repository string    `json:"repository,omitempty"`
	Branch     string    `json:"branch,omitempty"`
	Created    time.Time `json:"created,omitzero"`
	CPUs       uint8     `json:"cpus,omitempty"`
	Memory     int64     `json:"memory,omitempty"`
	Disk       int64     `json:"disk,omitempty"`
	DiskUsed   *int64    `json:"diskUsed,omitempty"`
	Dir        string    `json:"dir"`
	Error      string    `json:"error,omitempty"`
}

// printList writes sandboxes to w in the shape opts asks for, the ages counted from now.
func printList(w io.Writer, sandboxes []Listed, opts ListOptions, now time.Time) error {
	if opts.Format == formatJSON {
		out := make([]listedSandbox, 0, len(sandboxes))
		for _, l := range sandboxes {
			j := listedSandbox{
				ID: l.ID, Name: l.Name, State: string(l.State), Image: l.Image, Digest: l.Digest,
				Repository: l.Repository, Branch: l.Branch, Created: l.Created, CPUs: l.CPUs,
				Memory: int64(l.MemoryMiB) << 20, Disk: l.Disk, DiskUsed: l.DiskUsed, Dir: l.Dir,
			}
			if l.Err != nil {
				j.Error = l.Err.Error()
			}
			out = append(out, j)
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out) //nolint:wrapcheck // The only failure is the write to stdout.
	}
	if opts.Quiet {
		for _, l := range sandboxes {
			_, _ = fmt.Fprintln(w, l.ID[:min(len(l.ID), shortID)])
		}
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	// The resources of the VM stay in the JSON, as docker ps leaves them to inspect. The disk is
	// shown where docker ps hides its sizes behind -s: they cost docker a walk of the layer, the
	// disk costs cove one stat, and it is what a person who needs room looks for.
	_, _ = fmt.Fprintln(tw, "SANDBOX ID\tIMAGE\tREPOSITORY\tCREATED\tSTATUS\tDISK\tNAME")
	for _, l := range sandboxes {
		var created string
		if !l.Created.IsZero() {
			created = humanDuration(now.Sub(l.Created)) + " ago"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			l.ID[:min(len(l.ID), shortID)], l.Image, repositoryColumn(l.Repository), created, l.State,
			diskColumn(l), l.Name)
	}
	return tw.Flush() //nolint:wrapcheck // The only failure is the write to stdout.
}

// repositoryColumn says the repository at url as a person finds it: forge, group and name, as
// gitlab.com/group/repo. The scheme, the user and .git say how git reaches it, not which one it
// is, and the scp form of ssh, git@host:path, says the same as host/path. The JSON keeps the URL as
// run was given it, for a program that clones or compares.
func repositoryColumn(url string) string {
	rest, hasScheme := url, false
	if _, after, ok := strings.Cut(url, "://"); ok {
		rest, hasScheme = after, true
	}
	host, path, _ := strings.Cut(rest, "/")
	if _, after, ok := strings.Cut(host, "@"); ok {
		host = after
	}
	// Without a scheme, a colon in the host is the scp form of ssh, where the path follows it.
	if h, p, ok := strings.Cut(host, ":"); ok && !hasScheme {
		host, path = h, strings.TrimPrefix(p+"/"+path, "/")
	}
	path = strings.TrimSuffix(strings.TrimRight(path, "/"), ".git")
	if path == "" {
		return host
	}
	return host + "/" + path
}

// diskColumn says the write disk of l as docker ps -s says the size of a container: the space it
// takes, then the most it may take. What is not known is left out rather than written as zero,
// which would claim an empty disk.
func diskColumn(l Listed) string {
	var most string
	if l.Err == nil {
		most = "max " + strings.ToUpper(rwdisk.Size(l.Disk).String())
	}
	switch {
	case l.DiskUsed == nil:
		return most
	case most == "":
		return byteSize(*l.DiskUsed)
	default:
		return byteSize(*l.DiskUsed) + " (" + most + ")"
	}
}

// byteSize says n bytes as df -h does, in powers of two and in capitals, which run takes too: whole
// K and M, and G and T with one decimal, since a disk of 1.4G and one of 1G differ by what a
// person wants to know. The bytes are those run counts, so that 64G is the --disk 64g asked for,
// where docker ps writes powers of ten and would say 68.7GB.
func byteSize(n int64) string {
	const (
		kib = int64(1) << 10
		mib = kib << 10
		gib = mib << 10
		tib = gib << 10
	)
	decimal := func(unit int64) string {
		return strings.TrimSuffix(strconv.FormatFloat(float64(n)/float64(unit), 'f', 1, 64), ".0")
	}
	switch {
	case n < mib:
		return strconv.FormatInt(n/kib, 10) + "K"
	case n < gib:
		return strconv.FormatInt(n/mib, 10) + "M"
	case n < tib:
		return decimal(gib) + "G"
	default:
		return decimal(tib) + "T"
	}
}

// The units of an age, beyond the hour.
const (
	day   = 24 * time.Hour
	week  = 7 * day
	month = 30 * day
	year  = 365 * day
)

// ages are the words of an age below each bound, in order: the words alone when unit is zero, the
// age counted in unit otherwise. They are those docker ps says the age of a container with.
var ages = []struct {
	below, unit time.Duration
	words       string
}{
	{time.Second, 0, "Less than a second"},
	{2 * time.Second, 0, "1 second"},
	{time.Minute, time.Second, "%d seconds"},
	{2 * time.Minute, 0, "About a minute"},
	{time.Hour, time.Minute, "%d minutes"},
	{2 * time.Hour, 0, "About an hour"},
	{2 * day, time.Hour, "%d hours"},
	{2 * week, day, "%d days"},
	{3 * month, week, "%d weeks"},
	{2 * year, month, "%d months"},
}

// humanDuration says d in words, rounded down to the unit that suits it.
func humanDuration(d time.Duration) string {
	for _, a := range ages {
		if d < a.below {
			if a.unit == 0 {
				return a.words
			}
			return fmt.Sprintf(a.words, d/a.unit)
		}
	}
	return fmt.Sprintf("%d years", d/year)
}

// parseList turns the arguments of list into its options. It returns flag.ErrHelp when help was
// asked for, and an error carrying the message to show on any other usage error.
func parseList(args []string) (ListOptions, error) {
	var opts ListOptions
	fs := flag.NewFlagSet("cove list", flag.ContinueOnError)
	// flag would print the message itself; the caller prints it with the usage, once.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.BoolVar(&opts.Quiet, "q", false, "")
	fs.BoolVar(&opts.Quiet, "quiet", false, "")
	fs.StringVar(&opts.Format, "format", formatTable, "")
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "")

	if err := fs.Parse(args); err != nil {
		//nolint:wrapcheck // flag's messages are complete and name the flag; a prefix would only repeat them.
		return opts, err
	}
	if opts.Format != formatTable && opts.Format != formatJSON {
		return opts, fmt.Errorf("--format must be %s or %s (got %q)", formatTable, formatJSON, opts.Format)
	}
	if asJSON {
		// Only the order of the flags would decide between the two otherwise.
		if opts.Format != formatJSON && formatGiven(fs) {
			return opts, fmt.Errorf("--json and --format %s ask for two formats", opts.Format)
		}
		opts.Format = formatJSON
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("takes no argument, it reports every sandbox of cove (got %q)", fs.Arg(0))
	}
	return opts, nil
}

// formatGiven reports whether --format was on the command line fs parsed.
func formatGiven(fs *flag.FlagSet) bool {
	given := false
	fs.Visit(func(f *flag.Flag) { given = given || f.Name == "format" })
	return given
}

// listUsage is the help of list, in the shape of docker ps so that what developers
// already know applies. The aliases share it: the help of ls and ps is the help of list.
const listUsage = `Usage: cove list [OPTIONS]

List every sandbox of cove, stopped ones included: those whose directory is in
the state of cove, and not the other VMs of the host. docker ps hides a stopped
container by default; cove does not, since a stopped sandbox keeps its write
disk and hiding it would hide what fills the host. The columns follow those of
docker ps.

STATUS is running while the cove-vmm of the sandbox lives, and stopped once it
has ended, whatever ended it. It is unknown when the directory of the sandbox
has no lock, left by a sandbox older than the inventory or changed by hand: its
VM may still run. DISK is the space the write disk takes on the host, then the
most it may take, in powers of two as run takes them: 1G is 1024M. The vCPUs
and the memory of the VM are in the json. A sandbox whose metadata.json cannot be read, or whose state is
unknown, is listed all the same with the reason on stderr, since its files are
still on the host.

Aliases: cove ls, cove ps

Options:
  -q, --quiet           Only print the sandbox IDs, one per line
      --format string   Output format, table or json (default table)
      --json            Same as --format json

The json format is an array, one object per sandbox, latest created first:
id, name, state (running, stopped or unknown), image, digest, repository,
branch, created, cpus, memory in bytes, disk, the size in bytes the write disk
may reach, diskUsed, the bytes it takes on the host, dir, the directory of its
files, and error when its metadata.json could not be read. A field that is not known
is left out. A field is added, never renamed or removed.

Exit codes: 0; 2 on a usage error; 125 when cove could not carry the command out.
`
