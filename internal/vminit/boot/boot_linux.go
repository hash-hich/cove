package boot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/internal/kernelcheck"
	"gitlab.com/hich-hich/cove/internal/vminit/imageuser"
	"gitlab.com/hich-hich/cove/internal/vminit/launch"
	"gitlab.com/hich-hich/cove/internal/vminit/spec"
)

// rootMount is the root of the image in the initramfs, the overlay of the layers and the upper.
const rootMount = "/r"

// maxOpenFiles is the hard limit on open files the processes of the image start with. A VM
// without systemd starts at 4096, and dockerd and node count on the higher limit docker gave them.
const maxOpenFiles = 1 << 20

// Main runs the init and powers the VM off when it is done, whatever happened: the init is PID 1,
// and the kernel panics when PID 1 returns.
func Main() {
	if err := run(); err != nil {
		say("%v", err)
	}
	unix.Sync()
	_ = unix.Reboot(unix.LINUX_REBOOT_CMD_POWER_OFF)
	// A kernel that refuses to power off leaves the init nothing to do but wait for the end.
	select {}
}

func say(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stdout, spec.ConsolePrefix+format+"\n", args...)
}

// run boots the VM, then runs the command of the description when there is one, and returns
// when the VM should power off.
func run() error {
	s, err := boot()
	if err != nil {
		return err
	}
	l, err := launch.New(rootMount)
	if err != nil {
		return err //nolint:wrapcheck // New names the step that failed.
	}
	cmd, err := prepare(l, s)
	if err != nil {
		return err
	}
	say(spec.Ready)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, unix.SIGTERM, unix.SIGINT, unix.SIGPWR)
	if len(s.Command) == 0 {
		<-stop
		return nil
	}
	return runCommand(l, s, cmd, stop)
}

// boot mounts the image root and everything under it, and returns the description of the run.
func boot() (*spec.Run, error) {
	for _, m := range []struct{ fstype, target string }{
		{"proc", "/proc"}, {"sysfs", "/sys"}, {"devtmpfs", "/dev"},
	} {
		if err := mount(m.fstype, m.fstype, m.target, 0, ""); err != nil {
			return nil, err
		}
	}
	if err := kernelcheck.CheckRunning(); err != nil {
		return nil, err //nolint:wrapcheck // The refusal names what the kernel lacks.
	}
	s, err := spec.Read(spec.Path)
	if err != nil {
		return nil, err //nolint:wrapcheck // Read names the description.
	}
	if err := limits(); err != nil {
		return nil, err
	}
	if err := mountImage(s); err != nil {
		return nil, err
	}
	if err := mountSystem(rootMount); err != nil {
		return nil, err
	}
	if err := mountVolumes(s.Volumes); err != nil {
		return nil, err
	}
	if err := name(s); err != nil {
		return nil, err
	}
	return s, nil
}

// limits sets what docker set for the processes of an image and a VM without systemd does not:
// the mask of new files, and the hard limit on open files.
func limits() error {
	unix.Umask(0o022)
	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &lim); err != nil {
		return fmt.Errorf("read the limit on open files: %w", err)
	}
	lim.Max = maxOpenFiles
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &lim); err != nil {
		return fmt.Errorf("raise the limit on open files: %w", err)
	}
	return nil
}

// mkdirAll creates the directory p and its parents, readable by every user as a system lays out
// its directories: the root of the initramfs and the directories of the image are read by
// whoever the image runs as.
func mkdirAll(p string) error {
	//nolint:gosec // G301: a directory of the system, read by every user of the image.
	if err := os.MkdirAll(p, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", p, err)
	}
	return nil
}

// mkdir creates the directory p, as mkdirAll does, and fails when it exists.
func mkdir(p string) error {
	//nolint:gosec // G301: a directory of the system, read by every user of the image.
	return os.Mkdir(p, 0o755) //nolint:wrapcheck // The callers name the directory.
}

