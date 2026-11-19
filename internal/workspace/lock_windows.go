//go:build windows

package workspace

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// The standard library's syscall package does not wrap LockFileEx, and the
// dependency count is a security property, so the two calls are made directly.
// kernel32 is a KnownDLL: the loader takes it from the system directory and
// never from a path an attacker could plant a copy in.
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	// errorLockViolation is ERROR_LOCK_VIOLATION, what LockFileEx answers when
	// another handle holds the range and LOCKFILE_FAIL_IMMEDIATELY is set.
	errorLockViolation syscall.Errno = 33
)

// tryLockFile takes an exclusive lock on the first byte of file without
// blocking. It reports false, with no error, when another handle holds it.
//
// A Windows byte-range lock belongs to the handle, so each acquisition
// excludes every other, in this process or another. The range may lie past
// the end of the file, which is why an empty lock file is enough.
func tryLockFile(file *os.File) (bool, error) {
	var callErr error
	ok := false
	err := control(file, func(fd uintptr) {
		var overlapped syscall.Overlapped
		r, _, e := procLockFileEx.Call(fd,
			lockfileExclusiveLock|lockfileFailImmediately,
			0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
		ok, callErr = r != 0, e
	})
	if err != nil {
		return false, err
	}
	if ok {
		return true, nil
	}
	if errors.Is(callErr, errorLockViolation) {
		return false, nil
	}
	return false, callErr
}

// unlockFile releases the lock tryLockFile took on file.
func unlockFile(file *os.File) error {
	var callErr error
	err := control(file, func(fd uintptr) {
		var overlapped syscall.Overlapped
		r, _, e := procUnlockFileEx.Call(fd, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
		if r == 0 {
			callErr = e
		}
	})
	return errors.Join(err, callErr)
}

// control runs fn with file's handle.
func control(file *os.File, fn func(fd uintptr)) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	return conn.Control(fn)
}
