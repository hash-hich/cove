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
	"unsafe"
)

// profile is the Seatbelt profile of cove-vmm: everything denied, then only what the monitor was
// seen to need, each rule with the reason it is there.
//
//go:embed vmm.sb
var profile string

// Enter puts the process in the Seatbelt sandbox of cove-vmm, for good: nothing takes it out.
func Enter() error {
	p := C.CString(profile)
	defer C.free(unsafe.Pointer(p))
	var errbuf *C.char
	if C.sandbox_init(p, 0, &errbuf) != 0 {
		defer C.sandbox_free_error(errbuf)
		return fmt.Errorf("enter the sandbox of cove-vmm: %s", C.GoString(errbuf))
	}
	return nil
}
