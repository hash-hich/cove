package layer

import (
	"io/fs"
	"strings"
)

// index is the tree of the paths written so far: what the checks on the parents of an entry, on
// the target of a hard link and on a name written twice read. Each node
// holds the type of its entry and, for a directory, its children by name. A path is absolute and
// clean, the root being "/", and a directory is what the archive declared or what the writer made
// on the way as the parent of an entry: the index does not tell the two apart, since neither does
// the blob. A tree rather than a map of paths so that replacing a directory drops what it holds in
// one step, whatever an archive does to it.
type index struct {
	root node
}

// newIndex returns the index of a layer nothing was written to yet: the root, a directory.
func newIndex() index {
	return index{root: node{mode: fs.ModeDir}}
}

// node is one written path. Only a directory has children.
type node struct {
	mode     fs.FileMode
	children map[string]*node
}

// lookup returns the node at p, nil when nothing was written there.
func (x *index) lookup(p string) *node {
	n := &x.root
	for name := range components(p) {
		if n = n.children[name]; n == nil {
			return nil
		}
	}
	return n
}

// obstacle returns the first ancestor of p that was written as something other than a directory,
// with its node, or nil when every ancestor is a directory or nothing.
func (x *index) obstacle(p string) (string, *node) {
	n := &x.root
	depth := 0
	for name := range components(p) {
		if n = n.children[name]; n == nil {
			return "", nil
		}
		depth += len(name) + 1
		if depth < len(p) && !n.mode.IsDir() {
			return p[:depth], n
		}
	}
	return "", nil
}

// put records that p was written as an entry of type mode, the ancestors nothing was written at
// becoming directories. A directory written over a directory keeps what it holds; anything else
// replaces the whole subtree, as extracting the archive would.
func (x *index) put(p string, mode fs.FileMode) {
	n := &x.root
	for name := range components(p) {
		child := n.children[name]
		if child == nil {
			child = &node{mode: fs.ModeDir}
			if n.children == nil {
				n.children = map[string]*node{}
			}
			n.children[name] = child
		}
		n = child
	}
	if !n.mode.IsDir() || !mode.IsDir() {
		n.children = nil
	}
	n.mode = mode
}

// components yields the names of the absolute clean path p under the root, none for the root.
func components(p string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		if p == "/" {
			return
		}
		for name := range strings.SplitSeq(p[1:], "/") {
			if !yield(name) {
				return
			}
		}
	}
}
