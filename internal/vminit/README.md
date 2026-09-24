# vminit

The init of cove, `cove-init`: PID 1 in the VM, and what the host needs to
hand it its run. The packages are split by where they run.

| Package | Runs on | What it holds |
|---|---|---|
| [spec](spec/) | host and VM | `run.json`, the description of a run the host writes and the init reads |

The init is a binary of its own but not a module of its own. Cove puts it in
the initramfs of every run, so the two are built from one commit, and `spec`,
the contract between them, is read in one version on both sides.
