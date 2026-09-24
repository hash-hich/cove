# Decisions

What is decided now and why, for the decisions that bear on the choices still
to come. Not a history: git is the history. The file is maintained like code,
an entry amended when what it decides changes, and removed once what it holds
can serve nothing. Newest first.

An entry is a dated title and three labels, in this order and no other:

* The title states the solution, not the subject.
* **Decided.** One or two sentences. It does not restate the title. What the
  decision commits to from now on belongs here.
* **Why.** Short sentences, one fact each.
* **Rejected.** Every option weighed, each with the one fact that kills it.

No label is added, split or renamed for one entry: a template that varies is
no longer read. A rule about paths, file names or layout is not a decision and
goes to the spec or the code comment that owns it. An entry past thirty lines
is carrying something that belongs somewhere else.

## 2026-09-24: the write disk is copied from a template mkfs.ext4 made

**Decided.** Nothing formats a write disk at run time: cove writes the blocks
of an empty ext4 that a pinned `mkfs.ext4` made at the build, one template per
size from 8g to 1t, and leaves the rest of the file a hole. `--disk` asks for
the size, 64g by default, lowered to what the host has free past a margin of
4g; a full disk is an `ENOSPC` in the VM, and the run goes on. Cove alone
removes the disk, and unlinks it as soon as `cove-vmm` holds its descriptor
when the sandbox goes with its VM. `--keep` is gone.

**Why.** A template is copied, not written, so `mkfs.ext4` stays a tool of the
build and the entry of 2026-09-21 holds. Measured, an empty ext4 keeps 2.2 MiB
of blocks that are not zero at 8g and 14 MiB at 1t, 3 MiB compressed for the
eight, and two builds give the same bytes. The host pays for what a run writes,
never for the size, and the size costs the VM no memory. A file without a name
lives as long as a descriptor holds it, so a `cove-vmm` that dies, killed or
not, leaves nothing behind. Nothing on the host reads an ext4, so a kept disk
could only be booted again, which no verb does.

**Rejected.** A static `mke2fs` in the initramfs: a C binary per architecture
for a freedom of size a sparse file does not need. Formatting from the image:
the write layer is cove's. An ext4 writer in Go: the entry of 2026-09-21. A
sweep of orphans at the next command: the host fills until it runs. Killing
the run at a full disk: the agent can free space, and the host is safe already.

## 2026-09-24: the agent is started as runc starts a docker exec

**Decided.** Of the image configuration, the init applies `User` in its six
forms, resolved in the image's `/etc/passwd` and `/etc/group` by the rules of
runc, and `Env` over what docker sets before it, `HOME` taken from
`/etc/passwd` when neither sets it; `StopSignal` stops a process, `SIGKILL`
follows ten seconds later, and `Entrypoint` and `Cmd` never run. The project
lands in `<WorkingDir>/<repository>`, `/work` when `WorkingDir` is empty or
`/`; an image that already holds that directory is refused.

**Why.** Images are tested against runc, so its rules are copied rather than
improved. The agent is started at each turn, and nothing of the image runs
before it: an entrypoint that prepares the environment is what an image author
has to know about. A `WORKDIR` says where the image expects to work, and the
project goes inside it rather than on it, so the dotfiles of a home stay. A VM
without systemd starts at 4096 open files, and dockerd and node count on the
limit docker raised, so every process starts with a hard limit of 1048576 and
a umask of 022. No capability is dropped and no filter is set: the VM is the
boundary (2026-09-23).

**Rejected.** Running `Entrypoint`: the profiles already launch no command
(2026-09-13), and the agent is not a service. The project at `WorkingDir`
itself: a `WORKDIR` on a home would make the home the repository. Names
resolved in the initramfs or on the host: neither is the system the agent runs
on.

## 2026-09-24: a run writes on one ext4 disk of its own

**Decided.** The write disk of a run is one virtio-blk disk, attached after the
layers and formatted ext4. It holds the upper and the work directory of the
overlay, and a directory per `VOLUME` of the image, filled with what the image
holds at that path and bound on it. The init refuses a disk another run wrote
on.

**Why.** An overlay cannot take its upper on another overlay, so an engine the
agent runs needs a real file system under its directory of data: measured, the
`dockerd` 29 of `docker:29-dind` takes the overlayfs driver on the volume and
runs a container. A tmpfs holds in the memory of the VM whatever the agent
installs and builds, where memory is already the floor of a sandbox on macOS.
One disk for every volume keeps the disks of x86_64 for the layers. A volume is
filled by a copy, as docker fills an anonymous volume, since a stack is what
the engine cannot take.

**Rejected.** A tmpfs for the upper: memory, and no engine on it. A disk per
volume: each one takes a disk from the layers. The project off the overlay, on
the disk directly: the upper is on ext4 already, and nothing measured needs it.

