//go:build !windows

package workspace

import (
	"errors"
	"os"
	"syscall"
)

// tryLockFile takes an exclusive flock on file without blocking. It reports
// false, with no error, when another open file holds the lock.
//
// flock rather than fcntl record locks, because an fcntl lock belongs to the
// process: two goroutines in one process would both be granted it, and closing
// any descriptor for the file would drop it. A flock belongs to the open file,
// so each acquisition excludes every other, in this process or another.
func tryLockFile(file *os.File) (bool, error) {
	var lockErr error
	err := control(file, func(fd uintptr) {
		for {
			lockErr = syscall.Flock(int(fd), syscall.LOCK_EX|syscall.LOCK_NB)
			if !errors.Is(lockErr, syscall.EINTR) {
				return
			}
		}
	})
	if err != nil {
		return false, err
	}
	if errors.Is(lockErr, syscall.EWOULDBLOCK) {
		return false, nil
	}
	return lockErr == nil, lockErr
}

// unlockFile releases the flock held on file.
func unlockFile(file *os.File) error {
	var unlockErr error
	err := control(file, func(fd uintptr) {
		unlockErr = syscall.Flock(int(fd), syscall.LOCK_UN)
	})
	return errors.Join(err, unlockErr)
}

// control runs fn with file's descriptor. SyscallConn rather than Fd, because
// Fd switches the descriptor to blocking mode as a side effect.
func control(file *os.File, fn func(fd uintptr)) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	return conn.Control(fn)
}