// mount mounts source on target, a directory it creates when missing, and names the option the
// kernel lacks when it does not know fstype.
func mount(source, fstype, target string, flags uintptr, data string) error {
	if err := mkdirAll(target); err != nil {
		return err
	}
	if err := unix.Mount(source, target, fstype, flags, data); err != nil {
		//nolint:wrapcheck // FromMount keeps the error it was given, or names the option missing.
		return kernelcheck.FromMount(fstype, fmt.Errorf("mount %s on %s: %w", fstype, target, err))
	}
	return nil
}

// mountImage mounts the layers and the write disk, then the image root over them.
func mountImage(s *spec.Run) error {
	disks, err := virtioDisks()
	if err != nil {
		return err
	}
	if err := matchDisksOf(s, disks); err != nil {
		return err
	}
	for i, l := range s.Layers {
		if err := mount("/dev/"+disks[i], "erofs", l.Mountpoint, unix.MS_RDONLY, ""); err != nil {
			return fmt.Errorf("layer %s: %w", l.DiffID, err)
		}
	}
	if err := mountWriteDisk(disks[len(disks)-1]); err != nil {
		return err
	}
	// A run starts on an empty disk and leaves nothing to the next: an upper already there is what
	// another run wrote.
	for _, d := range []string{upperDir, workDir, volumesDir} {
		if err := mkdir(d); errors.Is(err, fs.ErrExist) {
			return errors.New("the write disk holds what another run wrote")
		} else if err != nil {
			return fmt.Errorf("create %s on the write disk: %w", d, err)
		}
	}
	return mount("overlay", "overlay", rootMount, 0, overlayOptions(s))
}

// writeOptions are the options of the mount of the write disk. noinit_itable keeps the kernel from
// zeroing the tables of inodes the empty ext4 leaves unwritten, which would grow the file of the host
// by gigabytes for nothing. errors=remount-ro turns the whole disk read only at the first error of
// I/O, at once and visibly, rather than failing writes one by one. nodiscard says that what a run
// frees does not go back to the host, as long as no backend carries a discard to the file.
const writeOptions = "errors=remount-ro,noinit_itable,nodiscard"

// mountWriteDisk mounts the disk named disk on writeMount once its superblock says it is a write
// disk.
func mountWriteDisk(disk string) error {
	f, err := os.Open("/dev/" + disk) //nolint:gosec // G304: disk is a disk the kernel lists.
	if err != nil {
		return fmt.Errorf("open the write disk: %w", err)
	}
	sb := make([]byte, superblockSize)
	_, err = f.ReadAt(sb, superblockOffset)
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("read the superblock of %s: %w", disk, err)
	}
	if err := checkWriteDisk(disk, sb); err != nil {
		return err
	}
	if err := mount("/dev/"+disk, "ext4", writeMount, unix.MS_NOATIME, writeOptions); err != nil {
		return fmt.Errorf("the write disk: %w", err)
	}
	return nil
}

// virtioDisks returns the virtio disks of the VM in their order of attachment.
func virtioDisks() ([]string, error) {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil, fmt.Errorf("list the disks: %w", err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "vd") {
			names = append(names, e.Name())
		}
	}
	diskOrder(names)
	return names, nil
}

// matchDisksOf checks the disks against the description by their sizes, which the kernel gives in
// sectors of 512 bytes.
func matchDisksOf(s *spec.Run, disks []string) error {
	sizes := make([]int64, len(disks))
	for i, d := range disks {
		b, err := os.ReadFile(filepath.Join("/sys/block", d, "size")) //nolint:gosec // G304: d is a disk the kernel lists.
		if err != nil {
			return fmt.Errorf("read the size of %s: %w", d, err)
		}
		n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil {
			return fmt.Errorf("read the size of %s: %w", d, err)
		}
		sizes[i] = n * 512
	}
	return matchDisks(s, disks, sizes)
}

