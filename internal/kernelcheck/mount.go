package kernelcheck

import (
	"errors"
	"syscall"
)

// mountOptions maps a file system the init mounts to the option that brings it.
var mountOptions = map[string]string{
	"proc":     procFS,
	"sysfs":    "CONFIG_SYSFS",
	"devtmpfs": "CONFIG_DEVTMPFS",
	"devpts":   "CONFIG_UNIX98_PTYS",
	"tmpfs":    "CONFIG_TMPFS",
	"erofs":    "CONFIG_EROFS_FS",
	"overlay":  "CONFIG_OVERLAY_FS",
	"ext4":     "CONFIG_EXT4_FS",
}

// FromMount turns the failure of the init to mount a file system of type fstype into the option
// the kernel lacks, when the kernel does not know the type (ENODEV): the init names PROC_FS from
// its mount of /proc, before the check that reads /proc can run. Any other error, or a type the
// list does not hold, is returned as it is.
func FromMount(fstype string, err error) error {
	if opt, ok := mountOptions[fstype]; ok && errors.Is(err, syscall.ENODEV) {
		return &MissingError{Options: []string{opt}}
	}
	return err
}
