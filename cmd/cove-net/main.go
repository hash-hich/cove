// Command cove-net holds the TCP/IP stack behind the network card of one VM, and nothing else. cove
// starts it before cove-vmm, gives it the socket of the card, and it ends when the card closes: the
// VM is gone, whatever the reason. It is confined to that socket and to the network of its exit:
// the stack parses every frame the guest writes, so the process it runs in holds no file.
package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/hich-hich/cove/cmd/cove-net/internal/hostexit"
	"gitlab.com/hich-hich/cove/cmd/cove-net/internal/relay"
	"gitlab.com/hich-hich/cove/cmd/internal/confine"
	"gitlab.com/hich-hich/cove/internal/netstack/netstackproto"
	"gitlab.com/hich-hich/cove/internal/process"
)

// profile is the Seatbelt profile of cove-net: everything denied, then only what it was seen to
// need, each rule with the reason it is there.
//
//go:embed net.sb
var profile string

func main() {
	// The standard error is net.log, in the directory of the sandbox.
	logger := log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
	// The first line loads the time zone of the host, which the sandbox would refuse afterwards.
	logger.Printf("started, build %q", process.Build())
	if err := run(logger); err != nil {
		logger.Printf("ended: %v", err)
		os.Exit(1)
	}
	logger.Print("ended: the card closed")
}

// run returns nil when the card closed, and why cove-net ends otherwise.
func run(logger *log.Logger) error {
	pipe := os.NewFile(process.Pipe, "pipe")
	if pipe == nil {
		return errors.New("started without the pipe of cove")
	}
	if err := process.Send(pipe, netstackproto.Hello{Build: process.Build()}); err != nil {
		return err //nolint:wrapcheck // Send names the message.
	}
	var cfg netstackproto.Config
	if err := process.NewReceiver(pipe).Receive(&cfg); err != nil {
		return err //nolint:wrapcheck // Receive names the message.
	}
	ln, err := listen(cfg.Socket)
	if err := answer(pipe, err); err != nil {
		return err
	}
	_ = pipe.Close()
	logger.Printf("listening on %s, resolvers %v", cfg.Socket, cfg.Nameservers)
	conn, err := accept(ln, cfg.AcceptTimeout)
	if err != nil {
		return err
	}
	logger.Print("the card is connected")
	//nolint:wrapcheck // Serve names what ended the card.
	return relay.Serve(context.Background(), conn, relay.Config{
		Gateway: cfg.Gateway, Exit: hostexit.New(cfg.Gateway, cfg.Nameservers), Log: logger,
	})
}

// listen enters the directory of socket, then the sandbox, and listens on socket by its name. A Unix
// socket is reached by a path of 104 bytes at most on macOS, and the directory of a sandbox is
// longer; the sandbox still matches its whole path.
func listen(socket string) (*net.UnixListener, error) {
	if err := os.Chdir(filepath.Dir(socket)); err != nil {
		return nil, fmt.Errorf("enter the directory of the card: %w", err)
	}
	if err := confine.Enter(profile, confine.Paths{Listen: []string{socket}}); err != nil {
		return nil, err //nolint:wrapcheck // Enter names the sandbox.
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Base(socket), Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on the card: %w", err)
	}
	// The file stays once the listener is closed, and cove removes it with the sandbox: the sandbox
	// lets cove-net create it, not remove it.
	ln.SetUnlinkOnClose(false)
	return ln, nil
}

// accept returns the one connection of the card, that of cove-vmm, and closes ln. It fails when
// none came within timeout: cove ended between the start of cove-net and that of cove-vmm.
func accept(ln *net.UnixListener, timeout time.Duration) (net.Conn, error) {
	defer func() { _ = ln.Close() }()
	if err := ln.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("wait for the card: %w", err)
	}
	conn, err := ln.Accept()
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return nil, fmt.Errorf("no card connected within %s", timeout)
	} else if err != nil {
		return nil, fmt.Errorf("wait for the card: %w", err)
	}
	return conn, nil
}

// answer sends cove the Status of err, and returns err, or the failure to send it.
func answer(pipe io.Writer, err error) error {
	var s netstackproto.Status
	if err != nil {
		s.Error = err.Error()
	}
	if sendErr := process.Send(pipe, s); sendErr != nil {
		return errors.Join(err, sendErr)
	}
	return err
}
