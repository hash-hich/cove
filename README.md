# Cove

**Let a coding agent work unsupervised. Nothing to break, no mess left behind.**

Cove gives it a throwaway micro-VM of its own: it can wreck the whole box, and
when the run ends the box is gone. You get a branch and a report.

```bash
cove run --name fix-login https://gitlab.com/you/repo.git
cove send fix-login "find why the login test is flaky and fix it"
```

## Why cove

**Avoid permission fatigue.** Inside the VM the agent asks you nothing. It
installs, deletes, reformats, recreates the test database, runs Docker. None of
it is yours, and none of it survives the run.

**There is nothing inside worth stealing.** No SSH key, no kubeconfig, no API
key, no cookie. The agent never holds a credential, so it cannot leak one.

**You control exactly what goes out.** A run reaches only the domains it
declared, and every other destination is refused. Nothing leaves quietly: what
was asked for and refused comes back to you in the report.

**You control what shapes the run.** An agent's behavior comes from its
context, and most of that context is usually inherited rather than chosen.
Here nothing enters unless you put it there: the image, the agent version, the
model, the instruction files, the tools. Nothing carries over from the last run
or from your workstation.

**Run ten at once.** Each run gets its own machine, its own ports, its own
state. Ten merge requests in parallel never step on each other, and your laptop
stays usable.

**Every run makes the next one better.** You get what it cost, which tools it
used and which kept failing, which domains was refused, where it got stuck,
and exactly which image, agent version and model ran. A tool failing nine times
out of ten is a line of your declaration to fix, not an agent to blame. You
amend it, the next run goes further.

## Who it is for

- You run agents on your own machine and you would rather stop watching them.
- You want an agent to take a merge request end to end, with nobody in front.
- You have to convince someone that an unsupervised agent is safe to run.

## Try it

Requires macOS 26 on Apple Silicon, with Apple `container` installed and its
system service started.

```bash
container build --platform linux/arm64 -t cove-sandbox:local images/sandbox
go build -o bin/cove ./cmd/cove

bin/cove run --name demo https://gitlab.com/you/repo.git  # a fresh micro-VM with the repository
bin/cove send demo                                        # a terminal on the agent
bin/cove send demo "fix the ci"                           # one turn, its JSON on stdout
bin/cove list                                             # what is running
bin/cove stop demo                                        # or: bin/cove stop --all
bin/cove pull ghcr.io/you/image:tag                       # an image into ~/.cache/cove/images, by digest on stdout
```

Go, with one dependency, go-containerregistry, justified in the decision log.
Any agent you can install in the image runs here.

## Read more

- [The need and the target](docs/need-and-target.md): the problem, and what
  cove is built to be.
- [Decisions](docs/decisions.md): what was chosen, why, and what was turned
  down.
