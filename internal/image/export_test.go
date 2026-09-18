package image

// TryLock exposes tryLock to the tests of the package.
var TryLock = tryLock

// Lock exposes lock to the tests of the package.
var Lock = lock

// FileLock exposes fileLock to the tests of the package.
type FileLock = fileLock

// Unlock exposes the unlock method of a lock to the tests of the package.
func Unlock(l *FileLock) { l.unlock() }

// LockPoll exposes lockPoll to the tests of the package.
const LockPoll = lockPoll

// FormatSize exposes formatSize to the tests of the package.
var FormatSize = formatSize
