package codebase

import "slices"

// Work is the directory of the repository in the sandbox, owned by root like the agent.
const Work = "/work"

// bundlePath is where the bundle lands in the VM for the time of the fetch. It doubles the disk
// space of the repository until it is removed; the agent does not run yet, so nothing races it.
const bundlePath = "/tmp/cove.bundle"

// SeedSpec describes the seeding of Work in a sandbox that was just created: the bundle of the
// receiver becomes a repository on Branch, with every branch and tag of the forge as local ones and
// no remote, and the identity the agent commits under.
type SeedSpec struct {
	// Target is the sandbox to seed, by name or ID, resolved by the backend.
	Target string
	// Branch is the branch the agent starts on; it must be in the bundle.
	Branch string
	// Author and Email are the identity written to the config of the repository: without one git
	// refuses to commit at all. They live in Work, not in the image nor in $HOME.
	Author string
	Email  string
}

// Step is one command of the seeding, as it runs inside the VM.
type Step struct {
	// Args is the command line, the program first.
	Args []string
	// Bundle tells whether the step reads the bundle on its stdin.
	Bundle bool
}

// Steps returns the commands that seed Work, in order, each a fixed program with fixed arguments:
// no shell and no user override, each runs as the user of the image, root, like the agent
// (images/sandbox/README.md). A backend carries them into the VM, the one marked Bundle with the
// bundle on its stdin.
//
// The refspecs are those of the receiver, so what leaves the host and what enters the sandbox are
// the same set, written once.
//
// The repository is initialized on Branch, so HEAD names it, unborn, before the fetch; the fetch
// refuses to write the branch HEAD names, even unborn, unless --update-head-ok (measured), and it
// leaves the index and the tree empty, which reset --hard fills from HEAD, the documented meaning
// of the verb. The identity refspecs copy every branch and tag under its own name: the repository
// is new, nothing is there to collide with. Nothing sets a remote: a push fails for lack of a
// destination, without asking for a credential.
func (s SeedSpec) Steps() []Step {
	git := []string{"git", "-C", Work}
	return []Step{
		{Args: append(slices.Clone(git), "init", "-q", "-b", s.Branch)},
		{Args: []string{"cp", "/dev/stdin", bundlePath}, Bundle: true},
		{Args: append(append(slices.Clone(git), "fetch", "-q", "--update-head-ok", "--no-write-fetch-head",
			bundlePath), refspecs...)},
		{Args: []string{"rm", bundlePath}},
		{Args: append(slices.Clone(git), "reset", "-q", "--hard")},
		{Args: append(slices.Clone(git), "config", "user.name", s.Author)},
		{Args: append(slices.Clone(git), "config", "user.email", s.Email)},
	}
}
