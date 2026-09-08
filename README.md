# Unsupervised coding-agent sandbox with empty-room isolation.

Run a coding agent on a repository **without approving each step**,
then get the result back as a merge request to review — while treating the agent
as **hostile and competent** the whole time.

The premise: an agent that runs arbitrary commands while reading untrusted text
(a README, a package's docs, an error message) is untrusted code execution. So
the question isn't "will the agent misbehave?" but "what happens when hostile
code runs with the agent's privileges?"

## The paradigm

**Empty the room instead of locking the door.** Rather than trying to stop
exfiltration — a losing game once any outbound channel exists — the sandbox is
built so there is nothing *from the host* to exfiltrate. Nice consequence:
internet access can stay open. The credentials never go in the box; they stay on
the host.

## How it works

- **A micro-VM per run** (Apple `container`). Nothing from the host is mounted —
  no `$HOME`, no SSH keys, no other projects. The guarantees hold *by absence*,
  not by a policy that has to stay exhaustive.
- **Credentials stay host-side.** A local broker holds the real token; the
  sandbox only ever receives a short-lived, per-run capability, unusable from
  anywhere else.
- **Recovery is typed and inert.** The agent's work comes back as git objects,
  inspected before a single file touches the host disk. The merge request is
  pushed *without* triggering CI — nothing reaches your machine or your pipeline
  without your decision.
- **You review the result, not the running agent.** Review is a quality gate,
  not the security boundary.

## Why it's built this way

Most agent sandboxes isolate the process and leave the credentials inside. This
one starts from an explicit threat model and turns it into requirements —
**every guarantee is written with a test that fails if it regresses**, and the
whole thing is validated by running a deliberately hostile agent against it. The
threat model and the surface catalogue are as much the project as the code.

## Status

Early. The threat model, the attack-surface catalogue and the requirements are
worked out; the transparent credential relay is validated; the wrapper is being
built in deliberate increments. Early versions **intentionally** trade some
guarantees for progress (e.g. the first cut injects the token into the sandbox),
and the roadmap tracks exactly which guarantee is held at each step. A security
project that claims more than it holds is a liability, so this README won't.

## Stack

Go · Apple `container` (micro-VM, Apple Silicon) · GitLab + Claude Code today,
forge- and model-agnostic later.

## Try it

Requires macOS 26 on Apple Silicon, with Apple `container` installed and its
system service started.

```bash
container build --platform linux/arm64 -t cove-sandbox:local images/sandbox
go build -o bin/cove ./cmd/cove
bin/cove run --name demo          # a micro-VM, kept alive until stopped
container exec -it demo claude    # until cove has its own verb to talk to the agent
container stop demo               # same
```

`cove run` creates the sandbox and nothing else; talking to the agent and
stopping the VM are separate verbs. It takes the flags of `docker run` that this
role justifies (`--name`, `--rm`, `--keep`, `--cpus`, `-m`, `-e`) and nothing
else: cove builds the argument array itself, so a mount, a user, a working
directory, a network option, the SSH agent or a command cannot even be asked
for. Exit code: 0 once the VM runs; 2 on a usage error; 125 when cove could not
launch it.

## Design notes

The scoping documents — threat model, adversary model, surfaces, requirements —
live alongside the code, split into short files under
[docs/cadrage/](docs/cadrage/README.md). *(Written in French for now.)*

