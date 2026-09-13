# Cove: a report on the need and the target

The order of the reasoning is deliberate: state the need, then describe the
target. The survey of solutions ([sandboxes-ecosystem.md](sandboxes-ecosystem.md))
comes after, to check that the principles of section 1 are not local
inventions and to bring back the ideas worth taking.

## 1. The need

### 1.1 Two usages, one tool

**Usage A, the developer's workstation.** A dev runs a coding agent on a
repository, on their Mac, and lets it work without approving every command.

**Usage B, unsupervised processing.** A Merge Request opens on GitLab, an
agent takes it end to end, with no human watching. Several MRs are processed
in parallel. The GitLab side (webhook, tokens, comments) is out of scope for
this document.

Both usages must share the same environment, the same policy and the same
budget. What the dev runs at home is exactly what the runner will run on the
MR. Without that continuity, two tools are better than one.

### 1.2 Why an agent sandbox

Today, the only thing bounding an agent is the human watching it. That human
does not scale and, after a few hours, stops reading: **permission fatigue**
makes the answer a reflex and the prompt a formality. Nor do they know what
is running: half of what determines the agent's behavior was never chosen
for this run. It comes from the workstation, it has accumulated, and it is
visible nowhere.

The sandbox does two things. It allows a move from a model of **supervision**
to a model of **infrastructure**: the boundary is set once, review shifts from
every action to the result. And it **replaces what is inherited with what is
declared**: what enters the run is named, the rest does not enter.

Everything else follows from that.

### 1.3 Two conceptions of the sandbox

The word covers two ideas that do not lead to the same product.

The **enclosure** is a protected place where one works: it persists, one comes
back to it, the workstation's code is mounted into it, the boundary is
negotiated along the way, and one enters it by hand since that is the point.
It is optimized for comfort and immediate feedback.

The **disposable environment** is materialized for a task from a declaration:
it exists for the time of the run, the code is copied into it, the boundary is
fixed beforehand and does not move, nobody enters it by hand. It is optimized
for reproducibility and parallelism.

| | Enclosure | Disposable environment |
|---|---|---|
| What it is | A place where I work | An environment for a task |
| Lifetime | Persists, one comes back to it | The time of the run |
| The code | Mounted from the workstation | Copied into it |
| The boundary | Negotiated along the way, one click to unblock | Declared beforehand, never moves |
| One enters it | Yes, that is the point | No, never by hand |
| Optimized for | Comfort, immediate feedback | Reproducibility, parallelism |

Nearly every existing tool builds an enclosure, and that explains the
landscape: three things one rarely finds there are not oversights, **they make
no sense in the first conception**. A cap per run, when the sandbox is not a
run but a place. A declared context, when finding one's things again is the
very point. Zero leftovers, when persistence is the service rendered.

The precedent is Docker. Its first years were spent using it as a lightweight
VM that one enters over ssh, in which one installs things, that one restarts.
It took years to arrive at the image as declaration, at the disposable
container, at state moved outside. The `-v $(pwd):/app` with a shell inside
was the comfort that dissolved the boundary: it is exactly the mount of the
current directory that today's wrappers perform.

Cove takes the second conception. It is not a better enclosure, it is the
other premise.

**The bet.** The enclosure is not a mistake: it wins on comfort, and that is
what a dev at their machine wants. Our bet is that the disposable conception
serves usage A anyway, as the Docker image ended up serving local development,
because the counterpart of the constraint is that what runs at the dev's is
what will run on the MR. If that bet is wrong, the two usages diverge and two
tools are needed.

### 1.4 The objectives, in five families

**A. Let the agent work.** Without a boundary, the agent is held back well
before any security question: it asks for permission, dares destroy nothing,
steps on the other runs, saturates the workstation, and never leaves the
machine where it was launched.

1. **Zero permission fatigue.** Permission prompts always end up accepted
   without being read. The agent must run without raising a single one.
2. **Legitimate destruction.** Recreate the test database, install a package,
   change the global configuration, run Docker or Kubernetes. Without a
   boundary, the agent is condemned to a read-only posture.
3. **Non-interference.** The need behind parallelism is not N runs at a time,
   it is N runs without collision: ports, caches, git state, Docker daemon,
   shared datasets.
