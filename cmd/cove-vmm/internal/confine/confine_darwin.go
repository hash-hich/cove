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
	_ "embed"
	"fmt"
	"strings"
	"unsafe"
)

// profile is the Seatbelt profile of cove-vmm: everything denied, then only what the monitor was
// seen to need, each rule with the reason it is there.
//
//go:embed vmm.sb
var profile string

// Enter puts the process in the Seatbelt sandbox of cove-vmm, for good: nothing takes it out. The
// monitor may then read the files of read, read and write those of write, and create the Unix
// sockets of listen and accept connections on them, each named by its resolved path: a rule
// matches the path a file is reached by, and a link to it is not that path.
func Enter(read, write, listen []string) error {
	var b strings.Builder
	_, _ = b.WriteString(profile)
	for _, path := range read {
		_, _ = fmt.Fprintf(&b, "(allow file-read-data file-read-metadata (literal %s))\n", quote(path))
	}
	for _, path := range write {
		_, _ = fmt.Fprintf(&b, "(allow file-read-data file-read-metadata file-write-data (literal %s))\n", quote(path))
	}
	for _, path := range listen {
		// libkrun looks whether the socket is there before it binds it, and binding creates its file.
		// Accepting a connection asks for nothing more.
		_, _ = fmt.Fprintf(&b, "(allow file-read-metadata file-write-create (literal %s))\n", quote(path))
		_, _ = fmt.Fprintf(&b, "(allow network-bind (local unix-socket (path-literal %s)))\n", quote(path))
	}
	p := C.CString(b.String())
	defer C.free(unsafe.Pointer(p))
	var errbuf *C.char
	if C.sandbox_init(p, 0, &errbuf) != 0 {
		defer C.sandbox_free_error(errbuf)
		return fmt.Errorf("enter the sandbox of cove-vmm: %s", C.GoString(errbuf))
	}
	return nil
}

// quote returns s as a string of the profile language.
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
