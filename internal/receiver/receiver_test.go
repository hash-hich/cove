package receiver_test

import (
	"bytes"
	"io"
	"io/fs"
	"net/http/cgi" //nolint:gosec // G504: the vulnerable versions of Go are far behind go.mod.
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/receiver"
)

// The placeholders the argv tests use for the receiver, its hooks directory and the forge.
const (
	dir   = "/tmp/r/repo.git"
	hooks = "/tmp/r/hooks"
	url   = "https://forge.example/group/repo.git"
	// The branches of the test forge, and the refs a rich one carries.
	branch  = "main"
	feature = "feature"
	main    = "refs/heads/" + branch
	feat    = "refs/heads/" + feature
	v1      = "refs/tags/v1"
	// options are the settings every command carries once the receiver exists.
	options = "-c protocol.file.allow=never -c fetch.fsckObjects=true -c fetch.recurseSubmodules=no" +
		" -c core.hooksPath=" + hooks
)

func TestArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "resolve the default branch",
			args: receiver.ResolveArgs(url),
			want: "-c protocol.file.allow=never -c fetch.fsckObjects=true -c fetch.recurseSubmodules=no" +
				" ls-remote --symref " + url + " HEAD",
		},
		{
			name: "init",
			args: receiver.InitArgs(dir, hooks),
			want: "--git-dir=" + dir + " " + options + " init -q --bare --template=" + hooks,
		},
		{
			name: "fetch",
			args: receiver.FetchArgs(dir, hooks, url, feature, false),
			want: "--git-dir=" + dir + " " + options + " fetch -q --no-write-fetch-head " + url +
				" +refs/heads/*:refs/heads/* +refs/tags/*:refs/tags/* +refs/heads/feature:refs/heads/feature",
		},
		{
			name: "fetch with progress",
			args: receiver.FetchArgs(dir, hooks, url, feature, true),
			want: "--git-dir=" + dir + " " + options + " fetch -q --progress --no-write-fetch-head " + url +
				" +refs/heads/*:refs/heads/* +refs/tags/*:refs/tags/* +refs/heads/feature:refs/heads/feature",
		},
		{
			name: "bundle",
			args: receiver.BundleArgs(dir, hooks),
			want: "--git-dir=" + dir + " " + options + " bundle create -q - --branches --tags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, strings.Fields(tt.want), tt.args)
		})
	}
}

