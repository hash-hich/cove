# Go profile

The [base image](../sandbox/README.md) plus the Go toolchain and
golangci-lint. A maintained profile, meant to be used as it is, whose only
commitment is that cove builds, lints and tests in it: that is what the
acceptance verifies, and nothing more is promised to other Go repositories.
The rules every profile must keep are in the extension point of the base
image README.

## Build

```bash
docker build --platform linux/arm64 -t cove-sandbox:local images/sandbox   # the base, first
docker build --platform linux/arm64 -t cove-go:local images/go
```

The tag follows the base image: local store only, no version in the tag, so
that the command above does not change at every bump.

## Acceptance

Run once when the Dockerfile changes, and paste the output in the MR:

```bash
docker run --rm cove-go:local go version              # the pinned version
docker run --rm cove-go:local golangci-lint version   # the pinned version
docker run --rm cove-go:local id -u                   # 0, as the base image
docker run --rm cove-go:local ls -A /root             # .claude.json only
```

Then the commitment itself, on a clone of cove fetched inside the image, the
way a sandbox gets it: its definition of done runs there as it runs on the
workstation. Nothing of the host is mounted, as in a real run.

```bash
docker run --rm cove-go:local sh -c '
  git clone --depth 1 https://gitlab.com/hich-hich/cove.git /work &&
  cd /work && go build ./... && golangci-lint run && go test ./...'
```

## What the profile adds

| Item | Value |
|------|-------|
| Go | official linux/arm64 tarball, exact version and SHA256 pinned in the Dockerfile, at `/usr/local/go` |
| golangci-lint | release binary, exact version and SHA256 pinned in the Dockerfile, at `/usr/local/bin/golangci-lint` |
| Environment | `PATH` with `/usr/local/go/bin` first, `GOTOOLCHAIN=local` |

Everything else is the base image, untouched: the agent and its first launch
state, `/work`, no entrypoint.

## Decisions

1. **Versions: those of the workstation of the maintainer.** Go 1.26.8 and
   golangci-lint 2.13.2 are what `go build`, `golangci-lint run` and
   `go test` were measured with, and the `go` line of `go.mod` is the floor
   the toolchain must reach. Rejected: tracking the latest release, which
   would make the profile drift from what the definition of done was
   written against.
2. **`GOTOOLCHAIN=local`.** Go downloads another toolchain on its own when a
   `go.mod` asks for a newer one; the profile forbids it, as the base image
   forbids Claude Code to update itself, so that what runs is what the
   Dockerfile names. A repository asking for a newer Go fails with a clear
   message, and the answer is a bump of the profile.
3. **Direct downloads, verified by checksum.** The official tarball of Go and
   the release archive of golangci-lint, checked against the SHA256 pinned
   next to the version. Rejected: `go install` of golangci-lint, which builds
   it from source at every build and does not pin what the build produces;
   the Debian packages, which lag several versions behind.
4. **A download stage.** The archives are fetched and unpacked in a stage of
   their own and only the two tools are copied over, so that neither the
   archives nor the apt state of the base image change in the profile.
5. **No user switch.** The base image runs as root, so the tools land under
   `/usr/local` with no `USER` line to add or to restore. Nothing else of the
   base image is touched, which is how the profile keeps the rules of the
   extension point without repeating them.
6. **Caches under `$HOME` at run time.** `go` and `golangci-lint` write their
   caches under `/root/.cache`, the module cache under `/root/go` and their
   configuration under `/root/.config` during the run
   (measured: those three next to `.claude.json` after the acceptance). The
   home is no longer required to hold the first launch state alone: what a
   profile must not do is break that state, and a cache next to it does not.

## Bumping the pins

1. Go: `curl -fsSL 'https://go.dev/dl/?mode=json'` lists the stable releases
   with the SHA256 of each file; take the one of
   `go<version>.linux-arm64.tar.gz`.
2. golangci-lint: the checksums file of the release,
   `golangci-lint-<version>-checksums.txt` on the releases page; take the
   line of `golangci-lint-<version>-linux-arm64.tar.gz`.
3. Update the four `ARG` values in the Dockerfile, rebuild and run the
   acceptance, the commitment included.

## Limitations

- **One commitment.** Another Go repository may need a tool this profile
  does not carry (a code generator, a database driver's C library); the
  agent installs it in user space during the run, or a profile of its own
  is written.
- **No procedure for the base.** The profile does not repeat the pinning
  and bump procedure of the base image for the apt layer: it inherits that
  layer as it is, and the acceptance above is what stands in for a
  verification.
- **linux/arm64 only.** Both checksums are per platform, as in the base
  image.
- **No registry.** The profile is built on every host, after the base
  image; a profile on a host without the base fails at `FROM`.
