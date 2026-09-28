package boot

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/internal/vminit/control"
	"gitlab.com/hich-hich/cove/internal/vminit/launch"
	"gitlab.com/hich-hich/cove/internal/vminit/turn"
)

// stopRequest is a stop the init was asked for: how long the processes have to end, and where
// each step goes.
type stopRequest struct {
	grace  time.Duration
	report func(control.Step)
}

// notifySignals turns the signals a kernel or a person sends PID 1 into stops, with the grace
// docker gives, and a report that goes nowhere.
func notifySignals(stops chan<- stopRequest) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, unix.SIGTERM, unix.SIGINT, unix.SIGPWR)
	go func() {
		for range ch {
			request(stops, stopRequest{grace: launch.StopGrace, report: func(control.Step) {}})
		}
	}()
}

// request hands req to the init unless a stop is already under way: that one goes on, and the
// later caller sees the VM power off as the first does.
func request(stops chan<- stopRequest, req stopRequest) {
	select {
	case stops <- req:
	default:
	}
}

// listen listens on the vsock port of the host named name, and serves each connection in a
// goroutine of its own. The host is the only peer: the VM has no other. A connection is non
// blocking, so that the runtime polls it and closing it ends a read under way.
func listen(port uint32, name string, serve func(*os.File)) error {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open the %s socket: %w", name, err)
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: port}); err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("bind the %s socket to port %d: %w", name, port, err)
	}
	if err := unix.Listen(fd, unix.SOMAXCONN); err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("listen on the %s socket: %w", name, err)
	}
	go func() {
		for {
			conn, _, err := unix.Accept4(fd, unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK)
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.ECONNABORTED) {
				continue
			}
			if err != nil {
				say("accept on the %s socket: %v", name, err)
				return
			}
			go serve(os.NewFile(uintptr(conn), name))
		}
	}()
	return nil
}

// listenControl listens on the control port, and turns each Stop it reads into a stop and each
// Cancel into the cut of a turn of ts.
func listenControl(stops chan<- stopRequest, ts *turns) error {
	return listen(control.Port, "control", func(conn *os.File) { serveControl(conn, stops, ts) })
}

// serveControl reads a Request on conn and says it was received. A Stop is handed to the init with
// a report that writes each step back on conn, which stays open for as long as the VM runs: its
// end is how the host sees the VM power off. A Cancel is carried out, and conn closed.
func serveControl(conn *os.File, stops chan<- stopRequest, ts *turns) {
	var req control.Request
	if err := control.NewReceiver(conn).Receive(&req); err != nil {
		say("read the control socket: %v", err)
		_ = conn.Close()
		return
	}
	switch {
	case req.Stop != nil:
		report := func(s control.Step) { _ = control.Send(conn, s) }
		report(control.Step{Name: control.Received})
		request(stops, stopRequest{grace: req.Stop.Grace, report: report})
	case req.Cancel != nil:
		step := control.Step{Name: control.Received}
		if err := ts.cancel(req.Cancel.Turn, turn.Cause(req.Cancel.Cause), req.Cancel.Grace); err != nil {
			step.Error = err.Error()
		}
		_ = control.Send(conn, step)
		_ = conn.Close()
	default:
		_ = control.Send(conn, control.Step{Name: control.Received, Error: "an empty request"})
		_ = conn.Close()
	}
}

// shutdown cuts the turns of ts as a stop and stops every process of the image with sig, killing
// those left once the grace of req has passed, then flushes and closes the write disk, reporting
// each step to req. The turns and the other processes share the one grace: waiting for the turns
// first would leave the others none, and run past the time the host gives the stop. The turns are
// cut before any process gets sig so that each one reports a stop as its cause, and is waited for
// so that it sends its last frame while the VM still runs. A step that fails does not stop the
// next: the VM is powered off after all the same.
func shutdown(sig syscall.Signal, req stopRequest, ts *turns) {
	step := func(name string, err error) {
		s := control.Step{Name: name}
		if err != nil {
			s.Error = err.Error()
			say("%s: %v", name, err)
		}
		req.report(s)
	}
	ts.stopAll(req.grace)
	err := launch.StopAll(sig, req.grace)
	// Every process ended, the agents of the turns included: what is left is reading what they
	// wrote.
	ts.awaitAll(2 * drainWait)
	step(control.ProcessesEnded, err)
	unix.Sync()
	step(control.Synced, nil)
	// Read only on the superblock closes the journal of every mount of the disk, those the
	// namespace of the agent holds included, so the next boot finds it clean.
	err = unix.Mount("", writeMount, "", unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOATIME, "")
	if err != nil {
		err = fmt.Errorf("remount %s read only: %w", writeMount, err)
	}
	step(control.ReadOnly, err)
}
