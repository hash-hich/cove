// Package codebase brings a forge repository to the codebase the agent works on: it fetches it on
// the host with the access its owner already has, into a bare repository that nothing of the forge
// configures, hands it over as a bundle, and says which commands plant that bundle in the sandbox.
// Neither end lets a credential, a hook or a remote through.
package codebase

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// binary is the git of the host, looked up on the PATH.
const binary = "git"

// ErrNotInstalled reports that git could not be found.
var ErrNotInstalled = errors.New("git not found")

// ErrNoDefaultBranch reports that the repository has no default branch: HEAD is not a branch, or
// the repository is empty.
var ErrNoDefaultBranch = errors.New("no default branch")

// fsckHint explains a fetch that git itself would have accepted. Validating the objects on entry
// is cove's requirement, not a default of git, and git names neither who asked for the check
// nor the fact that a plain clone would have passed. Old histories really do carry such objects:
// an author line without a space before the email, or an impossible time zone, both refused
// (measured). There is no flag to skip the check on purpose: it guards what enters the host.
const fsckHint = "cove validates every object on entry (fetch.fsckObjects), which git does not do by default, " +
	"so a repository git clone accepts can be refused here; the objects have to be fixed on the forge"

// fsckMarker is what git prints when that validation is what failed.
const fsckMarker = "fsck error"

// Git runs the git of the host. Its global configuration is read as it is: the credential helper,
// ~/.ssh/config and url.<base>.insteadOf of the owner are exactly the access cove reuses (H2), and
// the prompts of git reach the terminal, which is the owner's. None of it goes further than the
// host: a bundle carries objects and refs, nothing else.
type Git struct {
	// Stderr receives the progress and the messages of git.
	Stderr io.Writer
}

// Repo is a receiver: a bare repository of the host holding the branches and tags of a forge
// repository, and nothing that could act on the host. Close removes it.
type Repo struct {
	git *Git
	// dir is the temporary directory: the repository and the empty hooks directory are under it.
	dir string
}

// refspecs are what enters a receiver: the branches and the tags, under their own names. Nothing
// else of the forge does: refs/replace/, refs/notes/, refs/merge-requests/ and refs/pull/
// stay behind, which neither --mirror nor refs/* would leave.
var refspecs = []string{"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"}

// options returns the settings every git command of cove carries on its argv, never in a file of
// the repository: git itself refuses a local path or a file:// URL (H1), every object that
// enters is validated, submodules are left alone (H5), and the hooks are those of the
// directory hooks, kept empty. The last one matters: a core.hooksPath of the owner runs their
// reference-transaction hook on every ref the fetch writes, and an init.templateDir of theirs
// installs live hooks in the receiver (measured); both are silenced this way. An empty hooks
// string leaves that setting out, for a command that runs no repository.
func options(hooks string) []string {
	opts := []string{
		"-c", "protocol.file.allow=never",
		"-c", "fetch.fsckObjects=true",
		"-c", "fetch.recurseSubmodules=no",
	}
	if hooks != "" {
		opts = append(opts, "-c", "core.hooksPath="+hooks)
	}
	return opts
}

// resolveArgs returns the git argument array that asks the forge for its default branch, the
// target of its symbolic HEAD.
func resolveArgs(url string) []string {
	return append(options(""), "ls-remote", "--symref", url, "HEAD")
}

// initArgs returns the git argument array that creates the bare receiver at dir. The directory is
// given by --git-dir rather than as an operand, and the template is the empty hooks directory: a
// GIT_DIR inherited from the caller (cove run from a git alias or hook) would otherwise redirect
// the init, and the global template of the owner would otherwise populate it (both measured).
func initArgs(dir, hooks string) []string {
	return append([]string{"--git-dir=" + dir}, append(options(hooks), "init", "-q", "--bare", "--template="+hooks)...)
}

// fetchArgs returns the git argument array that fetches url into the receiver at dir, the
// branches and the tags only. branch is named on top of the globs: git then refuses the fetch
// before any download when the forge does not have it (measured: 128, zero objects), where the
// globs alone would fetch everything and leave the question open. Progress is asked for
// explicitly because -q, which silences the line per ref that would otherwise flood the terminal,
// silences the progress too.
func fetchArgs(dir, hooks, url, branch string, progress bool) []string {
	args := append([]string{"--git-dir=" + dir}, append(options(hooks), "fetch", "-q")...)
	if progress {
		args = append(args, "--progress")
	}
	args = append(args, "--no-write-fetch-head", url)
	args = append(args, refspecs...)
	return append(args, "+refs/heads/"+branch+":refs/heads/"+branch)
}

