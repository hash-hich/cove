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

```bash
go run ./cmd/cove <args>                # run the CLI locally (no "--": it would be passed to cove and end flag parsing)
go build -o bin/cove ./cmd/cove         # build the binary
go test ./...                           # all tests
go test ./internal/<pkg> -run TestName  # single test
golangci-lint fmt                       # format (gofumpt + gci)
golangci-lint run                       # lint (CI gate, also reports formatting drift)
container build --platform linux/arm64 -t cove-sandbox:local images/sandbox   # build the sandbox image
container build --platform linux/arm64 -t cove-go:local images/go             # build the Go profile, on the sandbox image
```

Definition of done: `golangci-lint fmt` leaves no diff, `golangci-lint run && go test ./...` green, and the touched command manually exercised via `go run`.

## Project layout
<!-- Intended layout, no code yet: -->

* `cmd/cove/`: the CLI entry point, wiring only.
* `internal/<pkg>/`: one package per concern, not importable from outside the module.

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
