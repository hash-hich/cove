# vminit

The init of cove, `cove-init`: PID 1 in the VM, and what the host needs to
hand it its run. The packages are split by where they run.

| Package | Runs on | What it holds |
|---|---|---|
| [spec](spec/) | host and VM | `run.json`, the description of a run the host writes and the init reads |
| [initramfs](initramfs/) | host | the archive the kernel boots the init from, with `run.json` |
| [boot](boot/) | VM | the init itself, from the first mount to the power off |
| [launch](launch/) | VM | the processes of the image, in the mount namespace of the agent |
| [imageuser](imageuser/) | VM | the `User` of the image, resolved as runc does |

The host never links what runs in the VM: the `vminit` rule of depguard, in
[.golangci.yml](../../.golangci.yml), refuses their import anywhere but in
those packages and in `cmd/cove-init`.

The init is a binary of its own but not a module of its own. Cove puts it in
the initramfs of every run, so the two are built from one commit, and `spec`,
the contract between them, is read in one version on both sides.
