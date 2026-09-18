package image

import (
	"fmt"
	"io"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// redrawEvery bounds how often a meter redraws: a terminal redrawn on every chunk would spend
// more time drawing than downloading.
const redrawEvery = 100 * time.Millisecond

// meter draws the progress of one download on a terminal, in place, and erases it once the
// download is done so that nothing of it stays on the screen. Off a terminal it draws nothing.
type meter struct {
	w     io.Writer
	label string
	total int64
	done  int64
	last  time.Time
}

// meter returns the meter of the blob digest of size bytes, drawing on Log when it is a terminal.
func (p *Puller) meter(digest v1.Hash, size int64) *meter {
	m := &meter{label: digest.String(), total: size}
	if p.Terminal && p.Log != nil {
		m.w = p.Log
	}
	return m
}

// advance counts n more bytes and redraws.
func (m *meter) advance(n int64) {
	m.done += n
	if m.w == nil || time.Since(m.last) < redrawEvery {
		return
	}
	m.last = time.Now()
	_, _ = fmt.Fprintf(m.w, "\r%s: %s / %s", m.label, formatSize(m.done), formatSize(m.total))
}

// clear erases the line of the meter.
func (m *meter) clear() {
	if m.w == nil || m.last.IsZero() {
		return
	}
	_, _ = fmt.Fprint(m.w, "\r\x1b[K")
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