func TestParseResolve(t *testing.T) {
	t.Parallel()

	const sha = "b85448b044a0a6be88f7cc914945caee2d6e89c9"

	tests := []struct {
		name    string
		out     string
		want    string
		wantErr error
	}{
		{name: "default branch", out: "ref: refs/heads/main\tHEAD\n" + sha + "\tHEAD\n", want: "main"},
		{name: "detached head", out: sha + "\tHEAD\n", wantErr: receiver.ErrNoDefaultBranch},
		{name: "empty repository", out: "", wantErr: receiver.ErrNoDefaultBranch},
		{name: "a branch is not HEAD", out: sha + "\t" + feat + "\n", wantErr: receiver.ErrNoDefaultBranch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := receiver.ParseResolve(tt.out)

			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()

	forge := newForge(t, true)

	got, err := (&receiver.Git{}).Resolve(t.Context(), forge.url)

	require.NoError(t, err)
	require.Equal(t, branch, got)
}

// TestResolveRefusesALocalPath is H1: a repository is a forge URL, and git itself refuses a path
// of the host, bare or as file://.
func TestResolveRefusesALocalPath(t *testing.T) {
	t.Parallel()

	forge := newForge(t, false)

	for _, path := range []string{forge.gitDir, "file://" + forge.gitDir} {
		var stderr bytes.Buffer
		_, err := (&receiver.Git{Stderr: &stderr}).Resolve(t.Context(), path)

		require.Error(t, err)
		require.Contains(t, stderr.String(), "not allowed")
	}
}

func TestResolveUnreachable(t *testing.T) {
	t.Parallel()

	forge := newForge(t, false)

	var stderr bytes.Buffer
	_, err := (&receiver.Git{Stderr: &stderr}).Resolve(t.Context(), forge.server.URL+"/nope.git")

	require.Error(t, err)
	require.NotEmpty(t, stderr.String())
}

// TestFetch checks what enters the receiver: the branches and the tags under their names, the
// whole history, and nothing else of the forge, with no remote and no hook.
func TestFetch(t *testing.T) {
	t.Parallel()

	forge := newForge(t, true)

	repo, err := (&receiver.Git{}).Fetch(t.Context(), forge.url, feature)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	gitDir := receiver.GitDir(repo)
	require.Equal(t, []string{feat, main, v1}, refs(t, gitDir))
	require.Equal(t, "2", strings.TrimSpace(git(t, "--git-dir="+gitDir, "rev-list", "--count", main)))
	require.NotContains(t, git(t, "--git-dir="+gitDir, "config", "--list", "--local"), "remote.")
	require.NoDirExists(t, filepath.Join(gitDir, "hooks"))
}

// TestFetchUnknownBranch: git refuses the fetch of a branch the forge does not have, before any
// download, and says which one.
func TestFetchUnknownBranch(t *testing.T) {
	t.Parallel()

	forge := newForge(t, true)

	var stderr bytes.Buffer
	_, err := (&receiver.Git{Stderr: &stderr}).Fetch(t.Context(), forge.url, "nope")

	require.Error(t, err)
	require.Contains(t, stderr.String(), "couldn't find remote ref refs/heads/nope")
}

func TestBundleAndClose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rich bool
		want []string
	}{
		{name: "branches and tags", rich: true, want: []string{feat, main, v1}},
		{name: "one branch, no tag", want: []string{main}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			forge := newForge(t, tt.rich)
			repo, err := (&receiver.Git{}).Fetch(t.Context(), forge.url, branch)
			require.NoError(t, err)

			bundle := filepath.Join(t.TempDir(), "cove.bundle")
			f, err := os.Create(bundle) //nolint:gosec // G304: the path is under t.TempDir.
			require.NoError(t, err)
			require.NoError(t, repo.Bundle(t.Context(), f))
			require.NoError(t, f.Close())

			git(t, "bundle", "verify", bundle)
			require.Equal(t, tt.want, heads(t, bundle))

			require.NoError(t, repo.Close())
			require.NoDirExists(t, receiver.Dir(repo))
			// Closing twice is not an error: Fetch closes on failure and the caller closes too.
			require.NoError(t, repo.Close())
		})
	}
}

// TestFetchRefusesAnObjectGitCloneWouldAccept: validating the objects on entry is cove's,
// not a default of git, so the failure has to say so. The malformed commit here, an author line
// with no space before the email, is one a plain clone takes without a word.
func TestFetchRefusesAnObjectGitCloneWouldAccept(t *testing.T) {
	t.Parallel()

	forge := newForge(t, false)
	tree := strings.TrimSpace(git(t, "--git-dir="+forge.gitDir, "rev-parse", main+"^{tree}"))
	bad := hashObject(t, forge.gitDir, "tree "+tree+"\nauthor B<b@example.invalid> 1 +0000\n"+
		"committer B<b@example.invalid> 1 +0000\n\nmalformed\n")
	git(t, "--git-dir="+forge.gitDir, "update-ref", "refs/heads/old", bad)

	// The premise: git itself takes this repository when nobody asks it to check.
	git(t, "clone", "-q", "--bare", forge.url, filepath.Join(t.TempDir(), "plain.git"))

	var stderr bytes.Buffer
	_, err := (&receiver.Git{Stderr: &stderr}).Fetch(t.Context(), forge.url, branch)

	require.Error(t, err)
	require.Contains(t, stderr.String(), "missingSpaceBeforeEmail")
	require.Contains(t, err.Error(), "fetch.fsckObjects")
}

