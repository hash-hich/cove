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

// listenControl listens on the vsock port of the host, and turns each Stop it reads into a stop.
// The host is the only peer: the VM has no other.
func listenControl(stops chan<- stopRequest) error {
	fd, err := unix.Socket(unix.AF_VSOCK, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open the control socket: %w", err)
	}
	if err := unix.Bind(fd, &unix.SockaddrVM{CID: unix.VMADDR_CID_ANY, Port: control.Port}); err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("bind the control socket to port %d: %w", control.Port, err)
	}
	if err := unix.Listen(fd, 1); err != nil {
		_ = unix.Close(fd)
		return fmt.Errorf("listen on the control socket: %w", err)
	}
	go func() {
		for {
			conn, _, err := unix.Accept4(fd, unix.SOCK_CLOEXEC)
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.ECONNABORTED) {
				continue
			}
			if err != nil {
				say("accept on the control socket: %v", err)
				return
			}
			go serve(os.NewFile(uintptr(conn), "control"), stops)
		}
	}()
	return nil
}

// serve reads a Stop on conn, says it was received, and hands it to the init with a report that
// writes each step back on conn. conn stays open for as long as the VM runs: its end is how the
// host sees the VM power off.
func serve(conn *os.File, stops chan<- stopRequest) {
	var stop control.Stop
	if err := control.NewReceiver(conn).Receive(&stop); err != nil {
		say("read the control socket: %v", err)
		_ = conn.Close()
		return
	}
	report := func(s control.Step) { _ = control.Send(conn, s) }
	report(control.Step{Name: control.Received})
	request(stops, stopRequest{grace: stop.Grace, report: report})
}

// shutdown stops every process of the image with sig, killing those left once the grace of req
// has passed, then flushes and closes the write disk, reporting each step to req. A step that
// fails does not stop the next: the VM is powered off after all the same.
func shutdown(sig syscall.Signal, req stopRequest) {
	step := func(name string, err error) {
		s := control.Step{Name: name}
		if err != nil {
			s.Error = err.Error()
			say("%s: %v", name, err)
		}
		req.report(s)
	}
	step(control.ProcessesEnded, launch.StopAll(sig, req.grace))
	unix.Sync()
	step(control.Synced, nil)
	// Read only on the superblock closes the journal of every mount of the disk, those the
	// namespace of the agent holds included, so the next boot finds it clean.
	err := unix.Mount("", writeMount, "", unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOATIME, "")
	if err != nil {
		err = fmt.Errorf("remount %s read only: %w", writeMount, err)
	}
	step(control.ReadOnly, err)
}
