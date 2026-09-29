//go:build cgo

package confine

/*
#include <stdint.h>
#include <stdlib.h>

// Declared here rather than taken from sandbox.h, which marks them deprecated: flags zero compiles
// the profile given as text, what sandbox-exec -p does.
int sandbox_init(const char *profile, uint64_t flags, char **errorbuf);
void sandbox_free_error(char *errorbuf);
*/
import "C"

import (
	"fmt"
	"strings"
	"unsafe"
)

// Enter puts the process in a Seatbelt sandbox, for good: nothing takes it out. profile is the
// Seatbelt profile of the binary, everything denied then what it was seen to need, and one rule per
// file of paths is appended to it. The process may then read the files of Read, read and write
// those of Write, create the Unix sockets of Listen and accept connections on them, and connect to
// those of Connect.
func Enter(profile string, paths Paths) error {
	var b strings.Builder
	_, _ = b.WriteString(profile)
	for _, path := range paths.Read {
		_, _ = fmt.Fprintf(&b, "(allow file-read-data file-read-metadata (literal %s))\n", quote(path))
	}
	for _, path := range paths.Write {
		_, _ = fmt.Fprintf(&b, "(allow file-read-data file-read-metadata file-write-data (literal %s))\n", quote(path))
	}
	for _, path := range paths.Listen {
		// libkrun looks whether the socket is there before it binds it, and binding creates its file.
		// Accepting a connection asks for nothing more.
		_, _ = fmt.Fprintf(&b, "(allow file-read-metadata file-write-create (literal %s))\n", quote(path))
		_, _ = fmt.Fprintf(&b, "(allow network-bind (local unix-socket (path-literal %s)))\n", quote(path))
	}
	for _, path := range paths.Connect {
		_, _ = fmt.Fprintf(&b, "(allow network-outbound (remote unix-socket (path-literal %s)))\n", quote(path))
	}
	p := C.CString(b.String())
	defer C.free(unsafe.Pointer(p))
	var errbuf *C.char
	if C.sandbox_init(p, 0, &errbuf) != 0 {
		defer C.sandbox_free_error(errbuf)
		return fmt.Errorf("enter the sandbox: %s", C.GoString(errbuf))
	}
	return nil
}

// quote returns s as a string of the profile language.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
