<!-- rename the H1 to your repository name -->
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

## Design notes

The scoping documents — threat model, adversary model, surfaces, requirements —
live alongside the code. *(Written in French for now.)*

## License

Apache-2.0.
