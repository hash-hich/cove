# The templates of the write disk: one empty ext4 per size a run can ask for, made by a pinned
# mkfs.ext4 on a sparse file of that size, of which only the blocks that are not zero are kept.
# Built from the root of the repository, the output replacing templates/:
#
#   docker build -f internal/writedisk/Dockerfile --output type=local,dest=internal/writedisk/templates .
#
# The same builder, packages and options give the same bytes, and the fingerprints the tests pin
# say so: a bump of e2fsprogs is a bump of DEBIAN_SNAPSHOT and E2FSPROGS_VERSION, and new
# fingerprints in template_test.go.
ARG BUILDER=debian:trixie-slim@sha256:a99cfc517144bc59b1978475ec53b46ecabec7e43635402ee5b77cc54cd1b20a
ARG GOLANG=golang:1.26-trixie@sha256:bdca99a00bc16590cb1a0bb4e698f5fc5d6a64e4d5eef13d9f18a0ee08e5fa65
ARG DEBIAN_SNAPSHOT=20260923T000000Z
ARG E2FSPROGS_VERSION=1.47.2-3+b12

FROM --platform=$BUILDPLATFORM ${GOLANG} AS tool
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
COPY vendor/ vendor/
COPY cmd/cove-template/ cmd/cove-template/
COPY internal/writedisk/*.go internal/writedisk/
# The package embeds the templates this build makes, so the tool is compiled with an empty one,
# which never leaves this stage.
RUN mkdir internal/writedisk/templates && : > internal/writedisk/templates/build.gz \
    && go build -trimpath -o /cove-template ./cmd/cove-template

FROM --platform=$BUILDPLATFORM ${BUILDER} AS build
ARG DEBIAN_SNAPSHOT
ARG E2FSPROGS_VERSION
# A snapshot is a past state of the archive, so its Release files are past their validity date by
# design.
RUN printf 'Types: deb\nURIs: http://snapshot.debian.org/archive/debian/%s\nSuites: trixie trixie-updates\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\n' \
      "${DEBIAN_SNAPSHOT}" > /etc/apt/sources.list.d/debian.sources \
    && apt-get -o Acquire::Check-Valid-Until=false update -qq \
    && apt-get install -y -qq --no-install-recommends "e2fsprogs=${E2FSPROGS_VERSION}" \
    && rm -rf /var/lib/apt/lists/*
COPY --from=tool /cove-template /usr/local/bin/

# What each option of mkfs.ext4 fixes:
#
# -b 4096, -i 16384: a block of the page of the guest, and an inode per 16 KiB, the default ratio.
#   The count of inodes never changes afterwards, and node_modules or the layers of an engine take
#   millions: an ENOSPC with space left is the most misleading failure. Unwritten tables cost the
#   template nothing.
# -m 0: no block kept for root, since the agent is root.
# -L cove-rw: the label the init checks before it mounts the disk.
# -U, hash_seed, E2FSPROGS_FAKE_TIME: the template is reproducible byte for byte. Every disk carries
#   the same UUID, which holds because a VM mounts one write disk only, by its device. The time is
#   one second after the epoch, since mke2fs reads zero as no time and takes the clock.
# lazy_itable_init, lazy_journal_init, nodiscard: neither the tables of inodes nor the journal are
#   zeroed, which is what keeps the template small. The init mounts with noinit_itable, or the
#   kernel zeroes them in the background and grows the file of the host by gigabytes.
# ^resize_inode: a disk is never resized, so no block is kept for it.
# -J size=256: a journal of 256 MiB whatever the size. It stays, as nothing else would bring back
#   a disk after a VM is cut off: cove ships no fsck.
#
# Each template is then written back to a disk as cove writes it, checked by e2fsck, and extracted
# again to the same bytes.
RUN set -e; mkdir /out /disk; cd /disk; \
    for size in 8g 16g 32g 64g 128g 256g 512g 1t; do \
      truncate -s "$(echo "$size" | tr gt GT)" "$size"; \
      E2FSPROGS_FAKE_TIME=1 mkfs.ext4 -F -q -b 4096 -i 16384 -m 0 -L cove-rw \
        -U 5f3c9a1e-7c2d-4b0e-9a61-3d8e2f4c6b10 \
        -E hash_seed=a1d0c6e2-3f4b-4c8a-9e7d-2b5f1a6c8e34,lazy_itable_init=1,lazy_journal_init=1,nodiscard \
        -O ^resize_inode -J size=256 "$size"; \
      cove-template extract "$size" "/out/$size.gz"; \
      rm "$size"; \
      cove-template write "$size" "/out/$size.gz" "$size"; \
      e2fsck -fn "$size"; \
      cove-template extract "$size" "$size.gz"; \
      cmp "/out/$size.gz" "$size.gz"; \
      rm "$size" "$size.gz"; \
    done

FROM scratch
COPY --from=build /out/ /
