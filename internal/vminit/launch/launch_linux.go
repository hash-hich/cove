package launch

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/internal/vminit/imageuser"
)

// StopGrace is how long a process has to end once it has been sent its stop signal, before it is
// killed, as docker gives it, when the stop comes with no grace of its own.
const StopGrace = 10 * time.Second

// Launcher starts processes in the mount namespace of the agent, whose root is the root of the
// image, and collects the end of every child of the init.
//
// The namespace lives on one thread of the init, locked to the goroutine that entered it and
// never released: a process forked from that thread inherits its namespace and its root, and
// nothing else in the init runs there. One namespace serves every process, so a mount the agent
// makes in one turn is still there at the next, as on a machine.
type Launcher struct {
	calls chan func()
	// mu is held across each fork and the registration of its pid, and across each wait: the end
	// of a process cannot be collected before its pid is known, nor its pid given to another
	// process while a signal is on its way to it.
	mu      sync.Mutex
	waiting map[int]*Process
}

// New creates the mount namespace of the agent, a copy of the namespace of the init as it stands,
// with root as its root, and starts collecting the children of the init.
func New(root string) (*Launcher, error) {
	l := &Launcher{calls: make(chan func()), waiting: make(map[int]*Process)}
	ready := make(chan error)
	go l.serve(root, ready)
	if err := <-ready; err != nil {
		return nil, err
	}
	sigchld := make(chan os.Signal, 1)
	signal.Notify(sigchld, unix.SIGCHLD)
	go func() {
		for range sigchld {
			l.reap()
		}
	}()
	return l, nil
}

func (l *Launcher) serve(root string, ready chan<- error) {
	runtime.LockOSThread()
	if err := enter(root); err != nil {
		// The thread is left locked, so the runtime ends it with the goroutine rather than giving
		// a half entered namespace to another goroutine.
		ready <- err
		return
	}
	ready <- nil
	for fn := range l.calls {
		fn()
	}
}

// enter moves the calling thread into a mount namespace of its own, rooted at root. CLONE_FS
// gives the thread a root and a working directory of its own, which the other threads of the init
// share otherwise.
func enter(root string) error {
	if err := unix.Unshare(unix.CLONE_NEWNS | unix.CLONE_FS); err != nil {
		return fmt.Errorf("create the mount namespace of the agent: %w", err)
	}
	if err := unix.Chroot(root); err != nil {
		return fmt.Errorf("enter the root of the image: %w", err)
	}
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("enter the root of the image: %w", err)
	}
	return nil
}

// InRoot runs fn in the mount namespace and the root of the agent, where a path is a path of the
// image and a symbolic link resolves inside it.
func (l *Launcher) InRoot(fn func() error) error {
	done := make(chan error, 1)
	l.calls <- func() { done <- fn() }
	return <-done
}

// Command is a process to start.
type Command struct {
	// Args is the command line; Args[0] is looked up in the PATH of Env.
	Args []string
	Env  []string
	// Dir is the working directory, a path of the image.
	Dir      string
	Identity imageuser.Identity
	// TTY gives the process a terminal, whose other side is Process.Terminal. Stdin, Stdout and
	// Stderr are its standard streams otherwise.
	TTY                   bool
	Stdin, Stdout, Stderr *os.File
}

// Process is a process a Launcher started.
type Process struct {
	Pid int
	// Terminal is the side of the terminal the process does not hold, nil without one.
	Terminal *os.File
	l        *Launcher
	exited   chan struct{}
	code     int
}

// Start starts c in the namespace of the agent. The process is the leader of a session of its
// own, and of its terminal when it has one.
func (l *Launcher) Start(c Command) (*Process, error) {
	var p *Process
	err := l.InRoot(func() error {
		var err error
		p, err = l.fork(c)
		return err
	})
	return p, err
}

