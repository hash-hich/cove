# Guest kernel

The kernel cove boots its VMs with by default: an unpatched longterm Linux
from kernel.org, configured from `allnoconfig` by the fragments of
[config/](config/) and nothing else, built in, without modules. Every option
on in this kernel is on because a fragment names it, so a new kernel version
turns nothing on behind cove's back. `EXPERT` is on so that the rule holds for
the options that otherwise keep their default whatever `allnoconfig` says.
The other side of that rule: an option the kernel turns on by default is off
here until a fragment names it, errata and CPU features included, which is why
the architecture fragments list them.

## Build

```bash
docker build --platform linux/arm64 --output type=local,dest=bin/kernel/arm64 kernel
docker build --platform linux/amd64 --output type=local,dest=bin/kernel/amd64 kernel
```

Each build writes two files: `kernel`, the raw `Image` on arm64 and the ELF
`vmlinux` on x86_64, stripped of its DWARF but not of its BTF, and `config`,
the final configuration. The build runs on the platform of the host and
cross-compiles, so either architecture builds on either host. It takes about
three minutes on a 12 core arm64 workstation, most of it the debug
information BTF is generated from.

The build fails when a line of a fragment does not hold in the final
configuration: `merge_config.sh` only warns when a dependency refuses an
option, and a kernel that silently lost one is not the kernel described here.

The same sources, fragments and builder give the same bytes: the builder is
pinned by digest, its packages come from snapshot.debian.org at a fixed date,
and the date, user and host the kernel records are fixed. A bump of the
compiler is a bump of `DEBIAN_SNAPSHOT`.

The initramfs the kernel is booted with is an uncompressed cpio: no
decompressor is built in.

## Fragments

| Fragment | What it holds |
|----------|---------------|
| `cove.config` | What cove requires of any kernel, its own or the user's: the disks (virtio-blk, EROFS with its attributes, overlayfs, ext4), vsock, virtio-net, a transport, a console, the initramfs, `/proc`, `/sys`, devtmpfs and the embedded configuration |
| `base.config` | A Linux userland in a virtual machine: system calls, TTY and serial console, tmpfs, IPv4 and IPv6 with multicast, the diag interfaces `ss` reads, the KVM PTP clock, transparent huge pages on request, and the defaults of the kernel that userland counts on |
| `agent.config` | The sandboxes of the agent itself: namespaces and seccomp for bubblewrap (Codex CLI, srt) and Chromium, Landlock |
| `containers.config` | A container engine run by the agent: the time namespace, cgroup v2 with its controllers and I/O throttling, seccomp, veth and bridge, netfilter for the three firewall paths of dockerd (iptables-nft, native nftables, iptables-legacy) |
| `dev.config` | The tools of a development workstation: Kubernetes in containers with its network policies, firewall scripts, the other network drivers of Docker, traffic control, loop devices with FAT and ISO images, FUSE, binfmt_misc, TUN, autofs, task accounting and pressure, CRIU, perf |
| `trace.config` | eBPF tracing: BTF, ftrace, kprobes, uprobes, the BPF JIT, and debugfs with the old tracing path |
| `arm64.config`, `x86_64.config` | The machine each backend gives, read in its device tree or its code, the errata and CPU features of the architecture, the clock, and on x86_64 the 32 bit binaries |

`cove.config` is where this kernel meets the closed list of
[internal/kernelcheck](../internal/kernelcheck/kernelcheck.go), which the
guest checks at boot; a test holds the fragment to the same check. A kernel
that cannot run the init at all, without ELF, futexes, epoll or eventfd, is
refused by the host from what the console shows (`kernelcheck.FromConsole`). The other
fragments are cove's choice for its own kernel and are checked by no one: what
the kernel compiles is permitted, and the tool the image brings says itself
what it misses.

## What was measured

Each fragment past `cove.config` was measured by booting the arm64 kernel
with `dockerd` 29 from `docker:29-dind`, and by booting it without the lines
under test to see them fail.

- `containers.config`: `hello-world`, egress from the default bridge, a
  published port and an IPv6 user network, under each of the three firewall
  paths; pause and unpause; the device filter of runc refusing `/dev/mem`;
  `io.max`, `cpu.max`, `memory.max` and `pids.max` set by `docker run`.
- `dev.config`: `kind create cluster`, then a service spread over two pods and
  a service without endpoints, rejected. Without the Kubernetes lines the
  cluster reports ready and every service, DNS included, is dead, the only
  trace being `Extension comment revision 0 not supported` in the log of
  kube-proxy. An amd64 image through qemu-user, a loop device, `/dev/fuse`, a
  TUN device, `ss`.
- `agent.config`: `bwrap --unshare-all` as an unprivileged user, `codex
  sandbox` running a command and refusing a write outside its root, a
  Landlock ruleset (ABI 7).
- `dev.config`, beyond Kubernetes: kube-proxy's nftables draw (`numgen`),
  `nft queue` and `ipset` for network policies, `-m owner`, `REDIRECT` in
  iptables and nft, dummy, VLAN, macvlan and ipvlan links, `tc` with netem and
  htb, FAT and ISO images mounted from a loop device, autofs, `/proc/PID/io`,
  `/proc/pressure`, `criu check`, `utf8=1` on FAT. Redis starts without its
  warning on huge pages, since they are given on request only.
- `trace.config`: bpftrace on `BEGIN`, a syscall tracepoint, a kprobe, an
  fentry probe and a uprobe; `perf stat` and `perf record`;
  `/sys/kernel/debug/tracing`.

x86_64 is built from the same fragments and has not been booted: the 32 bit
binaries and the shadow stack of userland are compiled, not measured.

## Limits

`perf` counts software events only: `cycles`, `instructions` and every
hardware counter are refused, and a sampled profile uses `cpu-clock`. `rr`,
which records on hardware counters, does not run at all, and neither does any
tool that reads the counters of the core. The guests measured here were given
no performance monitoring unit by their hypervisor, and on arm64 this
configuration does not build the driver of one (`ARM_PMUV3`) either: the day a
backend exposes one, both have to move.

On arm64 the kernel carries SME. The first releases of macOS 15 on M4 exposed
SME to guests without supporting it, and a kernel with SME crashed at boot;
both sides fixed it since. It has not been tested here, on an M3 without SME:
it wants a boot on an M4 under the oldest macOS cove supports, and
`arm64.nosme` on the command line is the way out if it fails.

## Bump the kernel

1. Set `KERNEL_VERSION` in the Dockerfile to the new longterm release, and
   `KERNEL_SHA256` to the checksum kernel.org publishes for its tarball in
   `https://cdn.kernel.org/pub/linux/kernel/v6.x/sha256sums.asc`.
2. Build, and read what moved under the fragments:
   `scripts/diffconfig old/config new/config` from a kernel tree. A new
   erratum or CPU feature the kernel turns on by default belongs in the
   architecture fragment.
3. Boot it with a container engine inside before anything else.

## Identity

The identity of a kernel is the sha256 of the `kernel` file the host hands the
virtual machine monitor, taken on the host. Nothing the guest reports about
its own kernel is used for that: the agent is root in the guest.