// hashObject writes body as a commit of the repository at gitDir, malformed on purpose, and
// returns its name.
func hashObject(t *testing.T, gitDir, body string) string {
	t.Helper()

	//nolint:gosec // G204: the arguments are the test's.
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir="+gitDir,
		"hash-object", "-t", "commit", "-w", "--stdin", "--literally")
	cmd.Stdin = strings.NewReader(body)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, stderr.String())
	return strings.TrimSpace(string(out))
}

// TestFetchLeavesTheHooksOfTheOwner is decision 3 of the design: a core.hooksPath or an
// init.templateDir of the owner would run their hooks on every ref the receiver writes; the
// receiver runs none. The trap is armed on the global configuration, so the test cannot be
// parallel.
func TestFetchLeavesTheHooksOfTheOwner(t *testing.T) {
	forge := newForge(t, true)

	// The hooks record every repository they run in.
	log := filepath.Join(t.TempDir(), "hooks.log")
	hooksDir := filepath.Join(t.TempDir(), "hooks")
	template := filepath.Join(t.TempDir(), "template")
	require.NoError(t, os.MkdirAll(filepath.Join(template, "hooks"), 0o750))
	require.NoError(t, os.Mkdir(hooksDir, 0o750))
	hook := "#!/bin/sh\necho \"$PWD\" >> " + log + "\n"
	for _, path := range []string{
		filepath.Join(hooksDir, "reference-transaction"),
		filepath.Join(template, "hooks", "reference-transaction"),
	} {
		require.NoError(t, os.WriteFile(path, []byte(hook), 0o700)) //nolint:gosec // G306: a hook must be executable.
	}
	config := filepath.Join(t.TempDir(), "gitconfig")
	require.NoError(t, os.WriteFile(config,
		[]byte("[core]\n\thooksPath = "+hooksDir+"\n[init]\n\ttemplateDir = "+template+"\n"), 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "elsewhere"))

	// The trap is live: a plain fetch of the owner runs the hook.
	plain := filepath.Join(t.TempDir(), "plain.git")
	git(t, "init", "-q", "--bare", plain)
	git(t, "--git-dir="+plain, "fetch", "-q", forge.url, "+refs/heads/*:refs/heads/*")
	require.FileExists(t, log)
	require.NoError(t, os.Remove(log))

	repo, err := (&receiver.Git{}).Fetch(t.Context(), forge.url, branch)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })
	require.NoError(t, repo.Bundle(t.Context(), &bytes.Buffer{}))

	require.NoFileExists(t, log)
	require.NoDirExists(t, filepath.Join(receiver.GitDir(repo), "hooks"))
}

// forge is a repository served over HTTP by the git of the host, as a forge would.
type forge struct {
	server *httptest.Server
	// gitDir is the bare repository on disk, and url the address the receiver reaches it at.
	gitDir string
	url    string
}

// newForge builds a repository with two commits on main and serves it. When rich, it also carries
// a branch feature, a tag v1, and the refs a forge keeps that must never enter a receiver:
// refs/replace/, refs/notes/ and refs/merge-requests/.
func newForge(t *testing.T, rich bool) forge {
	t.Helper()

	work := filepath.Join(t.TempDir(), "work")
	git(t, "init", "-q", "-b", branch, work)
	require.NoError(t, os.WriteFile(filepath.Join(work, "README"), []byte("hello\n"), 0o600))
	git(t, "-C", work, "add", "README")
	commit := func(msg string) {
		git(t, "-C", work, "-c", "user.name=t", "-c", "user.email=t@example.invalid",
			"commit", "-q", "--allow-empty", "-m", msg)
	}
	commit("one")
	commit("two")

	root := t.TempDir()
	gitDir := filepath.Join(root, "repo.git")
	// A clone with --no-tags and a single branch, then the refs by hand: what the forge holds is
	// then exactly what the test lists.
	git(t, "clone", "-q", "--bare", "--no-tags", "--single-branch", "-b", branch, work, gitDir)
	if rich {
		head := strings.TrimSpace(git(t, "--git-dir="+gitDir, "rev-parse", main))
		first := strings.TrimSpace(git(t, "--git-dir="+gitDir, "rev-parse", main+"~1"))
		for _, ref := range []string{feat, v1, "refs/replace/" + first, "refs/notes/commits", "refs/merge-requests/1/head"} {
			git(t, "--git-dir="+gitDir, "update-ref", ref, head)
		}
	}

	execPath := strings.TrimSpace(git(t, "--exec-path"))
	server := httptest.NewServer(&cgi.Handler{
		Path: filepath.Join(execPath, "git-http-backend"),
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	})
	t.Cleanup(server.Close)
	return forge{server: server, gitDir: gitDir, url: server.URL + "/repo.git"}
}

