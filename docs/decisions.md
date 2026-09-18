# Decisions

A log, newest first. Each entry says what was decided, why, and what was
rejected.

## 2026-09-18: dependencies are vendored

**Decided.** `vendor/` is committed. A dependency enters with
`go get <module>@latest`, and the version that command resolves is what counts
from then on: pinned in `go.mod`, its sum in `go.sum`, its source under
`vendor/`, the three changed in the same commit as the code that first imports
the packages. `go mod vendor` follows every change of `go.mod`; the build reads
the directory by default once it exists, so nothing else is configured.

**Why.** Every line the binary links can be read in review: a bump of a
dependency shows as a diff of source, where `go.sum` alone shows a changed
hash. The build no longer depends on the proxy, so a CI job, a bisect or an
offline build gives the same binary, and a module withdrawn upstream cannot
break it. The trust base becomes visible at a glance, and the figure is the
reference the next addition is measured against, `pull` included: 13 modules,
63 packages, 723 files, 12 MB on disk; the binary links 9 of the modules, the
other four (testify, go-cmp, yaml, gotest.tools) serve the tests only.

**Rejected.** `go.sum` alone (integrity, not readability). A vendored slice
without the modules the tests need (`go mod vendor` does not distinguish, and
the test dependencies are part of what is read). A tool that audits the module
graph instead (it reports; the vendored diff is the audit).

**In the contract.** Each dependency is justified in this log, and the count
above is what a new one is compared to. "Standard library only" is no longer a
promise of the README; the smallest trust base that does the job is.

## 2026-09-17: `pull` links go-containerregistry

**Decided.** The image pipeline links `github.com/google/go-containerregistry`
v0.22.1, published 2026-09-04 and still the latest on the proxy on
2026-09-18: `pkg/name` for the references, `pkg/v1/remote` for the registry,
`authn.DefaultKeychain` for the credentials the user already has, `pkg/v1`
and the reading of `pkg/v1/layout` for the store. Later, `mutate.Extract`
flattens the layers into one tar with the whiteouts applied; the untar into a
rootfs stays cove's own code, since it is the one step that parses hostile
data on the host and it is short enough to be read whole.

**Measured** on the same program written twice (parse of a reference,
platform `linux/arm64`, keychain, pull), compiled with Go 1.26 on macOS 26.5:

- **Trust base.** go-containerregistry brings 9 modules, itself included, and
  61 non standard packages into the probe. containers/image v5.36.2 brings 54
  modules and 301 packages, among them `containers/storage`, `docker/docker`,
  `grpc`, `protobuf`, four `sigstore` modules, `go-jose`, `miekg/pkcs11`,
  `letsencrypt/boulder`, `mattn/go-sqlite3` and `mpb` for progress bars. The
  probe binary weighs 10.1 MB against 26.5 MB. The cove binary with `pull`
  weighs 10.4 MB, and 30 packages of the library are vendored.
- **cgo.** containers/image does not build as it comes: `proglottis/gpgme`
  wants the native library, `-tags containers_image_openpgp` removes it, and
  `mattn/go-sqlite3` stays in the graph for the blob info cache.
  go-containerregistry builds under `CGO_ENABLED=0` untouched.
- **Credentials.** `authn.DefaultKeychain` is a cascade, first found served,
  never a merge: the docker config, `$DOCKER_CONFIG` naming its directory in
  the place of `~/.docker` when set; else the file `$REGISTRY_AUTH_FILE`
  names; else `containers/auth.json` under `$XDG_RUNTIME_DIR`, then under
  `$XDG_CONFIG_HOME` (`~/.config` by default). The credential helpers of the
  file kept are called through `docker/cli` and `docker-credential-helpers`,
  the modules podman uses for the same job. Two consequences, verified against
  a registry served by the tests: a docker config that does not know the
  registry hides a `containers/auth.json` that does, and a `$DOCKER_CONFIG`
  pointing at a directory without `config.json` gives an empty configuration
  even when `~/.docker/config.json` exists.
