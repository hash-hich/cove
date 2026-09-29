// Package netstacklaunch starts cove-net, the process that holds the TCP/IP stack behind the
// network card of one VM, and hands it the card as netstackproto describes. cove starts it before
// cove-vmm, which connects to the socket it listens on.
package netstacklaunch

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gitlab.com/hich-hich/cove/internal/netstack/netstackproto"
	"gitlab.com/hich-hich/cove/internal/process"
)

// Program is the name of cove-net in libexec.
const Program = "cove-net"

// Request is the card to serve.
type Request struct {
	// Socket is the path of the socket of the card, where cove-net listens. Nothing may be there.
	Socket string
	// Gateway is the address of cove-net on the network of the card, with its prefix.
	Gateway netip.Prefix
	// Nameservers are the resolvers of the host, in order.
	Nameservers []netip.AddrPort
	// AcceptTimeout is how long cove-net waits for cove-vmm to connect before it ends.
	AcceptTimeout time.Duration
	// Log receives what cove-net says, on its standard output and error.
	Log *os.File
	// Lock, when not nil, is the lock of the sandbox, which cove-net holds for as long as it lives,
	// as cove-vmm does: the sandbox runs until both have ended.
	Lock *os.File
}

// Net is a cove-net that listens on a card.
type Net struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

// Start starts the cove-net of dir, the libexec beside cove, and gives it req. It returns once
// cove-net is confined and listens on the card, or with the reason it could not. cove-net outlives
// cove, as the package process starts it, and ends when the card closes; one that did not listen
// is killed by Start itself.
func Start(ctx context.Context, dir string, req Request) (*Net, error) {
	socketDir, err := filepath.EvalSymlinks(filepath.Dir(req.Socket))
	if err == nil {
		socketDir, err = filepath.Abs(socketDir)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve the directory of the card: %w", err)
	}
	cfg := netstackproto.Config{
		// The sandbox of cove-net matches the path a socket is reached by, and a link in it is a path
		// it does not allow.
		Socket:  filepath.Join(socketDir, filepath.Base(req.Socket)),
		Gateway: req.Gateway, Nameservers: req.Nameservers, AcceptTimeout: req.AcceptTimeout,
	}
	pair, err := process.Socketpair()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", Program, err)
	}
	ours, theirs := pair[0], pair[1]
	defer func() { _ = ours.Close() }()
	cmd := process.Command(dir, Program, req.Log, theirs, req.Lock)
	err = cmd.Start()
	_ = theirs.Close()
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", Program, err)
	}
	n := &Net{cmd: cmd, done: make(chan struct{})}
	go func() {
		n.err = cmd.Wait()
		close(n.done)
	}()
	stop := context.AfterFunc(ctx, func() { _ = ours.Close() })
	defer stop()
	if err := n.handshake(ours, cfg); err != nil {
		n.Kill()
		// Closing the pipe is how the end of ctx stops the handshake, and what it reads then says
		// nothing of the reason.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("start %s: %w", Program, context.Cause(ctx))
		}
		return nil, err
	}
	return n, nil
}

// handshake checks who cove-net is, sends it the card, and waits for its answer.
func (n *Net) handshake(pipe io.ReadWriter, cfg netstackproto.Config) error {
	r := process.NewReceiver(pipe)
	var hello netstackproto.Hello
	if err := r.Receive(&hello); err != nil {
		return n.failed(err)
	}
	if hello.Build != process.Build() {
		return fmt.Errorf("%s comes from build %q and cove from build %q: build both with make",
			Program, hello.Build, process.Build())
	}
	if err := process.Send(pipe, cfg); err != nil {
		return n.failed(err)
	}
	var status netstackproto.Status
	if err := r.Receive(&status); err != nil {
		return n.failed(err)
	}
	if status.Error != "" {
		return fmt.Errorf("%s: %s", Program, status.Error)
	}
	return nil
}

// failed returns err, or when cove-net closed the pipe by exiting, how it exited: what it said on
// the way is in its log.
func (n *Net) failed(err error) error {
	if !errors.Is(err, io.EOF) {
		return err
	}
	<-n.done
	return fmt.Errorf("%s ended before it listened on the card: %w", Program, n.err)
}

// Process returns what names the cove-net of n for good, to be killed later by another cove.
func (n *Net) Process() (process.Process, error) {
	p, ok, err := process.Find(n.cmd.Process.Pid)
	if err == nil && !ok {
		// A process gone from the table was reaped, so how it exited is known.
		<-n.done
		err = fmt.Errorf("%s %d ended", Program, p.PID)
		if n.err != nil {
			err = fmt.Errorf("%w: %w", err, n.err)
		}
	}
	return p, err
}

// Kill ends cove-net, and the card with it, and waits for it.
func (n *Net) Kill() {
	_ = n.cmd.Process.Kill()
	<-n.done
}

// resolvConf is where macOS and Linux keep the resolvers of the host up to date.
const resolvConf = "/etc/resolv.conf"

// Nameservers returns the resolvers of the host, those of the nameserver lines of resolvConf, in
// order, on port 53; none when the file is missing. The resolvers a VPN sets for its domains alone
// are not there, and are not seen.
func Nameservers() ([]netip.AddrPort, error) {
	f, err := os.Open(resolvConf)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the resolvers of the host: %w", err)
	}
	defer func() { _ = f.Close() }()
	return parseResolvConf(f)
}

// dnsPort is the port of the DNS.
const dnsPort = 53

// parseResolvConf returns the resolvers of the nameserver lines of r, in order. A line whose address
// does not read is skipped: the file is the host's, and one bad line leaves the others.
func parseResolvConf(r io.Reader) ([]netip.AddrPort, error) {
	var ns []netip.AddrPort
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		if a, err := netip.ParseAddr(fields[1]); err == nil {
			ns = append(ns, netip.AddrPortFrom(a, dnsPort))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read the resolvers of the host: %w", err)
	}
	return ns, nil
}