## 2026-09-24: the init reads its run from a second archive of its initramfs

**Decided.** The host describes a run in `/cove/run.json`, in a cpio archive
laid after the one that holds the init; the init reads it before it mounts
anything. The disks are found in their order of attachment, the layers, then
the write disk, and a count or a size that differs from the description
refuses the run by naming the layer; a refusal is a line of the init on the
console, and the VM powers off.

**Why.** The kernel unpacks archives laid end to end into one root, so the
archive of the init is written once per version of cove and a run adds a few
hundred bytes. The order of attachment holds on Virtualization.framework,
measured through vfkit with seventeen disks. The size catches a disk missing,
added or out of place, not two layers of one size swapped. The virtio serial
of a disk would name it, and vfkit sets it, but no backend is known yet to set
it on every platform.

**Rejected.** A disk holding the description, as fly.io: one disk fewer for
the layers. The kernel command line: 2048 bytes, short of the environment of
an image and fifty layers. The vsock: the channel of the turns is not decided,
and the description is needed before it.

## 2026-09-23: the agent runs on the VM, the image is its root

**Decided.** The image is the root of the VM and the agent runs on it as a
process, root on the real root, in a mount namespace of its own under cove's
init, which is PID 1 from an initramfs and stays so. Cove ships no guest root,
no runtime and no engine: the kernel's configuration is the only policy
inside, what the kernel compiles is permitted, and a real file system is
mounted under the write layer and on every path the image declares as `VOLUME`.

**Why.** The boundary is the hypervisor, and nothing in the VM belongs to
anyone (2026-09-07). The engine comes from the image, at nerdbox too, so
neither model has to ship one. An envelope's limits are a policy, and an
agent that runs Docker forces the privileged one, where the envelope
separates nothing. With a kernel of the user, running on the VM leaves kernel
and userland to one owner; a container puts cove's runtime on a kernel it did
not choose. A container adds a root and a runtime per architecture to keep in
step with kernel and VMM, and one disk under a ceiling of about ten on x86_64.
libkrun is built for the agent on the VM.

**Rejected.** A privileged container by default, the `-docker` mode of sbx: the
same plus a runtime, a root and a disk, separating nothing. Two modes, as sbx:
a second mode to document, test and explain at every refusal. An unprivileged
container alone: it breaks the nested engine. The image root as the root of
the init: the initramfs is needed to stack the layers anyway, and the mount
namespace it keeps costs nothing. Injecting the agent into any image, as the
kits of sbx: a mounted binary must match the libc of the image, and the image
carries the agent (2026-09-13).

## 2026-09-23: cove's kernel is kernel.org Linux, configured from allnoconfig

**Decided.** The default guest kernel is an unpatched longterm Linux from
kernel.org, 6.18 today, built from `allnoconfig` with `EXPERT` by the fragments
of `kernel/config/` alone, without modules, for arm64 and x86_64, and
permissive enough for a development workstation: the agent's own sandboxes,
container engines, Kubernetes in containers, FUSE, loop, perf and eBPF with
BTF. Its identity is the sha256 of the file the host hands the VMM. A kernel
the user brings is accepted when it meets the list of `internal/kernelcheck`,
read in its file before boot, else from `/proc/config.gz`, so a kernel without
`IKCONFIG` is refused. Speculative execution mitigations are off.

**Why.** Without TSI, no patch set ties cove to a branch: the libkrunfw series
applies to 6.12 only, 19 of its 36 patches failing on 6.18.53. From
`allnoconfig` a new version turns nothing on that a fragment does not name,
and the build fails when a dependency drops a fragment line. Each fragment was
measured by a boot with `dockerd` 29 and a boot without it: without the
Kubernetes matches, kind reports ready and every service is dead; without BTF
no eBPF tool runs, since no image carries the headers of cove's kernel. Two
builds give the same sha256. Mitigations protect the guest from the agent,
and nothing in the guest is protected (the ADR of the same day).

**Rejected.** libkrunfw as a library: no initramfs, no embedded configuration,
no netfilter for `dockerd`. libkrunfw as a source: a patched 6.12 with TSI gone
to justify it. A base configuration from Apple, Firecracker or Kata: none meets
the list, and their changes would land unread. Modules: the init needs its
options before any `/lib/modules`, and the kernel stops being one file. IPVS:
not the default mode of kube-proxy.

## 2026-09-23: the guest reaches the network through virtio-net

**Decided.** Every backend gives the guest a virtio-net card wired to
`cove-proxy`. TSI is
never enabled, and `cove-vmm` holds no network socket.

**Why.** `cove-proxy` and its TCP/IP stack exist whatever libkrun offers:
Firecracker has no TSI, and a kernel of the user has it only if it carries the
libkrunfw patches, out of tree since the start. TSI would add a second path,
not remove one.