- **Retries.** Three attempts, one second then three of wait with 10 % of
  jitter, on transient network errors and on 408, 429, 499, 500, 502, 503, 504
  and 522. Kept as they are; the library says nothing by default, so cove
  writes each new attempt on stderr.

**Why.** containers/image carries the needs of podman: several sources,
several destinations, a trust policy for an enterprise. Cove pulls one image
from one registry into a store it reads itself, and 45 extra modules cannot
be justified by features the target rules out. Integrity does not come from
the library either way: the manifest is named by its own sha256 and lists the
sha256 of each layer, and cove verifies both on the way into the store.

**Given up, and what it would cost to get back.**

- **Mirrors of `registries.conf`**, the only real loss: a `[[registry]]`
  block rewrites the name the user wrote and lists hosts to try in order,
  which serves a cache in the datacenter and a way around the rate limit of
  Docker Hub. A table from written name to hosts plus a retry loop, not a
  reason to link the rest.
- **Signature verification** by `policy.json`, GPG through `gpgme` or sigstore
  through Fulcio. It answers "who produced these bytes", a question the target
  does not ask while an image is pinned by digest and that digest is in the
  report. The day it is asked, `sigstore-go` alone answers it.
- **The `containers-storage:` destination**, which keeps the layers separate
  for the kernel to stack with overlayfs. Cove wants the opposite, one
  flattened rootfs per digest and a throwaway copy per run, on a host that is
  macOS first. The other transports are covered: `pkg/v1/layout` is `oci:`,
  `pkg/v1/tarball` is `docker-archive:`, `pkg/v1/daemon` is `docker-daemon:`.

Two more settings of `registries.conf` are not losses.
`unqualified-search-registries` would make the origin of an image depend on
the machine, which the rule on references already refuses. `blocked` is weaker
than the domain list of the run, which decides the same thing for every
connection of the VM.

**Rejected.** containers/image, on the count above. `skopeo` or `podman` as a
subprocess, which breaks the promise of one binary with no other tool to
install and puts the image policy in a program cove does not ship. The
`docker` CLI for the same reason, plus a daemon.

**In the contract.** The image of a run is named by digest and that digest is
in the report; the pull verifies it, and no library choice moves that
guarantee.

## 2026-09-13: the agent runs without permission prompts

**Decided.** `send` starts `claude` with `--dangerously-skip-permissions`, in
both regimes, attached terminal included; no option of `send` and no setting
of the sandbox brings the prompts back. The disclaimer Claude Code shows once
before starting in that mode is answered by the first launch state of the
image (`bypassPermissionsModeAccepted` in `claude.json`), next to the
onboarding and the trust of `/work`.

**Why.** The sandbox is the boundary: a prompt inside it protects nothing the
VM does not already contain, and costs the run. Without the flag the README
promise was broken in both regimes. Driven, print mode never waits for an
answer: it denies the tool and goes on, so a turn that had to write a file or
run a command failed without anyone being asked. Attached, the human was asked
at every edit and command, the permission fatigue the target rules out first.
Docker Sandboxes (`sbx run claude` is `claude --dangerously-skip-permissions`)
and yoloAI do the same, with no opt-out documented.

**Rejected.** Prompts kept in the attached regime, as if a human watching made
them useful (the boundary is the same in both regimes, and the attached
terminal is the same sandbox); an option of `send` to keep them (a caller who
wants prompts wants a workstation, not a sandbox); cove detecting an image
that runs the agent as root (Claude Code refuses the flag as root and says so,
and cove lets that refusal pass as it lets every refusal of `container exec`).

**In the contract.** Not being root outside a declared sandbox is the one
condition Claude Code puts on the flag; the image meets it as root since the
amendment of the entry of 2026-09-07. The prompts bypass
mode does not remove (`ask` rules, deletion of critical paths) stay with the
agent: driven, it denies them and the turn continues. `--permission-prompts
none` (2.1.259 and later) treats those residual prompts as an unsupervised
turn would: to consider at the next bump of the image.

