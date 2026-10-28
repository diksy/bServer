//go:build unix

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// acquireLock memakai flock(LOCK_EX|LOCK_NB). Lock otomatis dilepas kernel
// saat proses berhenti, jadi lock basi (crash) tidak memblokir instance baru.
func acquireLock(dataDir string) (*Lock, error) {
	p := filepath.Join(dataDir, ".lock")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (%s)", ErrLocked, p)
		}
		return nil, err
	}
	// Isi PID hanya untuk diagnosa.
	if err := f.Truncate(0); err == nil {
		_, _ = f.Seek(0, 0)
		_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	}
	return &Lock{path: p, release: func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		_ = os.Remove(p)
	}}, nil
}
