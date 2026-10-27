package lockfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// child mode for TestLockDetectsOwnBinary: stay alive as a second copy of this binary
	if os.Getenv("LOCKFILE_TEST_SLEEP") != "" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestLockIgnoresForeignProcess(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".lockfile")
	// the parent process (go tool) is running, but it is not our binary
	if err := os.WriteFile(file, []byte(strconv.Itoa(os.Getppid())), 0666); err != nil {
		t.Fatal(err)
	}

	lock, _ := New(file)
	pid, err := lock.Lock()
	if err != nil || pid != os.Getpid() {
		t.Fatalf("expected stale lock to be taken over, got pid %d, err %v", pid, err)
	}
	if got, _ := getPid(file); got != os.Getpid() {
		t.Fatalf("expected lockfile to hold own pid %d, got %d", os.Getpid(), got)
	}
}

func TestLockDetectsOwnBinary(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "LOCKFILE_TEST_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	file := filepath.Join(t.TempDir(), ".lockfile")
	if err := os.WriteFile(file, []byte(strconv.Itoa(cmd.Process.Pid)), 0666); err != nil {
		t.Fatal(err)
	}

	lock, _ := New(file)
	if pid, err := lock.Lock(); err != ErrLocked || pid != cmd.Process.Pid {
		t.Fatalf("expected ErrLocked for pid %d, got pid %d, err %v", cmd.Process.Pid, pid, err)
	}
}
