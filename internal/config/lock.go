package config

import "errors"

// ErrLocked menandakan data dir sudah dipakai instance bServer lain.
var ErrLocked = errors.New("instance lain sedang berjalan pada data dir ini")

// Lock adalah lock eksklusif pada <dataDir>/.lock. Implementasi per-platform
// ada di lock_unix.go (flock, tahan terhadap lock basi) dan lock_other.go
// (fallback O_EXCL).
type Lock struct {
	path    string
	release func()
}

// AcquireLock mengunci data dir; gagal dengan ErrLocked bila instance lain
// masih memegang lock.
func AcquireLock(dataDir string) (*Lock, error) { return acquireLock(dataDir) }

// Release melepas lock dan menghapus file lock. Aman dipanggil berulang.
func (l *Lock) Release() {
	if l == nil || l.release == nil {
		return
	}
	l.release()
	l.release = nil
}
