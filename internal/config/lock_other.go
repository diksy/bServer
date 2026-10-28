//go:build !unix

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// acquireLock fallback non-unix: file .lock dibuat dengan O_EXCL. Lock basi
// (proses crash) harus dihapus manual.
func acquireLock(dataDir string) (*Lock, error) {
	p := filepath.Join(dataDir, ".lock")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w (%s)", ErrLocked, p)
		}
		return nil, err
	}
	fmt.Fprintf(f, "%d\n", os.Getpid())
	f.Close()
	return &Lock{path: p, release: func() { _ = os.Remove(p) }}, nil
}