4. **Workstation availability.** Processor, memory and disk caps so that the
   machine stays usable while the agent works.
5. **Run portability.** The same work on the laptop, in continuous
   integration, on a cloud machine. Without a defined environment, "moving it
   to CI" is a rewrite.

**B. Bound.** What makes autonomy acceptable.

6. **Contained damage.** A bad command must erase neither a database nor a
   home directory.
7. **No effect outside the VM.** An agent hijacked, by the content of an MR or
   by a bug, must not be able to touch the host or to work around its
   restrictions by writing scripts.
8. **No ambient authority, and nothing monetizable inside.** On a workstation,
   the agent inherits the human's identity: SSH keys, production kubeconfig,
   cookies, tokens for everything. The need is not only to prevent a secret
   from leaking, it is that the agent hold none. What it carries must be
   worthless to whoever steals it: a credential valid only from the VM where
   it was placed, and only for the time of the run.
9. **Hard stop.** A loop must be cuttable with certainty, on budget or on
   duration. Destroying the VM is the only switch that works; killing a
   process tree is not.

**C. Declare the context.** An agent's behavior is determined by its context,
and that context is far wider than the prompt.

10. **Everything that enters is named.** Image pinned by digest, agent
    version, exact model identifier, instruction files, tool servers, hooks
    and skills, environment variables, repository commit, reachable domains,
    prompt. Nothing else.
11. **Zero leftovers.** No persistent memory, no resumed session, no cache or
    file from a previous run, no workstation configuration. What is not
    declared is absent.

What these two rules eliminate, and what acts today without our seeing it:

| Context input | What injects it without our knowing |
|---|---|
| Instruction files | The workstation's global file, on top of the repository's |
| Persistent memory | A note written during a previous run, on an assumption since gone false |
| Tools: MCP, hooks, skills, plugins | The workstation's configuration, different from one dev to the next |
| Agent version | The automatic update, between two runs |
| Model | An alias that moves, a switch on the provider's side |
| Toolchain and variables | Whatever is lying around on the workstation |
| Disk state | The remains of a previous run, a cache, an untracked file |
| What the agent reads on the internet | A page that has changed, or that carries an instruction |

That last line ties families B and C together: the list of allowed domains is
not only an exfiltration control, it is a context control. What the agent
reads determines what it does.

The decisive point: a fresh VM from a pinned image makes that absence
**structural**. On a workstation, the right flags would have to be set every
time, and a single automatic update undoes it all. Here there is nothing to
disable, the home directory is empty at startup.

**D. Make verifiable.** Without which usage B is unmanageable.

12. **Reproducibility.** The same context at every run. Otherwise a failure
    cannot be replayed and the result is as attributable to the machine as to
    the code.
13. **Reversibility.** There is no undo for an agent, other than destroying
    the environment. That is what allows us to state that nothing is left.
14. **Traceability.** A closed perimeter is what makes it possible to produce
    the trace of what ran and of what was done. Without a boundary, the
    question "what did it do" has no answer.

**E. Make improvable.** A run that goes badly must say what to correct,
otherwise usage B is a bet replayed identically.

15. **Cost is measured, not noticed after the fact.** Tokens and price per
    run, to know whether the task was worth its budget and to calibrate the
    cap of the next one.
16. **What the agent did is counted.** Calls per tool, failures per tool,
    breakdown of the context window in percentages: instructions, tool
    definitions, tool results, code read. A tool that fails nine times out of
    ten is a defect of the image or of the contract, not a defect of the
    agent. Tool definitions eating a third of the window before the first turn
    are a line of the contract to remove. This is the measured counterpart of
    family C: we declare the tools, the trace says what they cost.
17. **Blockages are reported.** What the agent wanted to reach and could not,
    the tools that failed repeatedly, the dead ends where it went round in
    circles before giving up.

That last rule is the counterpart of a declared list: what is not on it is
refused, and the agent hits it without anyone knowing. The feedback opens an
**iterative** unblocking: the run reports what it wanted to reach, the human
amends the declared contract, the next run is better. How far the refusal
goes, what the list contains when the run says nothing about it and whether it
widens along the way, is not settled.

One last point is not a technical objective but decides adoption: the sandbox
is the artifact shown to whoever must accept that an agent runs unsupervised.
What it produces is trust as much as security.

