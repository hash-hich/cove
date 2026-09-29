// Package confine takes from a process cove starts from libexec every right it does not need, so
// that a guest that escapes into it lands in a process that can reach nothing of the user: no file
// but those it is given, no program to start. Enter is called once, before the process reads
// anything the guest wrote, with the profile of the binary and the files it is given. It lives
// under cmd, the only binaries it means something in.
package confine

// Paths are the files a confined process may reach, each by its resolved path: a rule matches the
// path a file is reached by, and a link to it is not that path.
type Paths struct {
	// Read are the files the process reads, Write those it writes as well, Listen the Unix sockets
	// it creates and accepts connections on, Connect those it connects to.
	Read, Write, Listen, Connect []string
}
