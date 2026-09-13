# Decisions

A log, newest first. Each entry says what was decided, why, and what was
rejected.

## 2026-09-12: what this base drops from the previous framing

The previous design documents were organised around one VMM and a merge
request flow. Kept: the dated entries below. Dropped, with the reason:

- **A LAN filter inside the guest** (nftables set as root before the agent):
  it contradicts "no control lives in the VM". The single route is imposed by
  the host or the infrastructure.
- **Internet open because there is nothing to steal**: replaced by egress
  denied by default and a domain list declared by the run. The list is a
  context control as much as an exfiltration control.
- **A broker on the workstation with a control face and a data face**: it
  becomes the proxy, a daemon deployed next to the sandboxes, with the same
  role and the per-run credential.
- **Inspection of the merge request content, push by SHA, `ci.skip`**: the
  forge side is out of scope. What comes out of a run is a branch or a diff and
  a trace; what the forge does with it is another project.
- **A catalogue of attack surfaces and numbered requirements**: the rules of
  the [need-and-target](need-and-target.md) carry their reasons instead.

## 2026-09-10: the whole repository in `/work`, without a remote

**Decided.** `run` takes a forge URL and a branch, never a local path or the
current directory; the forge default branch when none is given. The whole
repository enters the VM: every branch and tag under its own name, the full
history, checked out on the requested branch, and no remote at all. The agent
commits as `agent <agent@cove.invalid>`, a domain reserved by RFC 2606, never
signed with a key of the workstation.

**How.** The repository is read on the host with the access its owner already
has, into a bare receiver repository that only cove's argv configures: objects
validated on entry, submodules left empty, replace refs ignored, the owner's
global hooks silenced, a local path refused by git itself. Only branches and
tags cross; `refs/replace/`, `refs/notes/` and the merge request refs stay on
the forge. Everything that can fail does so before the VM exists. The objects
travel as a bundle on the stdin of `container exec`, then a `git fetch` of that
file inside the VM as the image user.

**Why.** Cove shares nothing with the user's machine; the owner's access stays
on the host and never enters the VM. A `git push` from inside fails for lack of
a destination and never asks for a credential. Signing the agent's commits with
the owner's key would destroy non-repudiation.

**Rejected.** A local clone as input; a shallow clone; `git clone` of the
bundle (branches under `refs/remotes/origin/`, a remote left pointing at the
bundle); `receive-pack` through `exec -i` (twice slower, an ssh helper); a
direct clone from the VM for public repositories (two mechanisms). Deferred: an
exact version (commit or tag) as the starting point, and a capability relay
that would give the sandbox a remote with wide read and narrow write.

## 2026-09-09: `send`, two regimes in one verb, cove owns the argv

**Decided.** `send` is a thin layer over `container exec`. Without a prompt it
attaches the agent's REPL to the terminal; with one it drives a single turn
and copies the JSON of `claude` to stdout, unparsed and unfiltered. Cove draws
a UUID and imposes it as the session identifier, so that no byte read from the
VM ever becomes an identifier; `-n` names a thread, `-r` resumes one by UUID or
name, `-c` continues the last attached one (attached only: `claude` keeps no
record of driven threads for it). Every send without `-r` opens a new thread;
several threads share `/work` and cove arbitrates nothing between them.

**Why.** Compared against Docker Sandboxes, container-use, Fletch and the
Claude Code, Codex and Cursor CLIs: `sbx run` is the only harness carrying both
regimes in one verb, and the user wanted that. Cove types its own flags rather
than passing the agent's argv through, because a pass-through lets the user
overwrite the flags the output contract depends on.

**Rejected.** A text output mode, the only path that would need an escape
filter; filtering ANSI escapes at all, since JSON escapes control characters
by construction and a PTY cannot be protected; reading the session identifier
back from the agent's output; one thread per sandbox.

**In the contract.** The send is the third level of the declaration: the
model and the prompt are chosen here, at each turn, not at the creation of the
VM. The detached verb is this driven regime with a budget, a lease and a trace
on top; the attached regime stays as the interactive verb, with no resumed
session across VMs.

## 2026-09-08: the CLI is shaped like docker, podman and Apple container

**Decided.** Verbs and flags start from what docker, podman and Apple
`container` do, market standards developers already know, and diverge only
where isolation demands it. `run` creates the sandbox and nothing else, with
the flags of `docker run` its role justifies (`--name`, `--rm`, `--keep`,
`--cpus`, `-m`, `-e`) and no other: cove builds the argument array itself, so
a mount, a user, a working directory, a network option, the SSH agent or a
command cannot even be asked for. `stop` and `list` (aliases `ls`, `ps`) act
only on VMs carrying the label `cove=sandbox`; `--all` is a filtered loop,
never `container stop --all`; an unknown name exits 1, a cove-side failure
125, a usage error 2. The stop delay stays the default of `container`.

