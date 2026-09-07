# Sandbox image

Base OCI image booted by the cove micro-VM (Apple `container`, decision D6).
It contains Claude Code, git, and the minimum needed to run them, and nothing
from the host. The decisions below are recorded as D9 in
[docs/cadrage/06-decisions.md](../../docs/cadrage/06-decisions.md); this file
is the detailed spec.

## Build

```bash
container build --platform linux/arm64 -t cove-sandbox:local images/sandbox
```

The image is stored in the local `container` image store only. No registry is
involved for now: every host that runs cove builds the image itself from this
directory, which is what keeps the content reproducible from the repository
alone.

## Why there is no verification script

The guarantees the image gives (R1, R2) are structural (P2): the Dockerfile
copies nothing from the host, sets a non-root user, creates an empty home and
carries no credential. Nothing checks that better than reading its eighty
lines, and the build already fails when the pinned checksum does not match.
Claude Code itself refuses to start in bypass mode as root, so the user
choice is enforced at every run without a script. A verification script was
written for this issue and removed on purpose: every check it ran tested the
Dockerfile against itself, and its lists of paths and patterns were the kind
of list P2 warns about.

What can still defeat R1 and R2 is not the image but the way it is started:
`container run` accepts `--volume`, `-e` and `-u`. Those flags live in the
argument array cove will build, and that array is where a test belongs: a Go
unit test asserting that no mount, no environment variable and no user
override is passed. It comes with the issue that introduces the wrapper.

## Acceptance

Run once when the Dockerfile changes, and paste the output in the MR:

```bash
container run --rm cove-sandbox:local claude --version   # the pinned version
container run --rm cove-sandbox:local id -u              # 1000, not 0
container run --rm cove-sandbox:local git --version
container run --rm cove-sandbox:local env                # PATH, HOME, DISABLE_UPDATES only
```

## What the image contains

| Item | Value |
|------|-------|
| Base | `debian:trixie-slim`, linux/arm64, pinned by index digest |
| Claude Code | native binary, exact version and SHA256 pinned in the Dockerfile, at `/usr/local/bin/claude` |
| Packages added | `git`, `ca-certificates`, and the tools the model reaches for: `curl`, `jq`, `patch`, `procps`, `python3` (no recommends, apt lists removed) |
| User | `agent`, uid 1000, gid 1000, shell `/bin/bash` |
| `$HOME` | `/home/agent`, created empty |
| Working directory | `/work`, owned by `agent`, where the repository will live |
| Environment | `HOME=/home/agent`, `DISABLE_UPDATES=1` |
| Entrypoint | none; the default command is the base image's `bash`, cove passes the command at run time |

`agent`, `/home/agent` and `/work` are the contract for the repository
transport issue: the repository is placed under `/work`, owned by uid 1000
(git refuses a directory owned by another user), and Claude Code is started
with `/work` as its working directory.

## Decisions

Each entry answers one question of the issue: the choice, why, and what was
rejected.

1. **Base: Debian trixie slim.** Claude Code ships one native binary per
   libc; Debian is a documented target and glibc based, so the binary and the
   ripgrep bundled inside it run unmodified with no extra environment
   variable. The follow-up toolchain layers (Go, Node official tarballs) also
   target glibc. Rejected: Alpine, smaller by a few tens of megabytes but musl
   based, which needs `libgcc`, `libstdc++`, a system `ripgrep` and
   `USE_BUILTIN_RIPGREP=0`, and would push musl constraints onto every layer
   built on top. The size difference is noise next to the 332 MB binary.
2. **Installation: direct download of the native binary.** The Dockerfile
   downloads `claude-code-releases/<version>/linux-arm64/claude` and checks it
   against the SHA256 pinned next to the version. Rejected: the official
   `install.sh`, which always downloads the latest release first and then runs
   `claude install <version>`, leaving a launcher symlink and a versions
   directory under `$HOME`; and the npm package, which only wraps the same
   native binary and would add Node 22 and npm to the image for nothing.
3. **Pinning.** Base by index digest (what Docker Hub displays, so a bump is
   re-checkable by eye; the builder selects the arm64 manifest inside it).
   Claude Code by exact version plus the SHA256 published in the release
   manifest. `DISABLE_UPDATES=1` in the image so that neither the background
   updater nor `claude update` can replace the pinned version at run time.
   The apt packages (`git`, `ca-certificates`) are the one thing that is not
   pinned: see Limitations.
4. **User and paths.** A non-root user because Claude Code refuses
   `--dangerously-skip-permissions` as root. Name `agent`, uid and gid 1000 so
   that files handed to the VM by the host side map to a predictable id.
   `$HOME` is `/home/agent` and is created empty (`useradd --no-create-home`,
   then `install -d`): no `.bashrc`, no `.profile`, nothing that sources
   anything. The repository lives at `/work`, outside `$HOME`, so that `$HOME`
   only ever contains what Claude Code writes during the run. `HOME` is set
   explicitly in the image rather than left to the guest init.
