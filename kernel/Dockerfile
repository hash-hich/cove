# The guest kernel of cove: Linux from kernel.org, unpatched, configured from
# allnoconfig by the fragments of config/ and nothing else. README.md next to
# this file says how to build it and what each fragment is for.

# The tarball is pinned by version and SHA256, the checksum being what the
# build re-checks; the SHA256 is the one kernel.org publishes in
# sha256sums.asc. The compiler is part of what decides the bytes of the
# kernel, and the configuration records its version in CONFIG_CC_VERSION_TEXT:
# the builder is pinned by digest, and its packages come from the Debian
# archive frozen at one date, so that a later build installs the same ones.
ARG KERNEL_VERSION=6.18.53
ARG KERNEL_SHA256=4d6fba95c2244b08a7b4144a4d38b9be4fb31abb5e7682ae40bb5cb11374cfe0
ARG BUILDER=debian:trixie-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a
ARG DEBIAN_SNAPSHOT=20260923T000000Z

# The build runs on the platform of the host and cross-compiles to the target,
# so both architectures build on either host without emulation.
FROM --platform=$BUILDPLATFORM ${BUILDER} AS build
ARG KERNEL_VERSION
ARG KERNEL_SHA256
ARG DEBIAN_SNAPSHOT
ARG TARGETARCH
# A snapshot is a past state of the archive, so its Release files are past
# their validity date by design.
RUN printf 'Types: deb\nURIs: http://snapshot.debian.org/archive/debian/%s\nSuites: trixie trixie-updates\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\n\nTypes: deb\nURIs: http://snapshot.debian.org/archive/debian-security/%s\nSuites: trixie-security\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\n' \
      "${DEBIAN_SNAPSHOT}" "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Check-Valid-Until=false update -qq \
    && apt-get install -y -qq --no-install-recommends \
      bc bison ca-certificates cpio curl flex gcc gcc-aarch64-linux-gnu gcc-x86-64-linux-gnu \
      libc6-dev libelf-dev libssl-dev make python3 xz-utils \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL -o /linux.tar.xz "https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-${KERNEL_VERSION}.tar.xz" \
    && echo "${KERNEL_SHA256}  /linux.tar.xz" | sha256sum -c - \
    && mkdir /linux && tar -C /linux --strip-components=1 -xJf /linux.tar.xz \
    && rm /linux.tar.xz
COPY config/ /fragments/
WORKDIR /linux

# The build identity is fixed, so that the same sources and fragments give the
# same bytes on any host: the kernel otherwise records the date, user and host
# of its build in its version string.
ENV KBUILD_BUILD_TIMESTAMP="Thu Jan  1 00:00:00 UTC 1970" \
    KBUILD_BUILD_USER=cove \
    KBUILD_BUILD_HOST=cove

# merge_config.sh -n starts from allnoconfig and only warns when a dependency
# refuses an option, so the loop after it fails the build instead: a fragment
# line that does not hold in the final configuration is a kernel that is not
# the one this directory describes.
RUN case "${TARGETARCH}" in \
      arm64) export ARCH=arm64 CROSS_COMPILE=aarch64-linux-gnu- ARCHFRAG=arm64 ;; \
      amd64) export ARCH=x86_64 CROSS_COMPILE=x86_64-linux-gnu- ARCHFRAG=x86_64 ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && set -- /fragments/base.config /fragments/cove.config /fragments/containers.config \
      "/fragments/${ARCHFRAG}.config" \
    && scripts/kconfig/merge_config.sh -n "$@" >/dev/null \
    && grep -h -E '^(CONFIG_|# CONFIG_.* is not set)' "$@" | sort -u > /wanted \
    && missing=$(grep -v -x -F -f .config /wanted || true) \
    && if [ -n "${missing}" ]; then echo "not in the final configuration:" >&2; echo "${missing}" >&2; exit 1; fi \
    && make -s -j"$(nproc)" \
    && mkdir /out && cp .config /out/config \
    && case "${ARCH}" in \
      arm64) cp arch/arm64/boot/Image /out/kernel ;; \
      x86_64) cp vmlinux /out/kernel ;; \
    esac

# Only the kernel and its configuration leave the build: the raw Image on
# arm64, the ELF vmlinux on x86_64, the formats libkrun, Firecracker and
# Virtualization.framework load.
FROM scratch
COPY --from=build /out/ /