func (l *Launcher) fork(c Command) (*Process, error) {
	if len(c.Args) == 0 {
		return nil, errors.New("start a process: no command")
	}
	path, err := LookPath(c.Args[0], Getenv(c.Env, "PATH"))
	if err != nil {
		return nil, err
	}
	sys := &syscall.SysProcAttr{
		Setsid:     true,
		Credential: &syscall.Credential{Uid: c.Identity.UID, Gid: c.Identity.GID, Groups: c.Identity.Groups},
	}
	var files []uintptr
	var terminal *os.File
	if c.TTY {
		t, err := openPTY(c.Identity.UID)
		if err != nil {
			return nil, err
		}
		defer func() { _ = t.process.Close() }()
		terminal = t.terminal
		files = []uintptr{t.process.Fd(), t.process.Fd(), t.process.Fd()}
		sys.Setctty = true
	} else {
		files = []uintptr{c.Stdin.Fd(), c.Stdout.Fd(), c.Stderr.Fd()}
	}

	p := &Process{Terminal: terminal, l: l, exited: make(chan struct{})}
	l.mu.Lock()
	defer l.mu.Unlock()
	//nolint:gosec // G204: the command is the one cove asked for, found in the PATH of the image.
	pid, _, err := syscall.StartProcess(path, c.Args, &syscall.ProcAttr{Dir: c.Dir, Env: c.Env, Files: files, Sys: sys})
	if err != nil {
		if terminal != nil {
			_ = terminal.Close()
		}
		return nil, fmt.Errorf("start %s: %w", c.Args[0], err)
	}
	p.Pid = pid
	l.waiting[pid] = p
	return p, nil
}

// pty is a terminal: the side a process holds, and the other.
type pty struct {
	terminal, process *os.File
}

// openPTY opens a terminal in the devpts of the image, its side for the process owned by uid.
func openPTY(uid uint32) (pty, error) {
	m, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return pty{}, fmt.Errorf("open a terminal: %w", err)
	}
	terminal := os.NewFile(uintptr(m), "/dev/ptmx")
	if err := unix.IoctlSetPointerInt(m, unix.TIOCSPTLCK, 0); err != nil {
		_ = terminal.Close()
		return pty{}, fmt.Errorf("unlock the terminal: %w", err)
	}
	n, err := unix.IoctlGetUint32(m, unix.TIOCGPTN)
	if err != nil {
		_ = terminal.Close()
		return pty{}, fmt.Errorf("name the terminal: %w", err)
	}
	name := "/dev/pts/" + strconv.FormatUint(uint64(n), 10)
	s, err := unix.Open(name, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = terminal.Close()
		return pty{}, fmt.Errorf("open %s: %w", name, err)
	}
	if err := unix.Fchown(s, int(uid), -1); err != nil {
		_ = unix.Close(s)
		_ = terminal.Close()
		return pty{}, fmt.Errorf("give %s to its user: %w", name, err)
	}
	return pty{terminal: terminal, process: os.NewFile(uintptr(s), name)}, nil
}

// reap collects every child of the init that has ended, and hands its exit code to the Process
// that waits for it. An orphan the kernel reparented to the init is collected and forgotten.
func (l *Launcher) reap() {
	for {
		l.mu.Lock()
		var ws unix.WaitStatus
		pid, err := unix.Wait4(-1, &ws, unix.WNOHANG, nil)
		if errors.Is(err, unix.EINTR) {
			l.mu.Unlock()
			continue
		}
		if err != nil || pid <= 0 {
			l.mu.Unlock()
			return
		}
		if p, ok := l.waiting[pid]; ok {
			delete(l.waiting, pid)
			p.code = exitCode(ws)
			close(p.exited)
		}
		l.mu.Unlock()
	}
}

// exitCode is the code a shell gives a process: its status, or 128 plus the signal that killed it.
func exitCode(ws unix.WaitStatus) int {
	if ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ws.ExitStatus()
}

// Wait waits for the process to end and returns its exit code, 128 plus the signal when a signal
// ended it.
func (p *Process) Wait() int {
	<-p.exited
	return p.code
}

// Signal sends sig to the process, and does nothing once it has ended.
func (p *Process) Signal(sig syscall.Signal) error {
	p.l.mu.Lock()
	defer p.l.mu.Unlock()
	select {
	case <-p.exited:
		return nil
	default:
	}
	if err := unix.Kill(p.Pid, sig); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("signal process %d: %w", p.Pid, err)
	}
	return nil
}

