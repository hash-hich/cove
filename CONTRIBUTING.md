# Contributing

## How to structure commits

Prefer a fine-grained commit history. Commits should be as small as possible
while still being meaningful and self-contained. Fine-grained commits on the branch, squashed at merge.

- **Every commit must compile and pass all tests.** No "WIP" commits, no
  commits that leave the tree broken and rely on a follow-up to fix it.
- **Every commit must be `gofumpt`-formatted.** Run `golangci-lint fmt`; it applies gofumpt and gci.
- **Every commit must be lint-clean.** Run `golangci-lint run`.
- **Commit messages explain _why_, not _what_.** The diff already shows what
  changed; the message should capture the motivation, the constraint, or the
  bug being fixed. If the reason is obvious from a one-line subject, no body
  is needed - but never paraphrase the diff.
- **Separate preparatory refactorings from behavior changes.** If a fix or
  feature is easier to review after a refactor, land the refactor in its own
  commit first. Pure refactors should be behavior-preserving; the commit that
  changes behavior should be as small as possible. This applies even when the
  refactor only becomes apparent _while_ writing the behavior change - e.g. you
  extract a helper to avoid duplication. Don't let "I discovered it mid-change"
  excuse bundling it in. Before committing, review your diff and split out any
  hunk that is behavior-preserving (an extraction, a rename, a move) into a
  preceding commit, by staging hunks or resetting and recommitting in order.
  This applies to any change you hand over, committed or not. Deliver the refactor and the behavior change as two separate diffs.
  Reordering, reformatting and renaming go in their own commit, before the change that needs them.

## Commit message format

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):

```
<type>(<scope>): <subject>

<body>
```

- **type** is one of `feat`, `fix`, `refactor`, `test`, `docs`, `build`, `ci`, `chore`.
  `perf` and `style` are not used: a performance change is a `fix` or a `feat`,
  and formatting never lands alone since every commit is already formatted.
- **subject** is imperative, lowercase, without a trailing period, and fits in 72
  characters.
- **body** is optional and explains the why, as described above. Wrap at 72 characters.
- **breaking changes** are marked with `!` after the type or scope (`feat(cli)!:`)
  when the CLI flags, config file or exit codes change incompatibly. Say what
  breaks and how to migrate in the body. Do not use the `BREAKING CHANGE:` footer.
- A preparatory refactor is `refactor:`, never folded into the `feat:` or `fix:`
  that needs it (see above).

## Workflow

* One issue = one branch + one MR. The branch you are working on is already in a Merge Request; push commits to it.
* Branch names follow the GitLab default `<issue-iid>-<issue-title-slug>`, e.g. `42-add-dry-run-flag`.
* Do not create MRs. Opening the MR is a human step.
* Fine-grained commits on the branch, squashed at merge.