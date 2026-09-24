package kernelcheck

// CheckFor and CheckFileFor expose the check of one architecture to the tests of the package, so
// that both are exercised whatever the host.
var (
	CheckFor     = checkArch
	CheckFileFor = checkFile
)

// Requirements returns the requirements of goarch, each as its first option: a configuration that
// builds in all of them meets the list.
func Requirements(goarch string) []string {
	reqs := requirementsOf(goarch)
	opts := make([]string, 0, len(reqs))
	for _, req := range reqs {
		opts = append(opts, req[0])
	}
	return opts
}