// StopAll sends sig to every process but the init, then SIGKILL to those still running grace
// later, and returns once none is left, or with how many a SIGKILL did not end within killWait: a
// process asleep on a disk that no longer answers outlives even SIGKILL.
func StopAll(sig syscall.Signal, grace time.Duration) error {
	// kill(-1) from PID 1 reaches every process but the init itself, those of the image whatever
	// session or namespace they made for themselves.
	if err := unix.Kill(-1, sig); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("send %v to the processes: %w", sig, err)
	}
	if n, err := endWithin(grace); err != nil || n == 0 {
		return err
	}
	if err := unix.Kill(-1, unix.SIGKILL); err != nil && !errors.Is(err, unix.ESRCH) {
		return fmt.Errorf("kill the processes: %w", err)
	}
	n, err := endWithin(killWait)
	if err == nil && n > 0 {
		err = fmt.Errorf("%d processes still running after SIGKILL", n)
	}
	return err
}

// killWait bounds the wait for the processes a SIGKILL was sent to.
const killWait = 5 * time.Second

// endWithin waits up to d for every process but the init to end, and returns how many are left.
func endWithin(d time.Duration) (int, error) {
	deadline := time.Now().Add(d)
	for {
		n, err := running()
		if err != nil || n == 0 || !time.Now().Before(deadline) {
			return n, err
		}
		time.Sleep(pollInterval)
	}
}

// pollInterval is how often the processes are counted while they end.
const pollInterval = 50 * time.Millisecond

// pfKthread is the flag of a kernel thread in /proc/<pid>/stat, PF_KTHREAD of the kernel.
const pfKthread = 0x00200000

// running returns how many processes are running, the init, the kernel threads and the zombies the
// reaper has yet to collect left out.
func running() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("list the processes: %w", err)
	}
	n := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == 1 {
			continue
		}
		stat, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			// The process ended between the listing and the read.
			continue
		}
		if counts(string(stat)) {
			n++
		}
	}
	return n, nil
}

// counts reports whether the process of stat, a /proc/<pid>/stat, is one that must end: not a
// kernel thread and not a zombie. The name, in parentheses, may hold spaces and parentheses, so
// the fields are read after the last one.
func counts(stat string) bool {
	i := strings.LastIndexByte(stat, ')')
	if i < 0 {
		return false
	}
	// After the name: state, ppid, pgrp, session, tty_nr, tpgid, flags.
	fields := strings.Fields(stat[i+1:])
	if len(fields) < 7 || fields[0] == "Z" {
		return false
	}
	flags, err := strconv.ParseUint(fields[6], 10, 64)
	return err == nil && flags&pfKthread == 0
}

// Resize gives the terminal of the process rows lines of cols columns.
func (p *Process) Resize(rows, cols uint16) error {
	if p.Terminal == nil {
		return errors.New("the process has no terminal")
	}
	ws := &unix.Winsize{Row: rows, Col: cols}
	if err := unix.IoctlSetWinsize(int(p.Terminal.Fd()), unix.TIOCSWINSZ, ws); err != nil {
		return fmt.Errorf("resize the terminal: %w", err)
	}
	return nil
}

// ParseSignal reads the StopSignal of an image, a name with or without SIG or a number, and
// returns SIGTERM when it is empty, as docker does.
func ParseSignal(s string) (syscall.Signal, error) {
	if s == "" {
		return unix.SIGTERM, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 || n > 64 {
			return 0, fmt.Errorf("image sets StopSignal %s, not a signal", s)
		}
		return syscall.Signal(n), nil
	}
	name := strings.ToUpper(s)
	if !strings.HasPrefix(name, "SIG") {
		name = "SIG" + name
	}
	if sig := unix.SignalNum(name); sig != 0 {
		return sig, nil
	}
	return 0, fmt.Errorf("image sets StopSignal %s, not a signal", s)
}
