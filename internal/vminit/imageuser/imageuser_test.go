package imageuser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vminit/imageuser"
)

const users = `root:x:0:0:root:/root:/bin/sh
# a comment, then a blank line

daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
node:x:1000:1000::/home/node:/bin/bash
short:x:1001
`

const groups = `root:x:0:
daemon:x:1:
wheel:x:10:root,node
docker:x:999:node
node:x:1000:
staff:x:50:
`

const nodeHome = "/home/node"

func TestResolve(t *testing.T) {
	t.Parallel()
	tests := []struct {
		user string
		want imageuser.Identity
	}{
		// No User is root, and root keeps the groups that list it.
		{"", imageuser.Identity{UID: 0, GID: 0, Groups: []uint32{10}, Home: "/root"}},
		{"node", imageuser.Identity{UID: 1000, GID: 1000, Groups: []uint32{10, 999}, Home: nodeHome}},
		{"1000", imageuser.Identity{UID: 1000, GID: 1000, Groups: []uint32{10, 999}, Home: nodeHome}},
		// A group given is the only group.
		{"node:staff", imageuser.Identity{UID: 1000, GID: 50, Groups: []uint32{}, Home: nodeHome}},
		{"node:50", imageuser.Identity{UID: 1000, GID: 50, Groups: []uint32{}, Home: nodeHome}},
		{"1000:staff", imageuser.Identity{UID: 1000, GID: 50, Groups: []uint32{}, Home: nodeHome}},
		{"1000:4242", imageuser.Identity{UID: 1000, GID: 4242, Groups: []uint32{}, Home: nodeHome}},
		// A uid /etc/passwd does not list is accepted, gid 0 and home /.
		{"4242", imageuser.Identity{UID: 4242, GID: 0, Groups: []uint32{}, Home: "/"}},
		{"4242:4243", imageuser.Identity{UID: 4242, GID: 4243, Groups: []uint32{}, Home: "/"}},
		// A line short of fields reads as empty.
		{"short", imageuser.Identity{UID: 1001, GID: 0, Groups: []uint32{}, Home: ""}},
	}
	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			t.Parallel()
			got, err := imageuser.Resolve(tt.user, strings.NewReader(users), strings.NewReader(groups))
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveRefusesANameTheImageDoesNotList(t *testing.T) {
	t.Parallel()
	_, err := imageuser.Resolve("agent", strings.NewReader(users), strings.NewReader(groups))
	require.EqualError(t, err, "image sets User agent, not found in the image's /etc/passwd")

	_, err = imageuser.Resolve("node:admins", strings.NewReader(users), strings.NewReader(groups))
	require.EqualError(t, err, "image sets User node:admins, group admins not found in the image's /etc/group")
}

func TestResolveInAnImageWithoutPasswd(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	id, err := imageuser.ResolveIn(root, "")
	require.NoError(t, err)
	require.Equal(t, imageuser.Identity{Groups: []uint32{}, Home: "/"}, id)

	id, err = imageuser.ResolveIn(root, "1000:1000")
	require.NoError(t, err)
	require.Equal(t, imageuser.Identity{UID: 1000, GID: 1000, Groups: []uint32{}, Home: "/"}, id)

	_, err = imageuser.ResolveIn(root, "node")
	require.Error(t, err)
}

func TestResolveInReadsTheFilesOfTheImage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "etc"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc/passwd"), []byte(users), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "etc/group"), []byte(groups), 0o600))

	id, err := imageuser.ResolveIn(root, "node")
	require.NoError(t, err)
	require.Equal(t, imageuser.Identity{UID: 1000, GID: 1000, Groups: []uint32{10, 999}, Home: nodeHome}, id)
}
