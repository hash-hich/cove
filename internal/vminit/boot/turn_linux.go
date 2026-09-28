package boot

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"gitlab.com/hich-hich/cove/internal/vminit/launch"
	"gitlab.com/hich-hich/cove/internal/vminit/turn"
)

// drainWait bounds the reading of what the agent wrote once it ended: a process it left behind
// may hold its streams open for as long as it lives.
const drainWait = time.Second

// chunk is the most a Stdout or Stderr frame carries.
const chunk = 32 << 10

// turns are the turns the init runs, by ID, for a Cancel or a stop to reach them.
type turns struct {
	l *launch.Launcher
	// base is the process a turn starts from, less its command line, and ttyEnv its environment
	// on a terminal.
	base   launch.Command
	ttyEnv []string

	mu   sync.Mutex
	live map[string]*session
	// stopping refuses the turns that come once a stop began.
	stopping bool
}

func newTurns(l *launch.Launcher, base launch.Command, ttyEnv []string) *turns {
	return &turns{l: l, base: base, ttyEnv: ttyEnv, live: make(map[string]*session)}
}

// session is a turn that started.
type session struct {
	req turn.Request
	p   *launch.Process
	// stdin is what the agent reads: the write end of its pipe, or its terminal.
	stdin io.WriteCloser
	// done is closed once the last frame of the turn was sent, or could not be.
	done chan struct{}

	mu    sync.Mutex
	cause turn.Cause
	cut   bool
}

// serve runs the turn conn asks for, and relays its streams until it ends. The last frame sent,
// it closes conn.
func (t *turns) serve(conn *os.File) {
	defer func() { _ = conn.Close() }()
	k, p, err := turn.Read(conn)
	if err == nil && k != turn.Exec {
		err = fmt.Errorf("frame %d where a request was due", k)
	}
	var req turn.Request
	if err == nil {
		err = turn.Decode(p, &req)
	}
	if err != nil {
		say("read a turn: %v", err)
		return
	}
	w := &frames{conn: conn}
	s, streams, err := t.start(req)
	if err != nil {
		w.write(turn.Failed, turn.FailedPayload(execError(err)))
		return
	}
	defer t.forget(req.ID)
	defer close(s.done)
	w.write(turn.Started, turn.PutUint32(uint32(s.p.Pid))) //nolint:gosec // G115: a pid is positive.
	go s.readHost(conn)
	s.relay(w, streams)
	w.writeJSON(turn.Exited, s.exit())
}

// start starts the agent of req and returns its session, with what it writes: its terminal, or the
// read ends of its stdout and stderr.
func (t *turns) start(req turn.Request) (*session, []outStream, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopping {
		return nil, nil, errors.New("the VM is stopping")
	}
	if _, ok := t.live[req.ID]; ok || req.ID == "" {
		return nil, nil, fmt.Errorf("turn ID %q is empty or in use", req.ID)
	}
	cmd := t.base
	cmd.Args, cmd.TTY, cmd.Rows, cmd.Cols = req.Args, req.TTY, req.Size.Rows, req.Size.Cols
	s := &session{req: req, done: make(chan struct{})}
	var streams []outStream
	if req.TTY {
		cmd.Env = t.ttyEnv
	} else {
		ends, err := openPipes()
		if err != nil {
			return nil, nil, err
		}
		// The ends of the agent are its own once it started, or of no one if it did not.
		defer ends.closeChild()
		cmd.Stdin, cmd.Stdout, cmd.Stderr = ends.child[0], ends.child[1], ends.child[2]
		s.stdin = ends.parent[0]
		streams = []outStream{{turn.Stdout, ends.parent[1]}, {turn.Stderr, ends.parent[2]}}
	}
	p, err := t.l.Start(cmd)
	if err != nil {
		for _, o := range streams {
			_ = o.f.Close()
		}
		if s.stdin != nil {
			_ = s.stdin.Close()
		}
		return nil, nil, err //nolint:wrapcheck // Start names the command; execError reads it.
	}
	s.p = p
	if req.TTY {
		s.stdin = p.Terminal
		streams = []outStream{{turn.Stdout, p.Terminal}}
	}
	t.live[req.ID] = s
	return s, streams, nil
}

// forget drops the turn id, once it ended.
func (t *turns) forget(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.live, id)
}

// cancel cuts the turn id for cause, its processes given grace.
func (t *turns) cancel(id string, cause turn.Cause, grace time.Duration) error {
	if cause != turn.CauseCancel && cause != turn.CauseTimeout {
		return fmt.Errorf("a turn is not cancelled for %q", cause)
	}
	t.mu.Lock()
	s, ok := t.live[id]
	t.mu.Unlock()
	if !ok {
		return fmt.Errorf("no turn %q runs", id)
	}
	s.hangUp(cause, unix.SIGINT, grace)
	return nil
}

// stopAll refuses the turns to come and cuts those that run as a stop, their processes given
// grace. It returns at once: the grace runs alongside the one of the other processes of the image.
func (t *turns) stopAll(grace time.Duration) {
	t.mu.Lock()
	t.stopping = true
	live := t.sessions()
	t.mu.Unlock()
	for _, s := range live {
		s.hangUp(turn.CauseStop, unix.SIGINT, grace)
	}
}

// awaitAll returns once each turn that runs sent its last frame, or once d has passed.
func (t *turns) awaitAll(d time.Duration) {
	t.mu.Lock()
	live := t.sessions()
	t.mu.Unlock()
	deadline := time.After(d)
	for _, s := range live {
		select {
		case <-s.done:
		case <-deadline:
			return
		}
	}
}

// sessions returns the turns that run; t.mu is held.
func (t *turns) sessions() []*session {
	live := make([]*session, 0, len(t.live))
	for _, s := range t.live {
		live = append(live, s)
	}
	return live
}

