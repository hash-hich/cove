# Contributing

## What the history promises

The history of `main` is linear. Every commit on it holds the following.

- **It compiles, its tests pass, it is formatted and lint-clean.**
  `git bisect` can stop on any commit. `git checkout` of any commit
  gives a working tree.
- **It is one decision.** A commit changes one thing for one reason.
  `git blame` on a line lands on a small diff whose message explains
  that line, not on a bundle where the reason must be guessed.
- **Refactors precede the change that needs them.** A `refactor:`
  commit preserves behavior. When blame lands on one, the reader knows
  the move, rename or extraction did not alter what the code does; the
  behavior change is in the `feat:` or `fix:` that follows.
- **The message says why.** The diff says what changed; the body says
  the motivation, the constraint, the measurement or the alternative
  that was rejected. Nothing in the body paraphrases the diff.

## How to shape commits

Fine-grained commits on the branch, fine-grained commits on `main`. The
branch is rewritten until every commit satisfies the promises above,
then it is merged as it is.

- **Every commit must compile and pass all tests.** No "WIP" commits, no
  commits that leave the tree broken and rely on a follow-up to fix it.
- **Every commit must be `gofumpt`-formatted and lint-clean.** Run
  `golangci-lint fmt` then `golangci-lint run`.
- **Separate preparatory refactorings from behavior changes.** If a fix
  or feature is easier to review after a refactor, land the refactor in
  its own commit first. This applies even when the refactor only becomes
  apparent while writing the behavior change, such as extracting a
  helper to avoid duplication. Before committing, review the diff and
  split out any hunk that is behavior-preserving (an extraction, a
  rename, a move) into a preceding commit, by staging hunks or
  resetting and recommitting in order. This applies to any change you
  hand over, committed or not: deliver the refactor and the behavior
  change as two separate diffs.
- **Fix the history, do not append to it.** Review feedback is folded
  into the commit it concerns with `git commit --fixup` and
  `git rebase --autosquash`, never left as a "fix review" commit. A
  reader of `main` must never see a commit that only exists because a
  previous one was wrong on the branch.
- **Commit messages explain why, not what.** If the reason is obvious
  from a one-line subject, no body is needed. A body that could be
  regenerated from the diff is not a body.

A body worth writing, from this repository:

```
feat(sandbox): stop sandboxes through the container cli

The argv carries the targets by ID and the two options docker, podman
and container share, -s and -t; the default delay stays the one of
container. It never carries --all, which would reach the VMs of the
store that are not cove's.
```

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

* One issue = one branch + one merge request. The branch you are working
  on is already in a merge request; push commits to it.
* Branch names follow the GitLab default `<issue-iid>-<issue-title-slug>`,
  e.g. `42-add-dry-run-flag`. The branch is deleted after merge and its
  name does not survive in git.
* Never touch the merge request. Opening it, editing its title or
  description, marking it ready and merging it are human steps. The
  agent only pushes commits to the branch.