**Rejected.** TSI

## 2026-09-22: the rootfs is one blob per layer, not one disk per image

**Decided.** Each layer of an image becomes one EROFS disk of its own, and the
guest stacks them with overlayfs in the order of the manifest, the highest
first. Cove flattens nothing: the whiteouts are written as overlay markers and
read by the guest kernel. The mount carries `xino=on` and `redirect_dir=on`;
an image with more layers than the VM can attach disks is refused, naming both.

**Why.** A blob depends on one archive and nothing else, so it converts on its
own core while the other layers are still downloading, where a flattened disk
applies the layers in series after the network. Two images on the same base
convert it once, since the cache is keyed by diff id and never by image. A blob
is a function of one archive, so it is reproducible whatever the order of the
run. The ceiling is a constant of the virtual machine monitor and of the
architecture, measured on libkrun HEAD `e6cdb55`, which has only virtio-mmio:
around 115 disks on arm64, around 10 on x86_64, against 14 layers at most on
the ten real images measured. `xino=on` is what the stacking costs: two files
from two blobs can otherwise carry one `st_ino` in the merged view, since
`CONFIG_OVERLAY_FS_XINO_AUTO` is off in libkrunfw, and a tool that deduplicates
by `(st_dev, st_ino)` would read them as one file. Without `redirect_dir=on`,
renaming a directory of a blob fails with EXDEV, measured in the guest.

**Rejected.** One flattened disk per image, the shape of nerdbox and of
`mkfs.erofs --tar` on a concatenated stream: it serializes the conversion
behind the download, converts a shared base once per image, and makes cove
answer for the whiteouts at write time. A hard link from a layer to a file of a
lower one, which two file systems cannot express: measured on ten real images,
from alpine:3.21 to golang:1.25 and `devcontainers/base:debian`, around 180
hard links and none crossing a layer, build tools emitting the link in the
layer of its target. It stays a named failure.

## 2026-09-22: a disk is reproducible

**Decided.** The same layer converted twice gives the same bytes, on any host
and in any order. Everything the archive of a layer leaves undated takes
`SOURCE_DATE_EPOCH`, the default zero included; the value is read once, when
the cache opens, and refused when it is not a number of seconds. Each blob is
fingerprinted with sha256 as it is written, and the fingerprint is kept in
`meta.json` next to it.

**Why.** A disk is keyed by the diff id of its layer, below, so the cache
serves one host's disk to another run without ever comparing the two.
Reproducible is what makes that key honest: same diff id, same bytes, which is
also why an epoch other than the default changes the key. The writer is given a
build time rather than a clock, and the dates of the files come from the
archive itself. The date of a conversion is a fact about that conversion, so it
lives in `meta.json` and never inside the blob. The fingerprint is computed
once, at write, and never again: it says which conversion wrote the blob, which
recomputing it on a read could not.

**Rejected.** Dating the undated entries with the clock of the conversion: two
runs of the same layer then differ for no reason a report can name. Ignoring a
`SOURCE_DATE_EPOCH` that is not a number of seconds: the blobs come out dated
by something nothing explains. One key per layer with the epoch recorded only
in `meta.json`: a conversion running without the variable would be served a
blob dated by it.

## 2026-09-22: a disk is keyed by the diff id of its layer

**Decided.** A converted layer lives in one directory of the cache of the
rootfs, named by the diff id of that layer, shared by every image that names
it. A layer whose config carries no diff id falls back to the digest of the
compressed blob. The writer that decides the bytes is a directory above,
`rootfs/v1/`, and a `SOURCE_DATE_EPOCH` other than the default zero is a
suffix on the key.

**Why.** The diff id is the hash of the decompressed layer, so it names what
goes into the disk and nothing else. It is known before a byte is read, which
is what lets a pull skip a layer instead of reading it to find out. The same
layer compressed twice over, or served by another registry, is one diff id and
one disk. The stream is hashed on the way in and refused when it does not
match, so a key is never taken on trust. Nothing in a key belongs to an image,
so the cache is shared and never walked.

**Rejected.** The digest of the compressed blob as the key: it names one
compression of the layer, so the same content recompressed converts twice. The
fingerprint of the disk as the key: it exists only once the disk is written,
which is after the work the key is there to avoid. One directory per image:
the same layer is converted once per image that holds it.

## 2026-09-21: the disk writer is go-erofs

**Decided.** `github.com/erofs/go-erofs` writes the disks, rather than a
serializer of cove's own, vendored like the rest at
`v0.3.2-0.20260901071538-03d68d88381c`, commit `03d68d8` of 2026-09-01, and
not at v0.3.1, the latest tag. The pin moves to a tag the day one covers that
commit, and every bump is measured again against `mkfs.erofs`.

