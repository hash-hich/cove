# The environment mkemptyext4 runs in: Go and the e2fsprogs whose bytes internal/emptyext4/generated
# holds. The Makefile runs it: make emptyext4 makes them again, make emptyext4-check compares them
# with the committed SHA256SUMS. A line that changes while the command of mkfs and the version of
# e2fsprogs did not says the making is not reproducible.
#
# The sparse files mkfs.ext4 writes stay in the container; only what is kept of them reaches the
# repository. A bump of e2fsprogs is a bump of DEBIAN_SNAPSHOT and E2FSPROGS_VERSION, of
# e2fsprogsVersion in main.go, and a new SHA256SUMS.
ARG GOLANG=golang:1.26-trixie@sha256:bdca99a00bc16590cb1a0bb4e698f5fc5d6a64e4d5eef13d9f18a0ee08e5fa65
FROM ${GOLANG}
ARG DEBIAN_SNAPSHOT=20260923T000000Z
ARG E2FSPROGS_VERSION=1.47.2-3+b12
# A snapshot is a past state of the archive, so its Release files are past their validity date by
# design.
RUN rm -f /etc/apt/sources.list.d/* \
    && printf 'Types: deb\nURIs: http://snapshot.debian.org/archive/debian/%s\nSuites: trixie trixie-updates\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\n' \
      "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Check-Valid-Until=false update -qq \
    && apt-get install -y -qq --no-install-recommends --allow-downgrades "e2fsprogs=${E2FSPROGS_VERSION}" \
    && rm -rf /var/lib/apt/lists/*
ENV GOTOOLCHAIN=local
