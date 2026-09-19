// Package agent builds the command line that drives a coding agent for one turn. What is common
// to every agent is here; what is measured on one of them is in the file that bears its name, and
// a second agent adds a file rather than changing this one.
package agent

import (
	"crypto/rand"
	"fmt"
)

// Turn describes one turn of the agent inside a sandbox. Target must already be screened
// (Screen): a backend reaches any VM it is given. Thread must be set, by NewThreadID for a new
// conversation or by the caller for a resumed one.
type Turn struct {
	// Target is the sandbox to reach, by name or ID, resolved by the backend.
	Target string
	// Prompt is the turn to drive; empty attaches a terminal to the agent instead.
	Prompt string
	// Thread identifies the conversation: a UUID for a new one, a UUID or a display name for a
	// resumed one.
	Thread string
	// Resume tells whether Thread names a conversation that already exists.
	Resume bool
	// Continue picks up the last conversation of the sandbox instead of naming one; Thread is then
	// unused. Attached regime only: claude keeps no record of its driven threads for --continue, so
	// a driven turn would silently start afresh. The CLI refuses that combination before it gets
	// here.
	Continue bool
	// Name is the display name to give the thread; empty leaves it unnamed.
	Name string
}

// NewThreadID draws the identifier of a new thread: a random version 4 UUID, the shape claude's
// --session-id takes. Cove draws it instead of reading one back from the box, so that a thread has
// an identity before the agent has said anything.
func NewThreadID() string {
	var b [16]byte
	// The error of crypto/rand.Read is always nil since Go 1.24.
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40 // Version 4.
	b[8] = b[8]&0x3f | 0x80 // Variant 10.
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
