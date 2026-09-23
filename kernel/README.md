# Guest kernel

The kernel cove boots its VMs with by default: an unpatched longterm Linux
from kernel.org, configured from `allnoconfig` by the fragments of
[config/](config/) and nothing else, built in, without modules. Every option
on in this kernel is on because a fragment names it, so a new kernel version
turns nothing on behind cove's back.

## Build

```bash
docker build --platform linux/arm64 --output type=local,dest=bin/kernel/arm64 kernel
docker build --platform linux/amd64 --output type=local,dest=bin/kernel/amd64 kernel
```

Each build writes two files: `kernel`, the raw `Image` on arm64 and the ELF
`vmlinux` on x86_64, and `config`, the final configuration. The build runs on
the platform of the host and cross-compiles, so either architecture builds on
either host. It takes about a minute on a 12 core arm64 workstation.

The build fails when a line of a fragment does not hold in the final
configuration: `merge_config.sh` only warns when a dependency refuses an
option, and a kernel that silently lost one is not the kernel described here.

The same sources, fragments and builder give the same bytes: the builder is
pinned by digest, its packages come from snapshot.debian.org at a fixed date,
and the date, user and host the kernel records are fixed. A bump of the
compiler is a bump of `DEBIAN_SNAPSHOT`.

## Fragments

| Fragment | What it holds |
|----------|---------------|
| `base.config` | A Linux userland in a virtual machine: syscalls, TTY, `/proc` and `/sys`, tmpfs, ext4, IPv4 and IPv6, virtio over mmio (libkrun, Firecracker) and PCI (Virtualization.framework), a clock read from the RTC at boot |
| `cove.config` | What cove requires of any kernel, its own or the user's: virtio-blk, EROFS with its attributes, overlayfs, vsock, virtio-net, the initramfs and the embedded configuration |
| `containers.config` | What a container engine run by the agent needs: namespaces, cgroup v2, seccomp, veth and bridge, netfilter for iptables-nft |
| `arm64.config`, `x86_64.config` | The virtual machine of each architecture |

`cove.config` is where this kernel meets the closed list of
[internal/kernelcheck](../internal/kernelcheck/kernelcheck.go), which the
guest checks at boot; a test keeps the two in step. The other fragments are
cove's choice for its own kernel and are checked by no one: what the kernel
compiles is permitted, and the engine the image brings says itself what it
misses.

`containers.config` was measured by booting the kernel with `dockerd` 29
from `docker:29-dind`: `hello-world`, egress from the default bridge, a
published port and a user-defined network all pass. The tables of
iptables-legacy are left out, as the kernel deprecates them.

## Bump the kernel

1. Set `KERNEL_VERSION` in the Dockerfile to the new longterm release, and
   `KERNEL_SHA256` to the checksum kernel.org publishes for its tarball in
   `https://cdn.kernel.org/pub/linux/kernel/v6.x/sha256sums.asc`.
2. Build, and read what moved under the fragments:
   `scripts/diffconfig old/config new/config` from a kernel tree.
3. Boot it with a container engine inside before anything else.

## Identity

The identity of a kernel is the sha256 of the `kernel` file the host hands the
virtual machine monitor, taken on the host. Nothing the guest reports about
its own kernel is used for that: the agent is root in the guest.