// bundleArgs returns the git argument array that writes the receiver at dir to stdout as a bundle
// of its branches and tags. -q keeps the progress meter git shows on a terminal by default: the
// bundle is local work that ends at once, and its counters would follow the ones of the fetch,
// repeating the same object count for nothing that waits.
func bundleArgs(dir, hooks string) []string {
	create := []string{"bundle", "create", "-q", "-", "--branches", "--tags"}
	return append([]string{"--git-dir=" + dir}, append(options(hooks), create...)...)
}

// Resolve returns the default branch of the repository at url. It returns ErrNotInstalled,
// ErrNoDefaultBranch, or the failure of git, whose message is on Stderr: an unknown host, an
// unreachable repository, an access refused.
func (g *Git) Resolve(ctx context.Context, url string) (string, error) {
	out, err := g.output(ctx, resolveArgs(url))
	if err != nil {
		return "", fmt.Errorf("reach the repository: %w", err)
	}
	return parseResolve(out)
}

// parseResolve reads the default branch out of the lines of ls-remote --symref, each a target and
// a ref separated by a tab: "ref: refs/heads/main\tHEAD" names it.
func parseResolve(out string) (string, error) {
	for line := range strings.Lines(out) {
		target, ref, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		if !ok {
			continue
		}
		if name, ok := strings.CutPrefix(target, "ref: refs/heads/"); ok && ref == "HEAD" {
			return name, nil
		}
	}
	return "", ErrNoDefaultBranch
}

// Fetch creates a receiver and fetches url into it: every branch and tag, the whole history, never
// shallow, and fails before any download when the forge does not have branch. The receiver is
// a temporary directory of the host, gone at Close, and gone already when Fetch fails. It returns
// ErrNotInstalled or the failure of git, whose message is on Stderr: an unknown host, an
// unreachable repository, an access refused, an unknown branch.
func (g *Git) Fetch(ctx context.Context, url, branch string) (*Repo, error) {
	dir, err := os.MkdirTemp("", "cove-receiver-")
	if err != nil {
		return nil, fmt.Errorf("create the receiver: %w", err)
	}
	repo := &Repo{git: g, dir: dir}
	if err := repo.fetch(ctx, url, branch); err != nil {
		_ = repo.Close()
		return nil, err
	}
	return repo, nil
}

// fetch fills the receiver: init, then fetch.
func (r *Repo) fetch(ctx context.Context, url, branch string) error {
	if err := os.Mkdir(r.hooks(), 0o700); err != nil {
		return fmt.Errorf("create the receiver: %w", err)
	}
	if _, err := r.git.output(ctx, initArgs(r.gitDir(), r.hooks())); err != nil {
		return fmt.Errorf("create the receiver: %w", err)
	}
	// The messages of the fetch are watched while they stream, to tell the reader what git leaves
	// unsaid when the validation cove asks for is what refused the repository.
	var said transcript
	args := fetchArgs(r.gitDir(), r.hooks(), url, branch, r.git.terminal())
	if err := r.git.stream(ctx, args, &said); err != nil {
		if said.contains(fsckMarker) {
			return fmt.Errorf("fetch the repository: %w (%s)", err, fsckHint)
		}
		return fmt.Errorf("fetch the repository: %w", err)
	}
	return nil
}

// Bundle writes the receiver to w as a bundle of its branches and tags, the form git reads back
// from a file with the same checks as a fetch. It returns ErrNotInstalled, the failure of git, or
// the one of w.
func (r *Repo) Bundle(ctx context.Context, w io.Writer) error {
	bin, err := lookPath()
	if err != nil {
		return err
	}
	//nolint:gosec // G204: bin comes from LookPath and the arguments are built here, never from a shell string.
	cmd := exec.CommandContext(ctx, bin, bundleArgs(r.gitDir(), r.hooks())...)
	cmd.Env = env()
	cmd.Stdout, cmd.Stderr = w, r.git.stderr()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bundle the repository: %w", err)
	}
	return nil
}