// mountSystem mounts in the image root the file systems every program expects, where docker would
// have. /dev is the whole devtmpfs rather than the handful of nodes docker gives, and the
// terminals are a devpts of their own, whose ptmx /dev/ptmx is. /run and /tmp are a tmpfs, as a
// systemd machine mounts them: what they hold dies with the VM and never lands on the write disk.
// cgroup2 and mqueue are mounted when the kernel has them: nothing of the init needs them, and
// what the image runs says itself what it misses.
func mountSystem(root string) error {
	const tmpfs = "tmpfs"
	mounts := []struct {
		fstype, target string
		flags          uintptr
		data           string
		optional       bool
	}{
		{"proc", "/proc", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, "", false},
		{"sysfs", "/sys", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, "", false},
		{"cgroup2", "/sys/fs/cgroup", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, "", true},
		{"devtmpfs", "/dev", unix.MS_NOSUID, "", false},
		{"devpts", "/dev/pts", unix.MS_NOSUID | unix.MS_NOEXEC, "newinstance,gid=5,mode=620,ptmxmode=666", false},
		{tmpfs, "/dev/shm", unix.MS_NOSUID | unix.MS_NODEV, "mode=1777", false},
		{"mqueue", "/dev/mqueue", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, "", true},
		{tmpfs, "/run", unix.MS_NOSUID | unix.MS_NODEV, "mode=755", false},
		{tmpfs, "/tmp", unix.MS_NOSUID | unix.MS_NODEV, "mode=1777", false},
	}
	for _, m := range mounts {
		target, err := resolveIn(root, m.target)
		if err != nil {
			return err
		}
		err = mount(m.fstype, m.fstype, target, m.flags, m.data)
		if m.optional && errors.Is(err, unix.ENODEV) {
			continue
		}
		if err != nil {
			return err
		}
	}
	ptmx := filepath.Join(root, "dev/ptmx")
	if err := unix.Mount(filepath.Join(root, "dev/pts/ptmx"), ptmx, "", unix.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind the ptmx of the terminals on /dev/ptmx: %w", err)
	}
	return nil
}

// mountVolumes gives each volume of the image a directory of the write disk, a real file system
// where the image root is an overlay, and binds it on the path of the volume. What the image holds
// at that path is copied into it first, as docker fills an anonymous volume the first time it is
// mounted: a copy, not a stack, since an overlay cannot take an upper inside another overlay and a
// container engine keeps its own under its directory of data.
func mountVolumes(vols []string) error {
	paths, err := volumePaths(vols)
	if err != nil {
		return err
	}
	for i, v := range paths {
		target, err := resolveIn(rootMount, v)
		if err != nil {
			return err
		}
		dir := filepath.Join(volumesDir, fmt.Sprintf("%02d", i))
		if err := fillVolume(v, target, dir); err != nil {
			return err
		}
		if err := unix.Mount(dir, target, "", unix.MS_BIND, ""); err != nil {
			return fmt.Errorf("mount volume %s: %w", v, err)
		}
	}
	return nil
}

// fillVolume creates dir, the directory of the volume v, with what the image holds at target, or
// empty when the image holds nothing there, in which case target is created to mount it on.
func fillVolume(v, target, dir string) error {
	fi, err := os.Lstat(target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := mkdirAll(target); err != nil {
			return fmt.Errorf("volume %s: %w", v, err)
		}
		if err := mkdir(dir); err != nil {
			return fmt.Errorf("create volume %s: %w", v, err)
		}
	case err != nil:
		return fmt.Errorf("volume %s: %w", v, err)
	case !fi.IsDir():
		return fmt.Errorf("image sets Volume %s, which the image holds as a file", v)
	default:
		if err := copyTree(target, dir); err != nil {
			return fmt.Errorf("fill volume %s: %w", v, err)
		}
	}
	return nil
}