**Why.** Cove does not own the format. Writing the serializer means
superblock, inode layout, directory blocks, shared table of extended
attributes and the planning of the nids. The library judges itself against the
reference implementation, its test helper calling `mkfs.erofs --tar=f --aufs`,
the invocation of the containerd differ. It is maintained by `hsiangkao`,
author of EROFS and of `erofs-utils`, and by `dmcgowan`, maintainer of
containerd. It carries no module of its own, no cgo, Apache 2.0, `go 1.23`,
4 900 lines. It sees neither the network nor the credentials. Its output is a
file in the cache, read by the kernel of the guest and not by the host.

The pin is seven commits ahead of the tag, and cove needs each of them. An untagged commit promises
nothing: the API can move, and a rebase upstream takes the hash away.
Vendoring contains it, since the source is in the repository and the build
reads it rather than the proxy.

**Rejected.** Writing the serializer in cove, above. v0.3.1 plus a fork
carrying the seven commits: the same code, a fork to maintain, a rebase to
follow. Waiting for a v0.3.2, which is not a plan. hcsshim `ext4/tar2ext4`,
the only Go writer that does the whole job: seven indirect modules and 11 MB
of `vendor/` pulled in through a logging helper, and it takes the tar itself,
which leaves the loop of `internal/layer` nowhere to stand. `mkfs.erofs --tar`
and `mke2fs -d`: an external binary to ship and pin, a Homebrew formula on
macOS, and for `mke2fs` the intermediate directory on the host that the target
removed. `mkfs.erofs` stays the oracle of the tests.

## 2026-09-21: the rootfs disk is EROFS

**Decided.** EROFS for the read only disk a run mounts, over ext4.

**Why.** The disk is read only by contract, since every write of a run lands
in the overlay above it. ext4 is a read-write file system, so its journal,
allocation bitmaps, htree and inode tables are capacity cove never uses and
must write correctly anyway. EROFS is an image format: the tree is laid out
once at the end, with no allocator, no hash seed and no UUID to fix. What it
costs is four lines of the guest kernel configuration, `CONFIG_EROFS_FS`, in
mainline since Linux 5.4, and `_XATTR`, `_POSIX_ACL` and `_SECURITY` to carry
the extended attributes (where ext4 asks for nothing).

**Rejected.** ext4: no writer cove can take.

## 2026-09-20: cove transforms the layers itself

**Decided.** `internal/layer` turns the archive of one layer into instructions
for a `Writer` interface. It sanitizes the paths, keeps every entry under the
root, and applies the conventions of tar, OCI and overlayfs. No unpacker of
the market is linked for that step, and `mutate.Extract` is not used either.

**Why.** No file held in a layer is ever written to the file system of the
host. The three reference unpackers all do just that: containerd
`pkg/archive`, `umoci` `oci/layer` and `moby/go-archive` take a tar and
produce a directory on the host, which is the intermediate form the target
removed, and it costs the image twice its size on the way. Their shape leaves
no seam either: a tar enters, a directory comes out, and there is nowhere to
put a writer.

The conventions cannot be inherited either, because the market does not agree
on a single way to read them. Measured on one hostile tar: containerd and
`umoci` bound a name that climbs, where `moby` refuses it; none of the three
refuses a symbolic link whose target leaves the root; `tar2ext4` normalizes
both without a word. Cove answers for what its images hold, so the rule is
stated once, in a package that touches no file and can be fuzzed, rather than
measured out of a dependency at every bump.

**Rejected.** containerd `pkg/archive`, `umoci` `oci/layer` and
`moby/go-archive`, above. `mutate.Extract`, which concatenates the layers into
one tar with the whiteouts already applied: it reads the image as one stream,
where the pull unpacks each layer as its blob lands and names the layer that
refused an entry. `mkfs.erofs --tar`, which reads the archive and writes the
image in one external binary, treated in the entry on the disk.

## 2026-09-18: Apple `container` is no longer a backend of cove

**Decided.** Supporting Apple `container` is no longer in the target, so the
backend leaves the repository with `internal/sandbox`, which held it. `run`,
`send`, `stop` and `list` keep their flags, their validation, their usage
texts and their parsers, and each exits 125 once its arguments are checked,
until a backend runs them again. The last commit where the backend runs is
tagged `apple-container`.

**Why.** Removing it leaves a healthier base than keeping it would. The
project is young and no one runs cove, so the change is allowed to break:
nothing is kept working for compatibility while the backend is replaced.

## 2026-09-18: dependencies are vendored

**Decided.** `vendor/` is committed. A dependency enters with
`go get <module>@latest`, and the version that command resolves is what counts
from then on: pinned in `go.mod`, its sum in `go.sum`, its source under
`vendor/`, the three changed in the same commit as the code that first imports
the packages. `go mod vendor` follows every change of `go.mod`; the build reads
the directory by default once it exists, so nothing else is configured.

