package boot

// The rules of the layout, exposed to the tests of the package, which run on any host.
var (
	WorkingDir     = workingDir
	OverlayOptions = overlayOptions
	VolumePaths    = volumePaths
	HostsFile      = hostsFile
	ResolvConf     = resolvConf
	ResolveIn      = resolveIn
	DiskOrder      = diskOrder
	MatchDisks     = matchDisks
	CheckWriteDisk = checkWriteDisk
)