// hangUp hangs the agent up as a terminal that closes hangs up a shell: its terminal is closed
// when it has one, and its process group sent sig when it has pipes. Its whole session is killed
// once the agent ended or grace passed, whichever comes first. cause, when set, is what Exited
// reports; the first cut sets it, and is the only one carried out.
func (s *session) hangUp(cause turn.Cause, sig syscall.Signal, grace time.Duration) {
	s.mu.Lock()
	if s.cause == "" {
		s.cause = cause
	}
	already := s.cut
	s.cut = true
	s.mu.Unlock()
	if already {
		return
	}
	if s.req.TTY {
		_ = s.p.Terminal.Close()
	} else {
		_ = unix.Kill(-s.p.Pid, sig)
	}
	go func() {
		select {
		case <-s.p.Done():
		case <-time.After(grace):
		}
		if err := launch.KillSession(s.p.Pid); err != nil {
			say("kill the session of turn %s: %v", s.req.ID, err)
		}
	}()
}

// readHost hands the agent what the host sends on conn, until conn ends. A connection lost while
// the agent runs hangs it up, as the terminal of the host closed.
func (s *session) readHost(conn io.Reader) {
	for {
		k, p, err := turn.Read(conn)
		if err != nil {
			select {
			case <-s.p.Done():
			default:
				s.hangUp("", unix.SIGHUP, s.req.Grace)
			}
			return
		}
		s.handle(k, p)
	}
}

// handle hands the agent the frame of kind k with payload p, a frame the host sends.
func (s *session) handle(k turn.Kind, p []byte) {
	//exhaustive:ignore // The frames the init sends never come from the host.
	switch k {
	case turn.Stdin:
		_, _ = s.stdin.Write(p)
	case turn.StdinEOF:
		// A terminal carries an end of file as a byte its line discipline reads; the agent on one
		// reads until it quits.
		if !s.req.TTY {
			_ = s.stdin.Close()
		}
	case turn.Resize:
		if size, err := turn.ParseSize(p); err == nil && s.req.TTY {
			_ = s.p.Resize(size.Rows, size.Cols)
		}
	default:
		say("turn %s: frame %d from the host, ignored", s.req.ID, k)
	}
}

// relay sends what the agent writes on streams to w until the agent ended and its streams were
// read to their end, or drainWait after it ended.
func (s *session) relay(w *frames, streams []outStream) {
	var wg sync.WaitGroup
	for _, o := range streams {
		wg.Go(func() {
			buf := make([]byte, chunk)
			for {
				n, err := o.f.Read(buf)
				if n > 0 {
					w.write(o.kind, buf[:n])
				}
				if err != nil {
					return
				}
			}
		})
	}
	copied := make(chan struct{})
	go func() {
		wg.Wait()
		close(copied)
	}()
	<-s.p.Done()
	select {
	case <-copied:
	case <-time.After(drainWait):
	}
	// Closing ends the reads still under way: the files are polled by the runtime.
	for _, o := range streams {
		_ = o.f.Close()
	}
	if !s.req.TTY {
		_ = s.stdin.Close()
	}
	<-copied
}

// exit returns the Exited of the agent, once it ended.
func (s *session) exit() turn.Exit {
	code, sig := s.p.Status()
	s.mu.Lock()
	e := turn.Exit{Cause: s.cause}
	s.mu.Unlock()
	if e.Cause == "" {
		e.Cause = turn.CauseAgent
	}
	if sig != 0 {
		e.Signal = int(sig)
	} else {
		e.Code = &code
	}
	return e
}

// execError returns what the host is told of err, why the agent of a turn did not start: the
// errno of execve or of the lookup of the program, 0 when neither failed.
func execError(err error) *turn.ExecError {
	e := &turn.ExecError{Message: err.Error()}
	var errno syscall.Errno
	switch {
	case errors.As(err, new(*launch.NotFoundError)):
		e.Errno = turn.ErrnoNotFound
	case errors.As(err, &errno):
		e.Errno = uint32(errno)
	}
	return e
}

// outStream is a stream the agent writes, and the kind of the frames that carry it.
type outStream struct {
	kind turn.Kind
	f    *os.File
}

// pipes are the three pipes of an agent without a terminal: its ends, stdin stdout and stderr in
// that order, and those of the init.
type pipes struct {
	child, parent [3]*os.File
}

func openPipes() (pipes, error) {
	var p pipes
	for i := range 3 {
		r, w, err := os.Pipe()
		if err != nil {
			p.closeChild()
			for _, f := range p.parent {
				if f != nil {
					_ = f.Close()
				}
			}
			return p, fmt.Errorf("open the pipes of the agent: %w", err)
		}
		if i == 0 {
			p.child[i], p.parent[i] = r, w
		} else {
			p.child[i], p.parent[i] = w, r
		}
	}
	return p, nil
}

func (p pipes) closeChild() {
	for _, f := range p.child {
		if f != nil {
			_ = f.Close()
		}
	}
}

// frames writes the frames of a turn on its connection, one at a time. Once a write failed, the
// host is gone: the frames that follow are dropped, and the streams still read so that the agent
// never blocks on them before it is hung up.
type frames struct {
	conn   io.Writer
	mu     sync.Mutex
	broken bool
}

func (w *frames) write(k turn.Kind, p []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.broken {
		return
	}
	if err := turn.Write(w.conn, k, p); err != nil {
		w.broken = true
	}
}

func (w *frames) writeJSON(k turn.Kind, v any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.broken {
		return
	}
	if err := turn.WriteJSON(w.conn, k, v); err != nil {
		w.broken = true
	}
}