## 2026-09-13: image profiles, named at `run`

**Decided.** `run --image` names the image of the VM, `cove-sandbox:local` by
default; nothing is remembered between runs, by VM or by repository, and the
repository declares nothing (target, 2.6). The preflight checks the image asked
for, before anything is spent: a name without a registry (`demo`,
`cove-sandbox:local`) is never looked for on the internet and must be in the
local store, one that names its registry (`ghcr.io/...`, `localhost:5000/...`)
is pulled when absent. Once the VM runs and before the repository enters it,
`claude --version` is the one check of the image, and a VM that fails it is
removed. A profile must keep the agent, `/work` as the working directory, the
first launch state of the agent in its home, and no command launched by
default; nothing else is verified. `images/go` is the maintained profile, Go
and golangci-lint pinned to the versions of the workstation, whose only
commitment is that cove builds, lints and tests in it.

**Why.** The base image carries no runtime: on a Go repository the agent reads
but can neither build nor test, so it cannot meet the definition of done of the
project. The rule of docker tells a registry from a bare name, and
`container run` on a bare name absent from the store queries docker.io and
fails 401 without saying why. The agent is checked in the VM that was created
rather than in a throwaway one: a boot costs 3 to 5 s, the abort path of the
seeding already exists, and a wrong image is the exception. The profile is not
a barrier because what the agent installs disappears with the VM; it pins what
the definition of done needs.

**Rejected.** A memory of the image per VM or per repository (the caller
declares); a check of the other profile rules (it would test the Dockerfile
against itself, as the verification script of the base image did); a version in
the tag of the profile (the build command would change at every bump); a
profile that repeats the pinning and bump procedure of the base image (cove
building and testing in it stands in for a verification). The limitation
accepted then, the agent as uid 1000 without sudo so that a profile had to
install its tools as root and restore the user, is lifted by the amendment of
the entry of 2026-09-07.

**In the contract.** The image of a run is the caller's declaration, whole,
and `list` shows the one actually used. The rules of a profile replace "the
user of the base image" and "nothing in the home": the user describes the base image, and the
home must keep the first launch state, not stay empty.

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
`python3`), the agent as root with `/root` holding only the first-launch
state of Claude Code, the repository at `/work`, no entrypoint, built locally
as `cove-sandbox:local`. The detailed spec, the acceptance and the bump
procedure are in [images/sandbox/README.md](../images/sandbox/README.md).

**Why.** The image is the declared context: what it pins is what runs, and
what the host would leak is absent rather than forbidden. A missing tool costs
context and sometimes the run, more than a few tens of megabytes.

**Amended 2026-09-16: root instead of uid 1000.** The image first ran the
agent as a dedicated user, uid 1000 without sudo, and its installation rule
(everything in user space) followed from it. That user was never a
confinement choice but a workaround: Claude Code refuses its unsupervised
mode as root. The price was paid on legitimate destruction (need, objective
2): the agent could not install a package, recreate a test database, run
Docker or touch the global configuration, a read-only posture while nothing
it would break in the VM belongs to anyone. The boundary is the hypervisor
(target, 2.3), and Docker Sandboxes runs its agent as root for that reason.
Measured on the pinned 2.1.236, the refusal is lifted by `IS_SANDBOX=1` in
the environment, which the image now sets; the seeding and the agent run as
root, and a profile installs its tools with no user switch.

**Rejected.** Alpine (musl constraints on every layer), the install script and
the npm package (a launcher under `$HOME`, Node for nothing), a leaner second
image, a verification script (every check tested the Dockerfile against
itself), uid 1000 kept with sudo (the refusal stays under `sudo claude`, and
the friction with it), `CLAUDE_CODE_BUBBLEWRAP` as the variable that lifts the
check (it names a mechanism the image does not have). Accepted limitations:
apt packages are not pinned; `IS_SANDBOX` is undocumented, so the acceptance
runs the flag as root at every bump.

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