### 1.5 What we fear

The attacker is not a human breaking the VM. It is the content of an MR, of a
ticket or of a web page that hijacks the agent. The agent then obeys someone
else, with every right it was given. Three scenarios:

- **Exfiltration.** The agent sends the code or a token to a domain of its
  choosing. The internet is needed for documentation, so the internet is also
  the way out.
- **Runaway.** The agent loops and consumes the API budget of the whole
  account, or runs for hours without finishing.
- **Circumvention.** The agent, with sudo, undoes a firewall, changes its
  routes, disables a filter placed inside the VM.

Corollary: **no control can live in the VM.** Everything that protects must be
placed by the host or by the infrastructure, out of the agent's reach.

### 1.6 What the sandbox will never solve

- **Determinism of the result.** A language model is not reproducible. What we
  aim for is determinism of the **inputs**: they are enumerable and identical
  from one run to the next. Two runs may differ, but the difference is
  attributable to the model and not to the machine. That is what makes a
  failure analyzable and a result reviewable.
- **The scope of what we delegate.** The sandbox guarantees that no credential
  is held by the agent, not that those credentials are restricted. Model
  provider key, write access to the repository, registry identifier: what each
  one opens is decided where it is minted, with the fewest rights and the
  shortest lifetime possible. Without that, the proxy is only a pass-through
  to an over-broad right.
- **The content of the repository.** A committed secrets file or a git hook is
  readable and executable by the agent. The sandbox protects the host, not the
  repository from itself.
- **The safety of what comes out.** The code produced will one day run outside
  the sandbox. The sandbox bounds the making, not the product.

## 2. The target

### 2.1 In one sentence

One sandbox contract declared once, executed identically on the dev's Mac, on
a Linux runner and at a VM provider, with a hard budget and a
machine-readable trace for every run.

### 2.2 The sandbox contract

Each rule carries its reason, so that we know what we break by removing it.

| Rule | Why |
|---|---|
| A fresh VM per run, destroyed whatever happens | No cache or file from a previous run, no orphan left running; freshness is the construction, not a script |
| No writable host mount, the repository arrives by copy | A git hook or a CI config from a booby trapped MR never runs on the host |
| A single outbound route, towards cove's proxy | With sudo the agent undoes any internal filter; the single route is placed by the host or the infrastructure |
| Egress filtered by the proxy, on a list of domains declared by the run | The internet stays open for documentation, but only towards what this run has named. What the list contains when the run says nothing about it, and whether it widens during the run, remains to be settled |
| No credential in the VM, the proxy places them at the moment of going out | A secret the agent does not hold does not leak, whatever becomes of the agent |
| What the VM carries is worth something only from the VM: credential minted per run, dead with it | The content of the VM is exfiltrable; there must be nothing inside that serves elsewhere |
| Budget per run with a hard stop | A runaway costs at most the budget of the run, never that of the account |
| Duration and disk capped | A run that does not finish is a killed run, not a run that fills the disk |
| Declared context: image by digest, agent version, exact model, instructions, tools, variables | What determines behavior must be named, otherwise the run is neither replayable nor reviewable |
| No persistent memory, no resumed session, no shared store | A note from a previous run steers today's without anyone seeing it |
| Structured result: exit code, branch or diff, JSON trace | Usage B has no human to read a terminal |
| Feedback returned at every run: cost, activity, blockages | Without it, a refusal by the proxy is visible nowhere and nothing says what to correct for the next run |
| Destruction by a lease carried by the VM, never by the calling process | A caller that dies leaves the VM running, and nobody notices in programmatic usage |
| Boundary at the hypervisor, never at the shared kernel | A classic container shares the workstation's kernel; that is not a sandbox. And behind a hypervisor, autonomy and confinement stop being traded one against the other: what is granted inside does not cross the boundary |
| The interactive verb is not an enclosure: no resumed session, no persistent volume, no mount, even for the dev on their workstation | A comfort exception on side A makes the two usages diverge, and continuity is the whole thesis (1.3) |

### 2.3 What the agent can do inside

Everything. Sudo, Docker, k3s, compilers, whatever the image contains. The
boundary is outside; hardening the inside brings no further security, only
friction against objective 2. Guest hardening may stay as defense in depth,
but no security property rests on it.