// name gives the VM its name and its resolvers, and brings the loopback up. The files are written
// in the upper, never bound over the image: the agent may edit them as on a machine.
func name(s *spec.Run) error {
	if err := unix.Sethostname([]byte(s.Hostname)); err != nil {
		return fmt.Errorf("name the VM: %w", err)
	}
	for _, f := range []struct {
		path string
		data []byte
	}{
		{"/etc/hostname", []byte(s.Hostname + "\n")},
		{"/etc/hosts", hostsFile(s.Hostname)},
		{"/etc/resolv.conf", resolvConf(s.Nameservers)},
	} {
		if err := writeInImage(f.path, f.data); err != nil {
			return err
		}
	}
	return loopbackUp()
}

// writeInImage writes data at p in the image root, replacing what is there: a resolv.conf that
// is a link to /run/systemd would otherwise have the file written wherever the link leads.
func writeInImage(p string, data []byte) error {
	dir, err := resolveIn(rootMount, path.Dir(p))
	if err != nil {
		return err
	}
	if err := mkdirAll(dir); err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	target := filepath.Join(dir, path.Base(p))
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("write %s: %w", p, err)
	}
	fd, err := unix.Open(target, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o644)
	if err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	f := os.NewFile(uintptr(fd), target)
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", p, err)
	}
	return nil
}

// loopbackUp brings lo up, which nothing else does in a VM without systemd.
func loopbackUp() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("bring the loopback up: %w", err)
	}
	defer func() { _ = unix.Close(fd) }()
	ifr, err := unix.NewIfreq("lo")
	if err != nil {
		return fmt.Errorf("bring the loopback up: %w", err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		return fmt.Errorf("bring the loopback up: %w", err)
	}
	ifr.SetUint16(ifr.Uint16() | unix.IFF_UP)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, ifr); err != nil {
		return fmt.Errorf("bring the loopback up: %w", err)
	}
	return nil
}

// prepare checks, in the root of the image, what a turn will need, and creates the directory of
// the project. It returns the process a turn starts from, less its command line.
func prepare(l *launch.Launcher, s *spec.Run) (launch.Command, error) {
	var cmd launch.Command
	err := l.InRoot(func() error {
		id, err := imageuser.ResolveIn("/", s.User)
		if err != nil {
			return err //nolint:wrapcheck // The refusal names the User of the image.
		}
		env := launch.Environ(s.Env, id.Home, s.Hostname, false)
		if _, err := launch.LookPath(s.Agent, launch.Getenv(env, "PATH")); err != nil {
			return err //nolint:wrapcheck // The refusal names the agent and the PATH.
		}
		wd := workingDir(s)
		if err := mkdirAll(wd); err != nil {
			return fmt.Errorf("the working directory: %w", err)
		}
		project := path.Join(wd, s.Project)
		if err := mkdir(project); errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists in the image, cove clones the project there", project)
		} else if err != nil {
			return fmt.Errorf("create the project directory %s: %w", project, err)
		}
		if err := os.Chown(project, int(id.UID), int(id.GID)); err != nil {
			return fmt.Errorf("give %s to the user of the image: %w", project, err)
		}
		cmd = launch.Command{Env: env, Dir: project, Identity: id}
		return nil
	})
	return cmd, err //nolint:wrapcheck // The function run in the root names what failed.
}

// runCommand runs the command of the description on the console, stops it with the stop signal of
// the image when the VM is asked to stop, and writes its exit code on the console.
func runCommand(l *launch.Launcher, s *spec.Run, cmd launch.Command, stop <-chan os.Signal) error {
	sig, err := launch.ParseSignal(s.StopSignal)
	if err != nil {
		return err //nolint:wrapcheck // The refusal names the StopSignal of the image.
	}
	cmd.Args = s.Command
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	p, err := l.Start(cmd)
	if err != nil {
		return err //nolint:wrapcheck // Start names the command.
	}
	exited := make(chan int, 1)
	go func() { exited <- p.Wait() }()
	var code int
	select {
	case code = <-exited:
	case <-stop:
		if code, err = p.Stop(sig); err != nil {
			return err //nolint:wrapcheck // Stop names the process.
		}
	}
	say("exit %d", code)
	return nil
}