5. **Deliberately absent.** No credentials, no host path, no shell profile,
   no apt lists, no package cache, no `~/.claude`, `~/.ssh`, `~/.aws`, no
   `/Users`. R1 holds by absence (P2): these paths do not exist, they are not
   merely forbidden (`ls` says "No such file or directory", not "Permission
   denied"). No `ENTRYPOINT`, so a derived
   image never has to undo one; the default command is the base image's
   `bash`. No telemetry related variables: what the guest may reach on the
   network is the networking issue's decision.
6. **Build and storage.** `container build` from this directory, tag
   `cove-sandbox:local`, local image store only. The tag does not carry the
   Claude Code version so that the build command in `AGENTS.md` does not
   change at every bump; the version is exposed by `claude --version` and by
   the `org.opencontainers.image.version` label.
7. **Extension point (not implemented).** A project image starts with
   `FROM cove-sandbox:local`, switches to `USER root` to add its toolchain,
   switches back to `USER agent`, keeps `/home/agent` empty and `/work` as the
   working directory, and passes the acceptance commands above. Go for cove
   itself and
   Node for a JavaScript repository are the first candidates.
8. **Common tools in the base.** Besides `git`, the base ships `curl`, `jq`,
   `patch`, `procps` and `python3`. They are not needed to run Claude Code;
   they are what the model reaches for on its own, measured on the owner's
   Claude Code history where `python3` ranks with `cat` and `sed`, ahead of
   `grep` and `go`. In an unsupervised run a missing tool costs context and
   sometimes the run, which outweighs a few tens of megabytes. Rejected: a
   second, leaner image, because no use case works better with less; and
   runtimes or linters in the base, which depend on the project and belong to
   the profile layer.
9. **Installation rule.** No sudo and no setuid helper. What the harness
   provides is what the project's definition of done needs (the runtime, its
   package manager, the linters), pinned in a profile layer. Everything else
   the agent installs in user space at run time (`go install`, `pip --user`,
   a binary under `/work`), with network access, and it disappears with the
   VM. The image that runs is thus always the one the Dockerfile describes,
   while the agent keeps the autonomy to do and not only to see.

## Bumping the pins

1. Pick the version: `curl -fsSL https://downloads.claude.ai/claude-code-releases/stable`
   prints the stable channel version, `.../latest` the newest one.
2. Verify the release manifest signature and read the checksum. The host has
   no gpg, so run it in a throwaway container:

   ```bash
   container run --rm -i debian:trixie-slim bash -s <version> <<'SH'
   set -euo pipefail
   V="$1"; R=https://downloads.claude.ai/claude-code-releases
   apt-get update >/dev/null && apt-get install -y --no-install-recommends ca-certificates curl gnupg jq >/dev/null
   curl -fsSL https://downloads.claude.ai/keys/claude-code.asc | gpg --import 2>/dev/null
   gpg --fingerprint security@anthropic.com | grep -A1 '^pub'
   curl -fsSLO "$R/$V/manifest.json" && curl -fsSLO "$R/$V/manifest.json.sig"
   gpg --verify manifest.json.sig manifest.json
   jq -r '.platforms["linux-arm64"] | "\(.checksum) \(.size)"' manifest.json
   SH
   ```

   The fingerprint must be `31DD DE24 DDFA B679 F42D 7BD2 BAA9 29FF 1A7E CACE`
   (compare with the Claude Code setup documentation, not with this file) and
   gpg must report a good signature.
3. Update `CLAUDE_CODE_VERSION` and `CLAUDE_CODE_SHA256` in the Dockerfile.
4. For the base, take the current index digest of `debian:trixie-slim` on
   Docker Hub and update `DEBIAN_DIGEST`.
5. Rebuild and run the acceptance commands.

## Limitations

- **apt packages are not pinned.** `git` and `ca-certificates` come from the
  trixie archive at build time; only security updates move on a stable
  release, but two builds on different days can differ in that layer.
  Accepted for now: pinning exact package versions breaks the build at every
  security update, and snapshot.debian.org adds slowness and moving parts.
  `container image inspect cove-sandbox:local` gives the digest actually
  built when the MR or the run log needs to record what ran.
- **The digest is not stable across builds.** Two builds from the same
  commit produce the same files but not the same digest: layer timestamps
  and the apt state differ by a few bytes. What is reproducible is the
  content (base by digest, binary by SHA256), not the image identifier.
  Compare the acceptance output, not digests, between hosts.
- **The tool list is a starting point.** It comes from one owner's usage
  history; the rule is to start restrictive and add a tool to the base only
  when real runs show it missing in every project.
- **Only `claude --version` is exercised.** A real headless run needs the
  broker (D1), so runtime dependencies beyond starting the binary (for
  instance `procps` for process listing) are validated by the first real run,
  not by this image.
- **The manifest signature is checked at bump time, not at build time.** The
  build re-checks the pinned SHA256, which is what the signature covered when
  the maintainer verified it. Verifying the signature in every build would add
  gnupg and a keyring to the download stage for no additional guarantee.
- **linux/arm64 only.** The checksum is per platform. Another platform means
  another checksum line and a build argument, not done until needed.
- **No registry.** Sharing the image between hosts is out of scope; each host
  builds it.
