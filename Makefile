# The steps of the build that go build alone does not chain.

# mkemptyext4 runs in the image of its Dockerfile, where e2fsprogs is pinned, on the repository
# mounted as it is.
MKEMPTYEXT4 := docker run --rm -v "$(CURDIR)":/cove -w /cove/tools/mkemptyext4 cove-mkemptyext4

.PHONY: emptyext4 emptyext4-check mkemptyext4-docker-image

# emptyext4 makes again the empty ext4 of internal/emptyext4/generated and their SHA256SUMS.
emptyext4: mkemptyext4-docker-image
	$(MKEMPTYEXT4) go run .

# emptyext4-check makes them again, fails on a line of SHA256SUMS that differs from the committed
# one, and checks each committed empty ext4 with e2fsck.
emptyext4-check: mkemptyext4-docker-image
	$(MKEMPTYEXT4) go test -count=1 ./...

# mkemptyext4-docker-image builds cove-mkemptyext4, the Docker image mkemptyext4 runs in: Go and
# the e2fsprogs pinned in tools/mkemptyext4/Dockerfile.
mkemptyext4-docker-image:
	docker build -q -t cove-mkemptyext4 tools/mkemptyext4