**Why.** Every line the binary links can be read in review: a bump of a
dependency shows as a diff of source, where `go.sum` alone shows a changed
hash. The build no longer depends on the proxy, so a CI job, a bisect or an
offline build gives the same binary, and a module withdrawn upstream cannot
break it. The trust base becomes visible at a glance, and the figure is the
reference the next addition is measured against, `pull` included: 13 modules,
63 packages, 723 files, 12 MB on disk; the binary links 9 of the modules, the
other four (testify, go-cmp, yaml, gotest.tools) serve the tests only.

**Rejected.** `go.sum` alone (integrity, not readability). A vendored slice
without the modules the tests need (`go mod vendor` does not distinguish, and
the test dependencies are part of what is read). A tool that audits the module
graph instead (it reports; the vendored diff is the audit).

**In the contract.** Each dependency is justified in this log, and the count
above is what a new one is compared to. "Standard library only" is no longer a
promise of the README; the smallest trust base that does the job is.

## 2026-09-18: the image pipeline is its own package, and cove writes the store

**Decided.** `internal/image` holds the rule on references (moved out of
`sandbox`, which now imports it), the store and the pull. The store is an OCI
layout at `~/.cache/cove/images`, `$XDG_CACHE_HOME/cove/images` when that
variable is set; the downloads in progress and the locks live next to it in
`tmp/`. Cove writes the blobs and `index.json` itself; from the library it
keeps the format, its reading and the annotation that carries the reference
(`org.opencontainers.image.ref.name`).

**Why.** One package per concern: `sandbox` drives the container CLI, and
`pull` talks to a registry and a directory, never to that CLI; a store that
`run` will read with the embedded backend has no reason to know the CLI
exists. The rule on references moved because `pull` needs it, it is about
images, and a rule is stated once. The layout sits under `images/` rather than
at the root of the cache because the format wants three entries at its root
and nothing else: `skopeo inspect oci:~/.cache/cove/images:<ref>` reads it as
it is, `rm -rf` empties it without touching `rootfs/` or `runs/`, and `tmp/`
outside the layout keeps the same file system for the rename to stay atomic.
Cove writes the store because the writers of the library, read in v0.22.1,
do not hold what an unattended cache needs: `layout.WriteBlob` opens
`blobs/sha256/<hex>` directly, without temporary or rename, and takes a blob
of any size for valid when the expected size is unknown, so a manifest
truncated by a `kill -9` would pass every later check; `AppendDescriptor`
rewrites `index.json` in place and appends a moved tag as a second entry. A
blob goes through `tmp/<digest>.<pid>`, is hashed on the way and renamed only
once the hash is its name; the index goes through a temporary, an fsync and a
rename, one entry per reference. One lock of the system per blob, at a path
two pulls compute alone, so that they only wait on each other for a layer both
want at that moment and a base layer shared by two images comes down once; the
kernel releases it when its holder dies, and the sweep of `tmp/` at start
follows that lock, not the pid in the name.

**Rejected.** The pull inside `sandbox` (it would tie the store to the
presence of the container CLI). The writers of the library (above). One lock
per image (a shared layer would come down twice) and one lock for the store
(the second pull would wait to the end of the first). A blocking `flock` (the
runtime restarts it after a signal, and a Ctrl-C must still be answered, so
the wait polls). Removing lock files in the sweep (a lock removed between the
look of a sweep and the `flock` of a pull lets two pulls lock two inodes of
one name). Resuming a download in the middle of a layer (docker does;
deferred, the granularity stays the layer and the structure of the store does
not change when it comes).

**In the contract.** `pull` prints the digest of the platform manifest, never
of an index, and asks the registry on every call, by digest included; `run`
will start an image named by digest and present in the store without the
network. The two rules serve the same promise, to say what actually ran.

## 2026-09-17: `pull` links go-containerregistry

**Decided.** The image pipeline links `github.com/google/go-containerregistry`
v0.22.1, published 2026-09-04 and still the latest on the proxy on
2026-09-18: `pkg/name` for the references, `pkg/v1/remote` for the registry,
`authn.DefaultKeychain` for the credentials the user already has, `pkg/v1`
and the reading of `pkg/v1/layout` for the store. The layers are read
one by one by `internal/layer`, never by `mutate.Extract`: reading the archive
of a layer is the one step that parses hostile data on the host, and it is
cove's own code.

**Measured** on the same program written twice (parse of a reference,
platform `linux/arm64`, keychain, pull), compiled with Go 1.26 on macOS 26.5:

- **Trust base.** go-containerregistry brings 9 modules, itself included, and
  61 non standard packages into the probe. containers/image v5.36.2 brings 54
  modules and 301 packages, among them `containers/storage`, `docker/docker`,
  `grpc`, `protobuf`, four `sigstore` modules, `go-jose`, `miekg/pkcs11`,
  `letsencrypt/boulder`, `mattn/go-sqlite3` and `mpb` for progress bars. The
  probe binary weighs 10.1 MB against 26.5 MB. The cove binary with `pull`
  weighs 10.4 MB, and 30 packages of the library are vendored.
- **cgo.** containers/image does not build as it comes: `proglottis/gpgme`
  wants the native library, `-tags containers_image_openpgp` removes it, and
  `mattn/go-sqlite3` stays in the graph for the blob info cache.
  go-containerregistry builds under `CGO_ENABLED=0` untouched.
- **Credentials.** `authn.DefaultKeychain` is a cascade, first found served,
  never a merge: the docker config, `$DOCKER_CONFIG` naming its directory in
  the place of `~/.docker` when set; else the file `$REGISTRY_AUTH_FILE`
  names; else `containers/auth.json` under `$XDG_RUNTIME_DIR`, then under
  `$XDG_CONFIG_HOME` (`~/.config` by default). The credential helpers of the
  file kept are called through `docker/cli` and `docker-credential-helpers`,
  the modules podman uses for the same job. Two consequences, verified against
  a registry served by the tests: a docker config that does not know the
  registry hides a `containers/auth.json` that does, and a `$DOCKER_CONFIG`
  pointing at a directory without `config.json` gives an empty configuration
  even when `~/.docker/config.json` exists.
- **Retries.** Three attempts, one second then three of wait with 10 % of
  jitter, on transient network errors and on 408, 429, 499, 500, 502, 503, 504
  and 522. Kept as they are; the library says nothing by default, so cove
  writes each new attempt on stderr.

**Why.** containers/image carries the needs of podman: several sources,
several destinations, a trust policy for an enterprise. Cove pulls one image
from one registry into a store it reads itself, and 45 extra modules cannot
be justified by features the target rules out. Integrity does not come from
the library either way: the manifest is named by its own sha256 and lists the
sha256 of each layer, and cove verifies both on the way into the store.

**Given up, and what it would cost to get back.**

- **Mirrors of `registries.conf`**, the only real loss: a `[[registry]]`
  block rewrites the name the user wrote and lists hosts to try in order,
  which serves a cache in the datacenter and a way around the rate limit of
  Docker Hub. A table from written name to hosts plus a retry loop, not a
  reason to link the rest.
- **Signature verification** by `policy.json`, GPG through `gpgme` or sigstore
  through Fulcio. It answers "who produced these bytes", a question the target
  does not ask while an image is pinned by digest and that digest is in the
  report. The day it is asked, `sigstore-go` alone answers it.
- **The `containers-storage:` destination**, which keeps the layers separate
  for the kernel to stack with overlayfs. Cove keeps them separate too, one
  EROFS blob per layer, but in a cache of its own that it writes and reads
  itself, on a host that is macOS first. The other transports are covered:
  `pkg/v1/layout` is `oci:`, `pkg/v1/tarball` is `docker-archive:`,
  `pkg/v1/daemon` is `docker-daemon:`.

Two more settings of `registries.conf` are not losses.
`unqualified-search-registries` would make the origin of an image depend on
the machine, which the rule on references already refuses. `blocked` is weaker
than the domain list of the run, which decides the same thing for every
connection of the VM.

**Rejected.** containers/image, on the count above. `skopeo` or `podman` as a
subprocess, which breaks the promise of one binary with no other tool to
install and puts the image policy in a program cove does not ship. The
`docker` CLI for the same reason, plus a daemon.

**In the contract.** The image of a run is named by digest and that digest is
in the report; the pull verifies it, and no library choice moves that
guarantee.

## 2026-09-13: the agent runs without permission prompts

**Decided.** `send` starts `claude` with `--dangerously-skip-permissions`, in
both regimes, attached terminal included; no option of `send` and no setting
of the sandbox brings the prompts back. The disclaimer Claude Code shows once
before starting in that mode is answered by the first launch state of the
image (`bypassPermissionsModeAccepted` in `claude.json`), next to the
onboarding and the trust of `/work`.

**Why.** The sandbox is the boundary: a prompt inside it protects nothing the
VM does not already contain, and costs the run. Without the flag the README
promise was broken in both regimes. Driven, print mode never waits for an
answer: it denies the tool and goes on, so a turn that had to write a file or
run a command failed without anyone being asked. Attached, the human was asked
at every edit and command, the permission fatigue the target rules out first.
Docker Sandboxes (`sbx run claude` is `claude --dangerously-skip-permissions`)
and yoloAI do the same, with no opt-out documented.

