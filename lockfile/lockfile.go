package ignore lockfile

import (
	"errors"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
)

var (
	// ErrLocked ...
	ErrLocked = errors.New("already locked by another process")
	// ErrAlreadyLocked ...
	ErrAlreadyLocked = errors.New("the lockfile is already locked")
	// ErrAlreadyUnlocked ...
	ErrAlreadyUnlocked = errors.New("already unlocked")
	// ErrInvalidPID ...
	ErrInvalidPID = errors.New("invalid pid")
)

// LockFile ...
type LockFile struct {
	File   string
	locked bool
}

// New Creates a new lockfile.
func New(file string) (*LockFile, error) {
	return &LockFile{File: file}, nil
}

// Lock attempts to lock the lockfile. If the lockfile was already locked by this process,
// it returns ErrAlreadyLocked.
// Otherwise it typically returns ErrLocked if already locked by another process, and nil if not locked.
func (lf *LockFile) Lock() (int, error) {
	ownPID := os.Getpid()
	if lf.locked {
		return ownPID, ErrAlreadyLocked
	}

	file, err := os.Open(lf.File)
	defer file.Close()

	if err != nil { // If we get an error we handle it
		if !os.IsNotExist(err) { // File not found errors mean the file is unlocked, so we only fail with err if it's not a file not found error.
			return 0, err
		}
	} else { // We read the file successfully, so we check the PID inside it.
		pid, err := getPid(lf.File)
		if err != nil {
			return pid, ErrInvalidPID
		}

		running, err := isRunning(pid)
		if err != nil {
			return pid, err
		}

		if running {
			return pid, ErrLocked
		}
	}

	ioutil.WriteFile(lf.File, []byte(strconv.Itoa(ownPID)), 0644) // The file's not locked, so we lock it with our PID.
	lf.locked = true

	return ownPID, nil
}

// Unlock LockFile.Unlock unlocks the lockfile. If the lockfile was not locked it returns ErrAlreadyUnlocked.
// Unlock will delete the lockfile if it is not already unlocked.
func (lf *LockFile) Unlock() error {
	if !lf.locked {
		return ErrAlreadyUnlocked
	}

	lf.locked = false
	return os.Remove(lf.File)
}

func getPid(file string) (int, error) {
	pid, err := ioutil.ReadFile(file)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(pid)))
}