**Why.** Developers reuse what they know; the VM store of `container` is
shared with other VMs (Apple's image builder), so cove never stops what it did
not create. The Go standard library does the dispatch and the flags: a static
binary with minimal dependencies, no cobra. Every process is started with an
argument array, never a shell string, because a shell would interpret exactly
the bytes cove must only transport.

**Rejected.** `-w`, a free command, `-i` and `-t` on `run`; a refusal list of
flags (the accepted flags are the only ones defined, the rest does not exist);
the Fletch model with the agent as process 1 and the VM dying with it;
podman's 125 for an unknown name and its `--ignore`.

**In the contract.** The flags of `run` are the run level of the declaration,
`-e` first: a variable is a declared context entry, not a pass-through, and no
credential travels this way.

## 2026-09-08: a waiting process 1 behind the init of `container`

**Decided.** The VM boots `sleep infinity` behind `--init`, a placeholder for
a cove process inside the VM.

**Why.** Nothing occupies the VM between two turns, and Linux treats process 1
apart: a signal with a default action is not delivered to it, and the orphans
of an `exec` are reparented to it. Measured on 1.3.1: without the init, `stop`
waits its kill delay (5 s, exit 137) and leftovers stay zombies; with it,
signals are forwarded, children reaped, and the exit code of the child is
returned. GNU `sleep` accepts `infinity` because it parses a float, which the
Debian base guarantees.

**In the contract.** The lease of the VM, the duration cap and the relay of the agent's
stdio to `logs` need a resident process; it lives in this slot, holds no
secret, and everything it returns is untrusted.

## 2026-09-07: the sandbox image

**Decided.** Debian trixie slim pinned by index digest, the native Claude Code
binary pinned by exact version and SHA256 with updates disabled, `git` and the
tools the model reaches for on its own (`curl`, `jq`, `patch`, `procps`,
`python3`), a dedicated user `agent` (uid 1000) with a home holding only the
first-launch state of Claude Code, the repository at `/work`, no sudo, no
entrypoint, built locally as `cove-sandbox:local`. The detailed spec, the
acceptance and the bump procedure are in [images/sandbox/README.md](../images/sandbox/README.md).

**Why.** The image is the declared context: what it pins is what runs, and
what the host would leak is absent rather than forbidden. Claude Code refuses
its unsupervised mode as root. A missing tool costs context and sometimes the
run, more than a few tens of megabytes.

**Rejected.** Alpine (musl constraints on every layer), the install script and
the npm package (a launcher under `$HOME`, Node for nothing), a leaner second
image, a verification script (every check tested the Dockerfile against
itself). Accepted limitation: apt packages are not pinned.

**In the contract.** The image is named by digest; a project image extends
this one with its toolchain.

## 2026-09-07: driving Apple `container` by its CLI, and what was measured

**Decided.** Cove runs the `container` CLI with argument arrays and parses only
the JSON of `list` and `inspect`. The Swift framework is out of reach of Go
without cgo, and the API server speaks only XPC.

**Measured** on `container` 1.3.1, macOS 26.5.2, kata kernel 6.18.35, and
relied upon by the code:

- **Exit codes.** Every failure of the CLI is 1 with `Error:` on stderr,
  indistinguishable from an exit 1 of the guest; the code of process 1 becomes
  that of `run` (137 on kill, on `stop` after its delay, on `delete -f`, on
  OOM). `stop a b` continues past an unknown name and exits 1; `inspect a b`
  fails as a whole. The identifier is the name, exact, never a prefix.
  `delete` refuses a running VM without `-f`. Without the image in the local
  store, `run` queries docker.io and fails 401, hence an image check first.
- **Orphans.** The VM belongs to a launchd service: `kill -9` of the CLI
  leaves it running, `--rm` included; SIGINT or SIGTERM to the CLI is an XPC
  error and the VM goes on. Destruction is an explicit verb, never a side
  effect. This is why the lease of the contract exists.
- **Network.** One address per VM in `192.168.64.0/24` on the NAT network
  `default`, sequential, read from `list` after start. The host is reachable
  at the gateway `192.168.64.1` only, on `bridge100`, which exists only while
  a VM runs; a listener bound to the gateway answers the VM and is refused
  from the LAN address and from `127.0.0.1`. The host never reaches the VM.
  The LAN, the internet and the other VMs of the same network are reachable;
  two NAT networks ignore each other, so one network per run isolates runs;
  `--internal` cuts everything, gateway included. DNS is relayed by the
  gateway. The single route of the contract is therefore pf on the host,
  per run, to demonstrate.
- **Bytes without a mount.** `cp` (writes as root), the stdin of `exec -i`
  and the stdout of `exec`, all on a running VM; 300 MB in under a second each
  way. Git reads a bundle only from a regular file.
- **Terminal.** `-it` on `run` and `exec`, no `attach`; a host TTY is
  required; the guest gets a PTY, the size is propagated, bytes pass raw,
  Ctrl-C reaches the guest process.
- **Resources.** `--cpus` (default 4) and `--memory` (default 1 GB); the guest
  sees one more CPU and 100 MB more; exceeding memory is an OOM kill, 137.
  Disk and duration have no flag: a sparse root filesystem of 513 GB, freed at
  `delete`. Boot takes 3 to 5 s.