// git runs the git of the host with args and returns its stdout, failing the test on any error.
func git(t *testing.T, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // G204: the arguments are the test's.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), stderr.String())
	return string(out)
}

// refs returns the refs of the repository at gitDir, sorted.
func refs(t *testing.T, gitDir string) []string {
	t.Helper()

	return names(t, git(t, "--git-dir="+gitDir, "show-ref"))
}

// heads returns the refs of the bundle, sorted.
func heads(t *testing.T, bundle string) []string {
	t.Helper()

	return names(t, git(t, "bundle", "list-heads", bundle))
}

// names reads the ref names out of lines of "<sha> <ref>", sorted.
func names(t *testing.T, out string) []string {
	t.Helper()

	var refs []string
	for line := range strings.Lines(out) {
		_, ref, ok := strings.Cut(strings.TrimSpace(line), " ")
		require.True(t, ok, "line %q", line)
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	return refs
}

// TestFetchWithoutAStderrDoesNotPanic: Stderr is optional, as everywhere else in the package, and
// a Git without one must still be able to report a failure.
func TestFetchWithoutAStderrDoesNotPanic(t *testing.T) {
	t.Parallel()

	forge := newForge(t, false)

	_, err := (&receiver.Git{}).Fetch(t.Context(), forge.server.URL+"/nope.git", branch)

	require.Error(t, err)
}

// TestFetchIgnoresAnInheritedObjectDirectory: --git-dir covers GIT_DIR only. A caller that is
// itself a git command exports more than that, and an inherited object directory would send the
// whole history of the forge outside the receiver, where Close would leave it on the host. The
// trap is set on the environment, so the test cannot be parallel.
func TestFetchIgnoresAnInheritedObjectDirectory(t *testing.T) {
	forge := newForge(t, false)
	elsewhere := t.TempDir()
	for _, name := range []string{"GIT_OBJECT_DIRECTORY", "GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		t.Setenv(name, filepath.Join(elsewhere, "inherited"))
	}
	t.Setenv("GIT_OBJECT_DIRECTORY", elsewhere)

	repo, err := (&receiver.Git{}).Fetch(t.Context(), forge.url, branch)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, repo.Close()) })

	require.Empty(t, files(t, elsewhere), "objects of the forge outside the receiver")
	require.NotEmpty(t, files(t, filepath.Join(receiver.GitDir(repo), "objects")))
	require.NoError(t, repo.Bundle(t.Context(), &bytes.Buffer{}))
}

// files returns the regular files under dir.
func files(t *testing.T, dir string) []string {
	t.Helper()

	var found []string
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			found = append(found, path)
		}
		return nil
	}))
	return found
}

// TestTranscriptKeepsTheEnd: git names the check that refused the repository after the progress of
// the download, so a transcript that kept the beginning would hold megabytes of progress and miss
// the one line that explains the failure.
func TestTranscriptKeepsTheEnd(t *testing.T) {
	t.Parallel()

	var said receiver.Transcript
	_, err := io.WriteString(&said, strings.Repeat("Receiving objects:  42%\r", 4096))
	require.NoError(t, err)
	_, err = io.WriteString(&said, "error: object abc: badTimezone\nfatal: fsck error in packed object\n")
	require.NoError(t, err)

	require.True(t, receiver.Contains(&said, "fsck error"))
	require.LessOrEqual(t, len(receiver.Kept(&said)), receiver.Limit)
}
