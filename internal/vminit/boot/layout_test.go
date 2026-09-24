package boot_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/hich-hich/cove/internal/vminit/boot"
	"gitlab.com/hich-hich/cove/internal/vminit/spec"
)

func runOf(layers ...int64) *spec.Run {
	s := &spec.Run{MountOptions: []string{"xino=on", "redirect_dir=on"}, Write: spec.Disk{Size: 1 << 30}}
	for i, size := range layers {
		s.Layers = append(s.Layers, spec.Layer{
			Disk: spec.Disk{Size: size}, DiffID: "sha256:" + string(rune('a'+i)), Mountpoint: "/l/0" + string(rune('0'+i)),
		})
	}
	return s
}

func TestWorkingDir(t *testing.T) {
	t.Parallel()
	const work, home = "/work", "/home/agent"
	for wd, want := range map[string]string{
		"":         work,
		"/":        work,
		"//":       work,
		home:       home,
		home + "/": home,
	} {
		s := runOf()
		s.WorkingDir = wd
		require.Equal(t, want, boot.WorkingDir(s), wd)
	}
}

func TestTheOverlayStacksTheLayersHighestFirstOverTheUpper(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		"lowerdir=/l/00:/l/01:/l/02,upperdir=/w/upper,workdir=/w/work,xino=on,redirect_dir=on",
		boot.OverlayOptions(runOf(4096, 8192, 4096)))
}

func TestVolumesAreMountedParentsFirst(t *testing.T) {
	t.Parallel()
	const docker = "/var/lib/docker"
	got, err := boot.VolumePaths([]string{docker + "/volumes", "/data/", docker, "/data"})
	require.NoError(t, err)
	require.Equal(t, []string{"/data", docker, docker + "/volumes"}, got)

	_, err = boot.VolumePaths([]string{"data"})
	require.EqualError(t, err, "image sets Volume data, not an absolute path")
	_, err = boot.VolumePaths([]string{"/."})
	require.EqualError(t, err, "image sets Volume /, which would hide the image")
}

func TestTheNetworkFiles(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		"127.0.0.1\tlocalhost\n::1\tlocalhost ip6-localhost ip6-loopback\n127.0.1.1\tsandbox\n",
		string(boot.HostsFile("sandbox")))
	require.Equal(t, "nameserver 10.0.2.3\nnameserver fd00::3\n",
		string(boot.ResolvConf([]string{"10.0.2.3", "fd00::3"})))
	require.Empty(t, boot.ResolvConf(nil))
}

func TestResolveInFollowsTheLinksOfTheImageInsideIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, d := range []string{"usr/etc", "var/lib", "run/systemd/resolve"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o750))
	}
	links := map[string]string{
		"etc":             "/usr/etc",
		"usr/etc/up":      "../../..",
		"var/run":         "../run",
		"usr/etc/resolv":  "/run/systemd/resolve/stub-resolv.conf",
		"var/lib/loop":    "loop",
		"var/lib/docker":  "/../../../srv/docker",
		"var/lib/nothing": "missing/deeper",
	}
	for name, target := range links {
		require.NoError(t, os.Symlink(target, filepath.Join(root, name)))
	}

	//nolint:gosec // G101: paths of an image, not credentials.
	tests := map[string]string{
		"/":                  "/",
		"/etc/passwd":        "/usr/etc/passwd",
		"/etc/up/etc/passwd": "/usr/etc/passwd",
		"/var/run/docker":    "/run/docker",
		"/etc/resolv":        "/run/systemd/resolve/stub-resolv.conf",
		"/../../etc":         "/usr/etc",
		"/var/lib/docker":    "/srv/docker",
		"/var/lib/nothing/x": "/var/lib/missing/deeper/x",
		"/new/dir":           "/new/dir",
	}
	for p, want := range tests {
		got, err := boot.ResolveIn(root, p)
		require.NoError(t, err, p)
		require.Equal(t, filepath.Join(root, want), got, p)
	}

	_, err := boot.ResolveIn(root, "/var/lib/loop")
	require.ErrorContains(t, err, "too many levels of symbolic links")
}

func TestDisksAreOrderedAsTheKernelNamesThem(t *testing.T) {
	t.Parallel()
	names := []string{"vdaa", "vdk", "vdz", "vdj", "vdab"}
	boot.DiskOrder(names)
	require.Equal(t, []string{"vdj", "vdk", "vdz", "vdaa", "vdab"}, names)
}

func TestTheDisksMustBeThoseOfTheRun(t *testing.T) {
	t.Parallel()
	s := runOf(8192, 20480)
	names := []string{"vda", "vdb", "vdc"}

	require.NoError(t, boot.MatchDisks(s, names, []int64{8192, 20480, 1 << 30}))
	require.EqualError(t, boot.MatchDisks(s, names[:2], []int64{8192, 20480}),
		"the VM has 2 disks, the run describes 3, 2 layers and the write disk")
	require.EqualError(t, boot.MatchDisks(s, names, []int64{20480, 8192, 1 << 30}),
		"disk vda holds 20480 bytes, layer sha256:a is 8192")
	require.EqualError(t, boot.MatchDisks(s, names, []int64{8192, 20480, 4096}),
		"disk vdc holds 4096 bytes, the write disk is 1073741824")
}

// superblock returns the superblock of an ext4 labelled label, as the init reads it on a disk.
func superblock(magic uint16, label string) []byte {
	sb := make([]byte, 1024)
	binary.LittleEndian.PutUint16(sb[0x38:], magic)
	copy(sb[0x78:0x78+16], label)
	return sb
}

func TestTheWriteDiskIsTheExt4OfTheTemplate(t *testing.T) {
	t.Parallel()
	require.NoError(t, boot.CheckWriteDisk("vdc", superblock(0xef53, spec.WriteLabel)))
	require.EqualError(t, boot.CheckWriteDisk("vdc", superblock(0xef53, "data")),
		`disk vdc is labelled "data", not "cove-rw": it is not the write disk`)
	require.EqualError(t, boot.CheckWriteDisk("vdc", superblock(0xe0f5, spec.WriteLabel)),
		"disk vdc holds no ext4, it is not the write disk")
	require.EqualError(t, boot.CheckWriteDisk("vdc", superblock(0xef53, "cove-rw-extra-lo")),
		`disk vdc is labelled "cove-rw-extra-lo", not "cove-rw": it is not the write disk`)
	require.Error(t, boot.CheckWriteDisk("vdc", nil))
}