// Close removes the receiver from the host. It does not fail when it is already gone.
func (r *Repo) Close() error {
	if err := os.RemoveAll(r.dir); err != nil {
		return fmt.Errorf("remove the receiver: %w", err)
	}
	return nil
}

// gitDir is the bare repository of the receiver.
func (r *Repo) gitDir() string { return filepath.Join(r.dir, "repo.git") }

// hooks is the directory that stands for the hooks of the receiver, kept empty.
func (r *Repo) hooks() string { return filepath.Join(r.dir, "hooks") }

// stream runs git with args, its stderr going to Stderr and to said as it comes. It returns
// ErrNotInstalled, or the failure of git.
func (g *Git) stream(ctx context.Context, args []string, said *transcript) error {
	bin, err := lookPath()
	if err != nil {
		return err
	}
	//nolint:gosec // G204: bin comes from LookPath and the arguments are built here, never from a shell string.
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env()
	cmd.Stderr = io.MultiWriter(g.stderr(), said)
	if err := cmd.Run(); err != nil {
		//nolint:wrapcheck // The caller names the step; git said the rest on Stderr.
		return err
	}
	return nil
}

// transcript keeps the beginning of what a command said, to look for a marker in it afterwards. It
// is bounded because the output is the repository's and may be long; a marker of git
// comes with the first errors.
type transcript struct {
	said []byte
}

// limit is how much of the output a transcript keeps.
const limit = 8 << 10

// Write keeps the end of what is written, never failing: git names the check that refused a
// repository after the progress of the download, so the beginning is what can be dropped, and
// dropping it must not stop the command that is writing.
func (t *transcript) Write(p []byte) (int, error) {
	t.said = append(t.said, p...)
	if len(t.said) > limit {
		t.said = append(t.said[:0], t.said[len(t.said)-limit:]...)
	}
	return len(p), nil
}

// contains reports whether marker is in what was kept.
func (t *transcript) contains(marker string) bool {
	return bytes.Contains(t.said, []byte(marker))
}

// output runs git with args, its stderr on Stderr, and returns its stdout. It returns
// ErrNotInstalled, or the failure of git.
func (g *Git) output(ctx context.Context, args []string) (string, error) {
	bin, err := lookPath()
	if err != nil {
		return "", err
	}
	//nolint:gosec // G204: bin comes from LookPath and the arguments are built here, never from a shell string.
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env()
	cmd.Stderr = g.stderr()
	out, err := cmd.Output()
	if err != nil {
		//nolint:wrapcheck // The callers name the step; git said the rest on Stderr.
		return "", err
	}
	return string(out), nil
}

// stderr is where the messages of git go: Stderr when the caller gave one, nowhere otherwise.
func (g *Git) stderr() io.Writer {
	if g.Stderr == nil {
		return io.Discard
	}
	return g.Stderr
}

// terminal reports whether Stderr is a terminal, where progress is worth showing.
func (g *Git) terminal() bool {
	f, ok := g.Stderr.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// redirected are the variables that move where git reads and writes. An inherited one sends the
// objects of the forge outside the receiver, where Close would leave them on the host, or makes git
// refuse to create it at all (both measured); --git-dir covers GIT_DIR alone, and a caller that is
// itself a git command exports more than that, which is the case it guards against. The variables
// that carry configuration are left alone: they are part of the access of the owner (H2), and what
// cove needs is on its argv, where it wins.
var redirected = []string{
	"GIT_DIR",
	"GIT_COMMON_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_NAMESPACE",
	"GIT_QUARANTINE_PATH",
	"GIT_GRAFT_FILE",
	"GIT_REPLACE_REF_BASE",
}

// env is the environment of every git command of cove: the one of the process without what would
// move the receiver elsewhere, and with the replace refs ignored whatever a repository or
// a configuration says.
func env() []string {
	from := os.Environ()
	kept := make([]string, 0, len(from)+1)
	for _, entry := range from {
		if name, _, _ := strings.Cut(entry, "="); !slices.Contains(redirected, name) {
			kept = append(kept, entry)
		}
	}
	return append(kept, "GIT_NO_REPLACE_OBJECTS=1")
}

// lookPath finds git on the PATH, or returns ErrNotInstalled.
func lookPath() (string, error) {
	bin, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNotInstalled, err)
	}
	return bin, nil
}
