# The commands of cove, written once: the steps of the build that go build alone does not chain,
# and those of every day. make with no target lists them.

.DEFAULT_GOAL := help

# ARCHS are the architectures of the guest, as Go names them.
ARCHS := arm64 amd64

# BUILD names the tree cove and cove-vmm are built from: its commit, what differs from it, and
# the files git does not track yet. cove refuses a cove-vmm of another build, since the two change
# together.
BUILD := $(shell { git rev-parse HEAD; git diff HEAD; git ls-files -z -o --exclude-standard | \
	xargs -0 shasum -a 256; } | shasum -a 256 | cut -c 1-16)
BUILD_LDFLAGS := -X gitlab.com/hich-hich/cove/internal/vmmproto.build=$(BUILD)

# LIBEXEC holds what cove runs a VM with, found beside the bin directory of cove.
LIBEXEC := libexec

# mkemptyext4 runs in the image of its Dockerfile, where e2fsprogs is pinned, on the repository
# mounted as it is.
MKEMPTYEXT4 := docker run --rm -v "$(CURDIR)":/cove -w /cove/tools/mkemptyext4 cove-mkemptyext4

.PHONY: help cove cove-init cove-vmm libkrun kernel image-sandbox image-go fmt lint test check \
	emptyext4 emptyext4-check mkemptyext4-docker-image

## help: list the targets and what each one does
help:
	@awk '/^## / { sub(/^## /, ""); i = index($$0, ": "); \
		printf "%-26s %s\n", substr($$0, 1, i - 1), substr($$0, i + 2) }' $(MAKEFILE_LIST)

## cove: build the CLI into bin/cove
cove:
	go build -ldflags "$(BUILD_LDFLAGS)" -o bin/cove ./cmd/cove

## libkrun: build libkrun as third_party/libkrun.lock pins it into libexec/lib, see its build.sh
libkrun:
	third_party/libkrun/build.sh $(LIBEXEC)/lib

## cove-vmm: build the process that runs the monitor of a VM into libexec, signed to create VMs
cove-vmm: libkrun
	@lib=$$(ls $(LIBEXEC)/lib/libkrun.*) && \
	sum=$$(shasum -a 256 "$$lib" | cut -d ' ' -f 1) && \
	. third_party/libkrun.lock && \
	case $$(uname -s) in Darwin) rpath=@loader_path/lib ;; *) rpath='$$ORIGIN/lib' ;; esac && \
	CGO_ENABLED=1 CGO_LDFLAGS="$(CURDIR)/$$lib" go build -o $(LIBEXEC)/cove-vmm \
		-ldflags "$(BUILD_LDFLAGS) -X main.libkrunVersion=$$LIBKRUN_TAG -X main.libkrunSHA256=$$sum \
			-extldflags=-Wl,-rpath,$$rpath" \
		./cmd/cove-vmm
	@if [ "$$(uname -s)" = Darwin ]; then \
		codesign -s - -f --entitlements cmd/cove-vmm/entitlements.plist $(LIBEXEC)/cove-vmm; fi

## cove-init: build the init of the VM, static, into bin/cove-init/<arch>/cove-init for each guest
cove-init:
	@for arch in $(ARCHS); do \
		echo "cove-init $$arch"; \
		CGO_ENABLED=0 GOOS=linux GOARCH=$$arch go build -o bin/cove-init/$$arch/cove-init ./cmd/cove-init || exit 1; \
	done

## kernel: build the guest kernel into bin/kernel/<arch> for each guest, see kernel/README.md
kernel:
	@for arch in $(ARCHS); do \
		docker build --platform linux/$$arch --output type=local,dest=bin/kernel/$$arch kernel || exit 1; \
	done

## image-sandbox: build cove-sandbox:local, the base image, see images/sandbox/README.md
image-sandbox:
	docker build --platform linux/arm64 -t cove-sandbox:local images/sandbox

## image-go: build cove-go:local, the Go profile on the base, see images/go/README.md
image-go: image-sandbox
	docker build --platform linux/arm64 -t cove-go:local images/go

## fmt: format the Go code of both modules (gofumpt and gci)
fmt:
	golangci-lint fmt
	cd tools/mkemptyext4 && golangci-lint fmt

## lint: lint both modules for the host and for Linux, formatting drift included
lint:
	golangci-lint run
	GOOS=linux golangci-lint run
	cd tools/mkemptyext4 && golangci-lint run && GOOS=linux golangci-lint run

## test: run the tests of both modules; those that need the pinned e2fsprogs run in emptyext4-check
test:
	go test ./...
	cd tools/mkemptyext4 && go test ./...

## check: what a commit must pass, lint then test
check: lint test

## emptyext4: make again the empty ext4 of internal/emptyext4/generated and their SHA256SUMS
emptyext4: mkemptyext4-docker-image
	$(MKEMPTYEXT4) go run .

## emptyext4-check: make them again, compare with the committed SHA256SUMS, e2fsck each one
emptyext4-check: mkemptyext4-docker-image
	$(MKEMPTYEXT4) go test -count=1 ./...

## mkemptyext4-docker-image: build cove-mkemptyext4, the Docker image mkemptyext4 runs in
mkemptyext4-docker-image:
	docker build -q -t cove-mkemptyext4 tools/mkemptyext4
