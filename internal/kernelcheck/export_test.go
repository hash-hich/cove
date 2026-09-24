package kernelcheck

// CheckFor, CheckRunningAt and CheckKernelFileFor expose the checks of one architecture to the
// tests of the package, so that both are exercised whatever the host.
var (
	CheckFor           = checkArch
	CheckRunningAt     = checkRunningAt
	CheckKernelFileFor = checkImage
)

// Requirements returns what a kernel booted with cmdline on goarch needs.
func Requirements(cmdline, goarch string) []string {
	reqs, err := requirementsOf(cmdline, goarch)
	if err != nil {
		panic(err)
	}
	return reqs
}

// EveryOption returns every option the list can require, whatever the command line and the
// architecture.
func EveryOption() map[string]bool {
	opts := map[string]bool{"CONFIG_VIRTIO_MMIO_CMDLINE_DEVICES": true}
	for _, o := range required {
		opts[o] = true
	}
	for _, c := range consoleOptions {
		for _, o := range c.options {
			opts[o] = true
		}
	}
	return opts
}