One limit comes from the machine and not from the contract: Docker in the VM
requires no nested virtualization, a container being nothing but namespaces of
the guest kernel, but it does need a guest kernel with overlayfs and cgroups
v2. Whatever does require nested virtualization is possible only if the
backend offers it.

### 2.4 The proxy and the credentials

The proxy is the heart of the product. It alone goes out to the internet on
behalf of the VM, and it alone holds the means to authenticate. It does three
things:

1. **Filters egress** by domain, on the list the run declares, and keeps the
   log of the domains contacted.
2. **Carries the credentials** in place of the VM and places them on the
   request at the moment it goes out, when the destination matches. One
   adapter per provider: Anthropic first, OpenAI and Google next.
3. **Counts the budget** by reading the usage events of each provider, and
   cuts off when the cap is reached.

**What never enters the VM.** Model provider key, write access to the
repository, registry identifier, any credential that is current elsewhere.
They live in the proxy process, on the host or on the neighboring machine. The
agent sees a URL and a response, never the header that authorized it. There is
therefore nothing to find in a configuration file, an environment variable, a
cache or a history of the VM, including for an agent that has sudo and goes
looking.

**What the VM carries instead.** A credential minted for this run, built so as
to have no value outside its context:

| Property | Why |
|---|---|
| Minted per run, known to this run's proxy alone | Two parallel runs share nothing, and the neighbor's credential is of no use |
| Accepted from the route of this VM only | The network position is needed on top of the secret; presented from elsewhere, it is refused |
| Alive for the time of the run, invalid as soon as the VM is destroyed | Nothing survives the run, so nothing replays afterwards |
| With no right of its own: it names the run, it opens nothing | The right stays with the proxy, which decides destination by destination |

**What this presupposes.** Placing a header on an encrypted request requires
the proxy to terminate TLS, hence that an authority minted for the run be
trusted inside the VM. It too is worth nothing outside, for the same reasons
as the credential. The counterpart is that the proxy sees the traffic in the
clear: that is what lets it filter and count, and it is one more reason for it
to be readable and hosted by whoever uses it.

The statement that counts: **exfiltrating the entire VM yields nothing usable
outside.** What remains is the code of the repository, and it is the domain
list that holds it back.

**What this does not solve.** During the run, the hijacked agent makes the
proxy act in its name: it does not steal the key, it uses it by proxy. That
risk is not removed, it is bounded, by the named domains alone, by what the
proxy accepts to do on each of them, by the budget and by the duration. And
the budget counts only what goes through the proxy, hence the calls to the
model, not a third party service the agent might call. Subscription tokens are
counted in tokens and not in euros.

**Two axes of adapters.** The proxy sees only what passes through it: the
calls to the model, their cost, the domains. The tools, the breakdown of the
context and the blockages are legible only in the event stream of the agent
itself. So one adapter per provider is needed for the budget, and one adapter
per agent for the measurement (objectives 16 and 17). Agnosticism is graded,
like the backend matrix: any agent installable in the image runs, only those
with an adapter return a complete trace.

### 2.5 The backends and the single route

Cove drives the backends through an interface of five verbs: create a VM from
an OCI image, wire its network towards the single proxy, execute, copy bytes,
destroy. Each backend enforces the single route with its own means. A backend
that cannot guarantee it is published as such.

| Backend | Where | Single route enforced by | Status |
|---|---|---|---|
| VM provider (fly.io Machines) | Cloud | Network policy denying all egress except the port of the proxy deployed alongside; filtering by port and protocol, not by domain | First version, path of the programmatic usage |
| Apple `container` | The dev's Mac, macOS 26 | One NAT network per run, pf on the host allowing the proxy only | First version, path of the workstation |
| Kata or Firecracker | Linux machine with KVM | One tap per VM, nftables on the host | Next; assumes bare metal or a VM with nested virtualization |
| Docker `sbx` | Mac, Windows, Linux, or its cloud | Its deny all policy plus its upstream proxy pointed at cove's | Next; gives Windows, a proven VMM and a host with no effort |
| Sandbox provider with an SDK (e2b, Daytona) | Cloud | Depending on the provider, to be qualified line by line | To be qualified |

