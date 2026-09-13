# Go profile of the cove sandbox: the base image plus the Go toolchain and
# golangci-lint, so that a Go repository can be built, linted and tested in
# it. The only commitment is that cove itself builds, lints and tests here
# (README.md next to this file); the rules a profile must keep are in the
# extension point of images/sandbox/README.md.

# Both downloads are pinned by exact version and SHA256, the checksum being
# what the build re-checks. The versions are the ones of the workstation of
# the maintainer: that is what "cove builds here" was measured with, and the
# go line of go.mod is the floor the toolchain must reach.
ARG GO_VERSION=1.26.8
ARG GO_SHA256=211ffced9dcb9633a55eac6364816ec0ddd951389a740e88fa8b3337971bdda0
ARG GOLANGCI_LINT_VERSION=2.13.2
ARG GOLANGCI_LINT_SHA256=a2a4e0065aa41be71f7c5ac90f271b61751331e5d04314e62afe4027855f0893

# Stage 1: download and verify. The base image already carries curl and
# ca-certificates; a separate stage keeps the archives out of the layers of
# the profile. Root, because the base image runs as its agent user and
# /usr/local is not its.
FROM cove-sandbox:local AS download
ARG GO_VERSION
ARG GO_SHA256
ARG GOLANGCI_LINT_VERSION
ARG GOLANGCI_LINT_SHA256
USER root
RUN curl -fsSL -o /go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-arm64.tar.gz" \
    && echo "${GO_SHA256}  /go.tgz" | sha256sum -c - \
    && tar -C /usr/local -xzf /go.tgz \
    && rm /go.tgz
RUN curl -fsSL -o /golangci-lint.tgz \
      "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-arm64.tar.gz" \
    && echo "${GOLANGCI_LINT_SHA256}  /golangci-lint.tgz" | sha256sum -c - \
    && tar -C / -xzf /golangci-lint.tgz --strip-components=1 \
      "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-arm64/golangci-lint" \
    && rm /golangci-lint.tgz

# Stage 2: the profile. Only the two tools cross over; the agent, its first
# launch state, /work and the absence of an entrypoint are inherited from the
# base image untouched. The tools land as root, since the base image has no
# sudo, and the user of the base image is restored before the end.
FROM cove-sandbox:local
USER root
COPY --from=download /usr/local/go /usr/local/go
COPY --from=download /golangci-lint /usr/local/bin/golangci-lint
USER agent

# GOTOOLCHAIN=local holds go to the version pinned above, as DISABLE_UPDATES
# holds claude to its own: a go.mod asking for a newer Go fails with a clear
# message instead of a silent download of a toolchain this file does not
# name. The caches of go and golangci-lint land under $HOME at run time, next
# to the first launch state of the agent, which they do not touch.
ENV PATH=/usr/local/go/bin:$PATH \
    GOTOOLCHAIN=local

LABEL org.opencontainers.image.title="cove go sandbox" \
      org.opencontainers.image.description="The cove sandbox with Go and golangci-lint"
