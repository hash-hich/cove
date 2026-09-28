package launch //nolint:testpackage // Exposes the internals the tests of the package reach.

// Counts reports whether a line of /proc/<pid>/stat is a process that must end.
var Counts = counts

// Session returns the session of a line of /proc/<pid>/stat.
var Session = session
