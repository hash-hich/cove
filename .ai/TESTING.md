# Testing

- **Testing**: Use testify's `require` package, parallel tests with `t.Parallel()`, `t.Setenv()` to set environment variables. Always use `t.TempDir()` when in need of a temporary directory. This directory does not need to be removed.

Make sure added tests **actually test the code you have written** and test the intention behind the change. If you are fixing a problem, write the tests first to reproduce the problem before starting on the fix.

Before fixing a bug, add or find a failing test. Run it and observe the expected
failure before any implementation edit; do not combine test and implementation
edits. A test is not observed until its command exits.

After implementing a bug fix, confirm that the same test passes.

**Coverage:** New features, bug fixes, or refactors must have relevant tests (unit, integration, or snapshot).

When adding tests for new behavior, read existing tests first to
understand what is covered. Add new cases for uncovered behavior. Edit
existing tests as needed, but do not change what they verify.

The conventions above are enforced by `.golangci.yml`: `testifylint`
(require), `paralleltest` and `tparallel` (`t.Parallel()`), `usetesting`
(`t.TempDir()`, `t.Setenv()`, `t.Context()`).