**Why the cloud first for usage B.** The case that speaks for an MR is a
service answering the webhook with no machine to maintain: nobody wants a Mac
switched on in a corner to process a team's MRs. A VM provider gives
parallelism without sizing a workstation, destruction billed by the second,
and a network policy placed by the infrastructure, hence out of the agent's
reach. It is also the only path when the machine that orchestrates is itself a
VM, where nested virtualization is not a given.

The two families cited are not equal for this contract. **fly.io Machines**
takes an OCI image, exposes a network policy per machine and leaves the guest
kernel open: the contract holds, Docker inside included, to be verified. The
**sandbox providers with an SDK** such as e2b start faster and manage the
lifecycle in our place, but the image and what runs inside it are more
constrained and the egress policy is the provider's: to be qualified rule by
rule before making a backend of it. In both cases the proxy is deployed next
to the sandboxes, not on the workstation.

### 2.6 Two verbs, one contract

- **Interactive**: the dev opens the agent in the VM, on their workstation,
  with the budget and the domain list of the project.
- **Detached**: a prompt goes in, a trace and an exit code come out. That is
  what the webhook calls, one VM per MR, N in parallel.

The only difference between the two is that a terminal opens. Everything else
is identical, including what does not persist: the dev on their workstation
does not resume a session and does not keep a volume, failing which they no
longer launch what the runner will launch.

Both receive the same declaration, and that declaration comes from the caller,
at three levels:

| Level | What is declared there | Why there |
|---|---|---|
| The image, pinned by digest | The agent and its version, its tools, the toolchain, the instruction files and the tool servers it embeds | What changes rarely and is built once |
| The run | The repository and the branch, the reachable domains, the budget and the aggregate bound, the duration, the resources, the variables | The boundary and the price: fixed before startup, never renegotiated during |
| The send | The model and the prompt | What changes at every turn: one agent writing the spec, another implementing, another reviewing, without rebuilding the VM |

The model therefore leaves the run contract: it is chosen per turn, not at
creation. What is declared stays whole, it is the moment of the declaration
that differs.

**The repository, for its part, declares nothing.** It is the payload of the
run, not its description. It is its content that we protect ourselves from
(1.5), and an MR that amended the file deciding its own budget, its domains
and its image would declare itself free. The constraint is structural too:
cove clones nothing on the host, it gives the URL of the repository to the VM,
so at the instant the contract must be known the repository exists nowhere it
could be read. It is the same rule as for `devcontainer.json`: a file of the
repository may describe what the agent launches inside the VM, never the VM.

Nothing stops the caller from keeping its values per repository in a versioned
file, but that is its own business and its own responsibility, not an input
that cove goes looking for.

### 2.7 What a run returns

A run always returns something, even when killed, and what it returns serves
two purposes: to **audit** what ran, to **improve** the next run.

To audit:

- an exit code distinguishing success, agent failure, budget reached, duration
  exceeded, admission refusal, cove failure;
- the branch pushed or the diff produced;
- the **resolved context**: image digest, agent version, model actually
  served, digests of the instruction files, list of tools, starting commit.
  One must be able to answer, after the fact, "what exactly was running".

To improve:

- the **cost**: tokens per category and price, duration, resources consumed;
- the **activity**: calls per tool, failures per tool, breakdown of the context
  window in percentages;
- the **blockages**: domains requested and refused, tools failing repeatedly,
  dead ends.

And an interface contract, because the caller is a program:

- the trace in JSON on standard output, the logs on standard error, nothing
  else to untangle;
- events as they happen, to report during the run and not only after;
- stable and documented exit codes, never an interactive prompt, never a
  required TTY;
- a run identifier supplied by the caller, so that a replayed webhook does not
  create two VMs for the same MR.

That is the product for usage B. Without it, a runner is worth nothing.

### 2.8 What programmatic usage imposes

**Destruction does not hang on the calling process.** On the `container`
backend, a `kill -9` of the CLI leaves the VM running, `--rm` included, and a
signal to the CLI does not touch it any further. A dev notices and runs
`cove stop`; a program does not, and the orphans eat the capacity until they
kill parallelism. The VM must therefore carry its own lease: a deadline beyond
which it is destroyed without anyone having to ask.

**An admission control.** Ten simultaneous calls on a capacity that holds
three: better an immediate refusal than a memory failure after ten minutes.
The refusal is an exit code, not a queue.