**Rejected.** Prompts kept in the attached regime, as if a human watching made
them useful (the boundary is the same in both regimes, and the attached
terminal is the same sandbox); an option of `send` to keep them (a caller who
wants prompts wants a workstation, not a sandbox); cove detecting an image
that runs the agent as root (Claude Code refuses the flag as root and says so,
and cove lets that refusal pass as it lets every refusal of `container exec`).

**In the contract.** Not being root outside a declared sandbox is the one
condition Claude Code puts on the flag; the image meets it as root since the
amendment of the entry of 2026-09-07. The prompts bypass
mode does not remove (`ask` rules, deletion of critical paths) stay with the
agent: driven, it denies them and the turn continues. `--permission-prompts
none` (2.1.259 and later) treats those residual prompts as an unsupervised
turn would: to consider at the next bump of the image.

## 2026-09-13: image profiles, named at `run`

**Decided.** `run --image` names the image of the VM, `cove-sandbox:local` by
default; nothing is remembered between runs, by VM or by repository, and the
repository declares nothing (target, 2.6). The preflight checks the image asked
for, before anything is spent: a name without a registry (`demo`,
`cove-sandbox:local`) is never looked for on the internet and must be in the
local store, one that names its registry (`ghcr.io/...`, `localhost:5000/...`)
is pulled when absent. Once the VM runs and before the repository enters it,
`claude --version` is the one check of the image, and a VM that fails it is
removed. A profile must keep the agent, the first launch state of the agent
in its home, and no command launched by default; nothing else is verified, and
the project lands in its `WorkingDir` (2026-09-24). `images/go` is the maintained profile, Go
and golangci-lint pinned to the versions of the workstation, whose only
commitment is that cove builds, lints and tests in it.

**Why.** The base image carries no runtime: on a Go repository the agent reads
but can neither build nor test, so it cannot meet the definition of done of the
project. The rule of docker tells a registry from a bare name, and
`container run` on a bare name absent from the store queries docker.io and
fails 401 without saying why. The agent is checked in the VM that was created
rather than in a throwaway one: a boot costs 3 to 5 s, the abort path of the
seeding already exists, and a wrong image is the exception. The profile is not
a barrier because what the agent installs disappears with the VM; it pins what
the definition of done needs.

**Rejected.** A memory of the image per VM or per repository (the caller
declares); a check of the other profile rules (it would test the Dockerfile
against itself, as the verification script of the base image did); a version in
the tag of the profile (the build command would change at every bump); a
profile that repeats the pinning and bump procedure of the base image (cove
building and testing in it stands in for a verification). The limitation
accepted then, the agent as uid 1000 without sudo so that a profile had to
install its tools as root and restore the user, is lifted by the amendment of
the entry of 2026-09-07.

**In the contract.** The image of a run is the caller's declaration, whole,
and `list` shows the one actually used. The rules of a profile replace "the
user of the base image" and "nothing in the home": the user describes the base image, and the
home must keep the first launch state, not stay empty.

## 2026-09-10: the whole repository in `/work`, without a remote

**Decided.** `run` takes a forge URL and a branch, never a local path or the
current directory; the forge default branch when none is given. The whole
repository enters the VM: every branch and tag under its own name, the full
history, checked out on the requested branch, and no remote at all. The agent
commits as `agent <agent@cove.invalid>`, a domain reserved by RFC 2606, never
signed with a key of the workstation.

**How.** The repository is read on the host with the access its owner already
has, into a bare receiver repository that only cove's argv configures: objects
validated on entry, submodules left empty, replace refs ignored, the owner's
global hooks silenced, a local path refused by git itself. Only branches and
tags cross; `refs/replace/`, `refs/notes/` and the merge request refs stay on
the forge. Everything that can fail does so before the VM exists. The objects
travel as a bundle on the stdin of `container exec`, then a `git fetch` of that
file inside the VM as the image user.

**Why.** Cove shares nothing with the user's machine; the owner's access stays
on the host and never enters the VM. A `git push` from inside fails for lack of
a destination and never asks for a credential. Signing the agent's commits with
the owner's key would destroy non-repudiation.

**Rejected.** A local clone as input; a shallow clone; `git clone` of the
bundle (branches under `refs/remotes/origin/`, a remote left pointing at the
bundle); `receive-pack` through `exec -i` (twice slower, an ssh helper); a
direct clone from the VM for public repositories (two mechanisms). Deferred: an
exact version (commit or tag) as the starting point, and a capability relay
that would give the sandbox a remote with wide read and narrow write.

## 2026-09-09: `send`, two regimes in one verb, cove owns the argv

