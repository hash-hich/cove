package image

import (
	"fmt"
	"io"
	"slices"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// redrawEvery bounds how often the board redraws: a terminal redrawn on every chunk would spend
// more time drawing than downloading.
const redrawEvery = 100 * time.Millisecond

// shortDigest is how much of a digest names a layer on screen: the twelve hex characters docker
// prints. The whole digest would wrap the line on a narrow terminal, and a wrapped line makes
// the redraw count the lines of the board wrong.
const shortDigest = 12

// short names the layer h on screen.
func short(h v1.Hash) string {
	if len(h.Hex) < shortDigest {
		return h.Hex
	}
	return h.Hex[:shortDigest]
}

// gauge is the line of one download: what it brings and how much of it is in.
type gauge struct {
	label string
	total int64
	done  int64
}

// text is the line a gauge draws.
func (g *gauge) text() string {
	return fmt.Sprintf("%s: %s / %s", g.label, formatSize(g.done), formatSize(g.total))
}

// board draws the downloads in flight, one line each as docker does, in a block it rewrites in
// place under what has already been written; the cursor rests on the line below the block. A
// download that ends takes its line out of the block and leaves a line of facts above it. Off a
// terminal nothing is drawn and the lines of facts alone come out, so that a journal or a pipe
// stays readable.
//
// A board is not safe for two goroutines at once: its caller holds a lock.
type board struct {
	// draw is where the block is rewritten: nil off a terminal, and nil when the pull keeps quiet.
	draw io.Writer
	// log is where the lines of facts go, nil when the pull keeps quiet.
	log  io.Writer
	live []*gauge
	last time.Time
}

// board returns the board of a pull, drawing on Log when it is a terminal.
func (p *Puller) board() *board {
	b := &board{log: p.Log}
	if p.Terminal {
		b.draw = p.Log
	}
	return b
}

// start puts the line of a download of total bytes, named label, at the bottom of the block.
func (b *board) start(label string, total int64) *gauge {
	g := &gauge{label: label, total: total}
	b.live = append(b.live, g)
	if b.draw != nil {
		_, _ = fmt.Fprintf(b.draw, "%s\n", g.text())
		b.last = time.Now()
	}
	return g
}

// advance counts n more bytes of g and rewrites the block.
func (b *board) advance(g *gauge, n int64) {
	g.done += n
	if b.draw == nil || time.Since(b.last) < redrawEvery {
		return
	}
	b.erase()
	b.show()
}

// finish takes the line of g out of the block and writes the facts of that download where the
// block began: what is still coming down keeps its lines under it.
func (b *board) finish(g *gauge, format string, args ...any) {
	b.erase()
	b.live = slices.DeleteFunc(b.live, func(other *gauge) bool { return other == g })
	b.logf(format, args...)
	b.show()
}

// drop takes the line of g out of the block, saying nothing: the download failed, and the error
// of the pull says what happened.
func (b *board) drop(g *gauge) {
	b.erase()
	b.live = slices.DeleteFunc(b.live, func(other *gauge) bool { return other == g })
	b.show()
}

// say writes a line of facts above the block.
func (b *board) say(format string, args ...any) {
	b.erase()
	b.logf(format, args...)
	b.show()
}

// clear takes the block off the screen: nothing is coming down any more.
func (b *board) clear() {
	b.erase()
	b.live = nil
}

// erase removes the block from the screen and puts the cursor back where its first line was.
func (b *board) erase() {
	if b.draw == nil || len(b.live) == 0 {
		return
	}
	_, _ = fmt.Fprintf(b.draw, "\x1b[%dA\r\x1b[J", len(b.live))
}

// show draws the block under the cursor, which it leaves on the line below it.
func (b *board) show() {
	if b.draw == nil {
		return
	}
	for _, g := range b.live {
		_, _ = fmt.Fprintf(b.draw, "%s\n", g.text())
	}
	b.last = time.Now()
}

// logf writes one line of facts.
func (b *board) logf(format string, args ...any) {
	if b.log != nil {
		_, _ = fmt.Fprintf(b.log, format+"\n", args...)
	}
}

// formatSize writes n bytes for a human, in the unit that fits: 12.3 MB, 456 kB, 78 B.
func formatSize(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, exp := float64(n), 0
	for value >= unit && exp < 4 {
		value /= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", value, "kMGT"[exp-1])
}
