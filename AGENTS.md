# cove - agent instructions

Go based command-line tool that runs a coding agent unsupervised in a micro-VM.

## Task-specific guidance

Load only the guidance relevant to the task:

| Scope                       | Guidance                                      |
|-----------------------------|-----------------------------------------------|
| Go / Golang guidelines      | [GO.md](.ai/GO.md)                            |
| Tests                       | [TESTING.md](.ai/TESTING.md)                  |
| The need and the target     | [need-and-target.md](docs/need-and-target.md) |
| What was decided, and why   | [decisions.md](docs/decisions.md)             |
| Commits, branches and MRs   | [CONTRIBUTING.md](CONTRIBUTING.md)            |

## Commands

The Makefile holds every command; `make` lists its targets.

```bash
go run ./cmd/cove <args>                # run the CLI locally (no "--": it would be passed to cove and end flag parsing)
make cove                               # build the binary
make fmt                                # format (gofumpt + gci), both modules
make check                              # lint for the host and Linux, then test, both modules (CI gate)
go test ./internal/<pkg> -run TestName  # single test
```

Definition of done: `make fmt` leaves no diff, `make check` green, and the touched command manually exercised via `go run`.

## Project layout
<!-- Intended layout, no code yet: -->

* `cmd/cove/`: the CLI entry point, wiring only.
* `internal/<pkg>/`: one package per job, named after that job and not after a thing, not importable from outside the module.

## Rules

* When asked a question, answer the question instead of jumping to implementation.
* Keep changes focused and reviewable.
* Name code for what it does, not its implementation or history.
* Follow existing patterns in the file you are editing.
* Do not use em dashes, en dashes, or spaced double hyphens as punctuation in code, comments, strings, or documentation.

## About this file

* Avoid modifying this file unless necessary.
* AGENTS.md is an index, not an encyclopedia. Put details in the task-specific guidance files.
* Enforced rules beat written instructions. Prefer a lint rule over a sentence whenever possible.
* Keep this file under 200 lines.
