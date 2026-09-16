# Base image of the cove sandbox: Claude Code, git, and nothing else.
# Rationale and limitations: README.md next to this file, and the image
# entry of docs/decisions.md. Only linux/arm64 is built (Apple Silicon
# host): the checksum below is the one of the linux-arm64 binary.

# Everything downloaded during the build is pinned here, so that a clean
# checkout rebuilds the same content. Bumping any of these values is the
# procedure described in README.md (the manifest signature is verified by the
# maintainer at bump time, the build only re-checks the pinned checksum).
ARG DEBIAN_DIGEST=sha256:d7e12182ce18b85b93007c1dedf31f2d29e01ccf3182cc4017c709b6259bc132
ARG CLAUDE_CODE_VERSION=2.1.236
ARG CLAUDE_CODE_SHA256=c38d37deaf1643083326c48a6acc0afb09dada126e6bda77ef1a4410ae60ca12

# Stage 1: download the native binary and check it against the pinned checksum.
# A separate stage keeps curl, its dependencies and the apt state of this step
# out of the final image. The official install script is not used: it always
# fetches the latest release before installing the requested one, and leaves a
# launcher symlink plus a versions directory under $HOME. The npm package is not
# used either: it only wraps the same native binary and would pull node into
# the image for nothing.
FROM debian:trixie-slim@${DEBIAN_DIGEST} AS download
ARG CLAUDE_CODE_VERSION
ARG CLAUDE_CODE_SHA256
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /claude \
      "https://downloads.claude.ai/claude-code-releases/${CLAUDE_CODE_VERSION}/linux-arm64/claude" \
    && echo "${CLAUDE_CODE_SHA256}  /claude" | sha256sum -c - \
    && chmod 0755 /claude

# Stage 2: the image itself.
FROM debian:trixie-slim@${DEBIAN_DIGEST}
ARG CLAUDE_CODE_VERSION

# git is the only tool claude needs besides itself; ca-certificates lets git
# and claude open TLS connections. The other packages are the tools the model
# reaches for on its own, measured on the owner's Claude Code history (python3
# ranks with cat and sed, ahead of grep): in an unsupervised run a missing
# tool costs context and sometimes the run, which is worth more than a few
# tens of megabytes. Runtimes and project tooling (Go, Node, linters) are not
# here: they belong to the profile layer built on top of this image. The apt
# lists are removed so that no package index is left in the image. Debian is
# glibc based, so the ripgrep bundled in the claude binary runs as is; on a
# musl base it would have to be installed separately.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        jq \
        patch \
        procps \
        python3 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=download /claude /usr/local/bin/claude

# Claude Code refuses to bypass permissions when running as root, so the agent
# runs as a dedicated user. The home directory is created by hand, not by
# useradd, which would copy /etc/skel (.bashrc, .profile): nothing of the host
# enters $HOME, only what this Dockerfile and the run write. /work is
# where the repository will live; it is created here, owned by the user,
# because WORKDIR would create it owned by root and git would refuse the
# directory.
RUN groupadd --gid 1000 agent \
    && useradd --uid 1000 --gid agent --shell /bin/bash --no-create-home agent \
    && install -d -m 0755 -o agent -g agent /home/agent /work

# The only file in $HOME: the state Claude Code writes after its first launch
# (onboarding done, /work trusted, the disclaimer of the bypass permissions
# mode accepted), so that a fresh sandbox answers the first turn instead of
# asking. No credential and no version in it; the keys are undocumented and
# measured on the pinned version, so the first launch is replayed by hand at
# every bump (README.md). Mode 0600 is what claude gives the file when it
# writes it itself.
COPY --chown=agent:agent --chmod=0600 claude.json /home/agent/.claude.json

# HOME is set explicitly rather than derived from /etc/passwd by the runtime,
# because claude writes its state under $HOME and the guest init must not be
# trusted to compute it. DISABLE_UPDATES blocks background and manual updates
# so that the version pinned above is the one that runs.
ENV HOME=/home/agent \
    DISABLE_UPDATES=1

LABEL org.opencontainers.image.title="cove sandbox" \
      org.opencontainers.image.description="Claude Code and git for the cove micro-VM" \
      org.opencontainers.image.version="${CLAUDE_CODE_VERSION}"

USER agent
WORKDIR /work

# No sudo and no setuid helper: the agent installs what it wants in user
# space (go install, pip --user, a binary under /work) but cannot change the
# system packages, so the image that runs stays the one the Dockerfile
# describes.
#
# No ENTRYPOINT: cove passes the whole command at run time, and a derived
# project image must not have to undo one. The default command stays the
# base image's bash, which is only what an interactive shell gets.