**Decided.** `send` is a thin layer over `container exec`. Without a prompt it
attaches the agent's REPL to the terminal; with one it drives a single turn
and copies the JSON of `claude` to stdout, unparsed and unfiltered. Cove draws
a UUID and imposes it as the session identifier, so that no byte read from the
VM ever becomes an identifier; `-n` names a thread, `-r` resumes one by UUID or
name, `-c` continues the last attached one (attached only: `claude` keeps no
record of driven threads for it). Every send without `-r` opens a new thread;
several threads share `/work` and cove arbitrates nothing between them.

**Why.** Compared against Docker Sandboxes, container-use, Fletch and the
Claude Code, Codex and Cursor CLIs: `sbx run` is the only harness carrying both
regimes in one verb, and the user wanted that. Cove types its own flags rather
than passing the agent's argv through, because a pass-through lets the user
overwrite the flags the output contract depends on.

**Rejected.** A text output mode, the only path that would need an escape
filter; filtering ANSI escapes at all, since JSON escapes control characters
by construction and a PTY cannot be protected; reading the session identifier
back from the agent's output; one thread per sandbox.

**In the contract.** The send is the third level of the declaration: the
model and the prompt are chosen here, at each turn, not at the creation of the
VM. The detached verb is this driven regime with a budget, a lease and a trace
on top; the attached regime stays as the interactive verb, with no resumed
session across VMs.

## 2026-09-08: the CLI is shaped like docker, podman and Apple container

**Decided.** Verbs and flags start from what docker, podman and Apple
`container` do, market standards developers already know, and diverge only
where isolation demands it. `run` creates the sandbox and nothing else, with
the flags of `docker run` its role justifies (`--name`, `--rm`, `--keep`,
`--cpus`, `-m`, `-e`) and no other: cove builds the argument array itself, so
a mount, a user, a working directory, a network option, the SSH agent or a
command cannot even be asked for. `stop` and `list` (aliases `ls`, `ps`) act
only on VMs carrying the label `cove=sandbox`; `--all` is a filtered loop,
never `container stop --all`; an unknown name exits 1, a cove-side failure
125, a usage error 2. The stop delay stays the default of `container`.

**Why.** Developers reuse what they know; the VM store of `container` is
shared with other VMs (Apple's image builder), so cove never stops what it did
not create. The Go standard library does the dispatch and the flags: a static
binary with minimal dependencies, no cobra. Every process is started with an
argument array, never a shell string, because a shell would interpret exactly
the bytes cove must only transport.

**Rejected.** `-w`, a free command, `-i` and `-t` on `run`; a refusal list of
flags (the accepted flags are the only ones defined, the rest does not exist);
the Fletch model with the agent as process 1 and the VM dying with it;
podman's 125 for an unknown name and its `--ignore`.

**In the contract.** The flags of `run` are the run level of the declaration,
`-e` first: a variable is a declared context entry, not a pass-through, and no
credential travels this way.

## 2026-09-07: the sandbox image

**Decided.** Debian trixie slim pinned by index digest, the native Claude Code
binary pinned by exact version and SHA256 with updates disabled, `git` and the
tools the model reaches for on its own (`curl`, `jq`, `patch`, `procps`,
`python3`), the agent as root with `/root` holding only the first-launch
state of Claude Code, the repository at `/work`, no entrypoint, built locally
as `cove-sandbox:local`. The detailed spec and the bump procedure are in
[images/sandbox/README.md](../images/sandbox/README.md).

**Why.** The image is the declared context: what it pins is what runs, and
what the host would leak is absent rather than forbidden. A missing tool costs
context and sometimes the run, more than a few tens of megabytes.

**Amended 2026-09-16: root instead of uid 1000.** The image first ran the
agent as a dedicated user, uid 1000 without sudo, and its installation rule
(everything in user space) followed from it. That user was never a
confinement choice but a workaround: Claude Code refuses its unsupervised
mode as root. The price was paid on legitimate destruction (need, objective
2): the agent could not install a package, recreate a test database, run
Docker or touch the global configuration, a read-only posture while nothing
it would break in the VM belongs to anyone. The boundary is the hypervisor
(target, 2.3), and Docker Sandboxes runs its agent as root for that reason.
Measured on the pinned 2.1.236, the refusal is lifted by `IS_SANDBOX=1` in
the environment, which the image now sets; the seeding and the agent run as
root, and a profile installs its tools with no user switch.

**Rejected.** Alpine (musl constraints on every layer), the install script and
the npm package (a launcher under `$HOME`, Node for nothing), a leaner second
image, a verification script (every check tested the Dockerfile against
itself), uid 1000 kept with sudo (the refusal stays under `sudo claude`, and
the friction with it), `CLAUDE_CODE_BUBBLEWRAP` as the variable that lifts the
check (it names a mechanism the image does not have). Accepted limitations:
apt packages are not pinned; `IS_SANDBOX` is undocumented, so the flag is run
as root again at every bump.

**In the contract.** The image is named by digest; a project image extends
this one with its toolchain.
