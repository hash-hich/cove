# Bring your own image

Cove runs the agent in an image you choose: any OCI image built with an
ordinary Dockerfile, as long as it is a whole Linux system. The image becomes
the root of the VM. Cove adds its init outside of it and nothing inside, so the
image has to hold everything the agent needs, the agent included.

This page says what cove does with each instruction of your Dockerfile, what
it provides in place of docker, and where it differs from `docker run`. The
two images in this directory are examples:
[sandbox](sandbox/README.md) is the base, [go](go/README.md) a profile built on
it.

## What the image must be

- **Linux, for the architecture of the host.** An arm64 image on Apple
  Silicon, amd64 on x86_64. Nothing is emulated, and `cove pull` refuses an
  image with no manifest for the host.
- **A whole system.** A `scratch` image, or one with nothing but a static
  binary, has no shell, no `/etc/passwd` and nothing for the agent to work
  with.
- **The agent in the `PATH`.** It is looked up in the `PATH` of the image when
  the VM starts, and a VM whose image lacks it stops before the first turn:
  `claude not found in PATH (/usr/local/sbin:/usr/local/bin:...)`.
- **Its engine, if the agent runs containers.** Cove ships no Docker. An image
  whose agent runs `docker` holds `dockerd`, `containerd` and `runc`, and
  declares `VOLUME /var/lib/docker` (see [Volumes](#volumes)). `docker:dind`
  already does both.
- **Not too many layers.** Each layer is one disk of the VM, and a VM takes
  about 115 disks on arm64 and about 10 on x86_64. An image with more layers
  is refused, naming both numbers.

## Instruction by instruction

Cove never reads your Dockerfile. It reads the image configuration the build
wrote from it, and each instruction that matters at run time has a field
there.

| Instruction | What cove does |
|---|---|
| `FROM` | Its layers become the root of the VM. |
| `RUN`, `COPY`, `ADD` | Their result is in the layers; that is all cove sees of them. |
| `USER` | The agent runs as this user. See [User](#user). |
| `ENV` | The agent gets this environment. See [Environment](#environment). |
| `WORKDIR` | The parent of the project directory. See [Where the code lands](#where-the-code-lands). |
| `VOLUME` | Each path gets a real file system of its own, thrown away with the run. See [Volumes](#volumes). |
| `STOPSIGNAL` | Sent to the agent when cove stops it, `SIGTERM` by default. It gets ten seconds, then `SIGKILL`. |
| `ENTRYPOINT`, `CMD` | Never run. Cove starts the agent itself at each turn, so a script that prepares the environment in an entrypoint does not run: the image must be ready without it. |
| `EXPOSE` | No effect: nothing enters the VM. |
| `HEALTHCHECK` | Ignored: the agent is not a service. |
| `LABEL`, `SHELL`, `ONBUILD`, `ARG` | No effect at run time. |

### User

`USER` takes the six forms docker takes: `user`, `uid`, `user:group`,
`uid:gid`, `uid:group` and `user:gid`. Names are looked up in the
`/etc/passwd` and `/etc/group` of the image, never in those of the host.

- Without `USER`, the agent is root.
- A name the image does not list stops the VM before the first turn:
  `image sets User node, not found in the image's /etc/passwd`.
- A numeric uid the image does not list is accepted, with gid 0 and `HOME=/`.
- Without a group, the agent also gets every group of `/etc/group` that lists
  the user. With a group, it gets that one alone.

These are the rules of runc, the runtime images are tested against, and they
are implemented in [internal/vminit/imageuser](../internal/vminit/imageuser/imageuser.go).

### Environment

`ENV` is taken as it is: no expansion, a `$` in a value stays a `$`, and the
last value of a name wins. Underneath it, cove sets what docker sets:
`PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`,
`HOSTNAME`, and `TERM=xterm` when the agent has a terminal. `HOME` comes from
`/etc/passwd` unless the image sets it.

### Where the code lands

The project directory is `<WORKDIR>/<repository>`, `/work/<repository>` when
the image sets no `WORKDIR` or sets `/`. The agent starts there.

- The working directory is created if missing and left alone if present, so a
  `WORKDIR /home/agent` keeps its dotfiles next to the project.
- The project directory is created and given to the user of the image. An image
  that already holds it is refused: `/work/front already exists in the image,
  cove clones the project there`.
- A configuration of the image that names the path of the code must name the
  project directory, not the working directory.

### Volumes

Each `VOLUME` path gets a directory of its own on the write disk of the run, an
ext4 file system, filled with what the image holds at that path, as docker
fills an anonymous volume. It is not shared with the host and does not
survive the run.

This is what a container engine needs: its data directory must be a real file
system, since overlayfs cannot stack its layers on the overlay that holds the
root of the image. An image whose agent runs `dockerd` declares
`VOLUME /var/lib/docker`, one that runs podman `VOLUME /var/lib/containers`.
Without it, the engine fails in the middle of the run.

## What cove does in place of docker

Docker, or the Linux under it, did these without a field of the configuration.
In the VM, the init of cove does them before the first turn.

- **Mounts:** `/proc`, `/sys`, `/sys/fs/cgroup` as cgroup2, `/dev` as the whole
  devtmpfs rather than the few nodes docker gives, `/dev/pts` with its own
  `/dev/ptmx`, `/dev/shm` and `/dev/mqueue`.
- **Name and resolvers:** the hostname is set, and `/etc/hostname`,
  `/etc/hosts` and `/etc/resolv.conf` are written as plain files, which the
  agent may edit. A link in their place is replaced.
- **Network:** the loopback is up.
- **Limits:** a hard limit of 1048576 open files, where a VM without systemd
  starts at 4096, and a umask of 022.
- **PID 1:** the init reaps the orphans and stays PID 1. The agent never is.

Nothing is taken away: no capability dropped, no seccomp filter, no masked
path, no cgroup limit. The agent has the rights of root on a machine. The VM
is the boundary, not the image.

## Differences with `docker run`

For an author who tested the image with docker:

- No entrypoint ran before the agent.
- The code is in `<WORKDIR>/<repository>`, not in `WORKDIR` itself.
- `/dev` is whole, the network files are writable, and there is no
  `/.dockerenv`.
- A volume is a throwaway disk, never a mount of the host. Cove has no `-v`.
- Only the domains the run declares are reachable.

## Out of scope

Windows images, another architecture through emulation, a mount from the host,
a port exposed to the host, dependencies preinstalled in the project directory
(the working directory itself stays as the image holds it), reading
`devcontainer.json`, and a cache that outlives the run.