**An aggregate budget on top of the budget per run.** The cap per run does not
bound two hundred runs in a day. A bound per repository and per period is
needed, and a rule for MRs updated in bursts: cancel the run in progress or
stack it.

**The proxy is a daemon, cove stays a CLI.** Each invocation cannot start its
own proxy: the credentials would be within reach of a process the caller
spawns, at every call. The proxy is therefore a durable service, under its own
dedicated user, deployed where the sandboxes are; cove is its client. That
does not make cove a server.

## 3. What we take from elsewhere

The survey of existing solutions
([sandboxes-ecosystem.md](sandboxes-ecosystem.md)) is not there to compare
ourselves: it is there to check that the principles of section 1 are not local
inventions, and to spot what is worth taking.

**Validated by convergence.** Independent projects, without coordinating,
arrived at the same primitives:

| Principle | What confirms it |
|---|---|
| Boundary at the VMM, not at the shared kernel | Matchlock, k7, Chamber, agent-sandbox: started from the container, moved up to the VM |
| Egress filtered on a declared list | yoloAI (`none`, `allowlist`, `open` per sandbox), k7 (by FQDN), srt, Nono, OpenShell; nobody kept the provider preset as a sufficient answer |
| No credential in the VM | `sbx`, Matchlock, cleanroom, fletch, Docker MCP Gateway, Warp, Cursor, Codex cloud: different mechanics, same rule |
| Trace verifiable after the fact | cleanroom (provenance attached to the snapshot), container-use (state in git notes), punkgo-jack (signed log) |

**Little addressed elsewhere**: the declared context and zero leftovers,
whereas persistence is the almost universal default; the budget per run with a
hard stop; the feedback loop of family E, which no surveyed solution returns;
and the same contract from the workstation to the cloud.

**Good ideas to borrow**, with their reason:

| Borrowing | Why |
|---|---|
| Validation of IPs after resolution (srt) | A list by domain alone lets rebinding through, and at a cloud provider that leads to the instance credentials |
| SOCKS5 alongside the HTTP proxy, same policy (srt) | The proxy places headers and therefore covers HTTPS only; the rest goes out with no policy |
| A refusal legible to the agent rather than a silent timeout (littlebox) | A silent block makes the agent improvise; an explicit refusal feeds family E |
| Credential bounded by the operation (fletch, Nono, OpenShell) | Not only by destination and duration: which refs, which methods, which paths |
| A run in two stages (Codex cloud) | Network and credentials during bootstrap, cut off before the agent phase |
| A learning mode to generate the declaration (Greywall) | The domain list is written from an observed run instead of being guessed, and nobody has to click |
| Backend contract as a binary plus a manifest (DevPod) | The concrete form of VMM agnosticism |
| Copy on write clone from a frozen seed (Chamber) | Zero leftovers without paying a rebuild per run |

Out of scope, with no intention of coming back to it: native Windows, kits,
MCP gateway, interactive comfort, home grown VMM.

## Sources

- [Docker Sandboxes, documentation](https://docs.docker.com/ai/sandboxes/)
- [Docker Sandboxes, isolation](https://docs.docker.com/ai/sandboxes/security/isolation/)
- [Docker Sandboxes, local policy](https://docs.docker.com/ai/sandboxes/security/policy/)
- [Docker Sandboxes, release notes](https://docs.docker.com/ai/sandboxes/release-notes/)
- [docker/sbx-releases, proprietary license](https://github.com/docker/sbx-releases)
- [docker/sbx-kits-contrib](https://github.com/docker/sbx-kits-contrib)
- [Apple container, technical overview](https://github.com/apple/container/blob/main/docs/technical-overview.md)
- [Kata Containers, Docker in Kata](https://kata-containers.github.io/kata-containers/how-to/how-to-run-docker-with-kata/)
- [Fly.io, Network Policies](https://fly.io/docs/machines/guides-examples/network-policies/)
- [List of agent sandboxes, May 2026](https://gist.github.com/wincent/2752d8d97727577050c043e4ff9e386e)
- [Running AI agents safely in a microVM using docker sandbox](https://andrewlock.net/running-ai-agents-safely-in-a-microvm-using-docker-sandbox/)
