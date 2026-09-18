// Package image brings OCI images into the store of cove: the rule on the references it accepts,
// the store itself, and the pull from a registry.
package image

import "strings"

// NamesRegistry reports whether image says where it comes from: its first path component is a
// host, one with a dot or a port or localhost, the rule of docker and containerd. A bare name
// (demo, org/repo:tag) is one of docker.io for them, which is never what a cove image is: such a
// name is local by definition and is never looked for on the internet.
func NamesRegistry(image string) bool {
	host, _, found := strings.Cut(image, "/")
	if !found {
		return false
	}
	return host == "localhost" || strings.ContainsAny(host, ".:")
}
