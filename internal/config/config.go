package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

type Config struct {
	DataDir string
	Token   string
}

// DefaultDataDir mengembalikan direktori data portable: selalu <dir-exe>/data
// (di samping binary), bukan bergantung pada direktori kerja saat dijalankan.
// Lokasi lain bisa dipilih eksplisit lewat flag --data.
func DefaultDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "data"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), "data")
}

func New(dataDir string) (*Config, error) {
	if dataDir == "" {
		dataDir = DefaultDataDir()
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	// Pastikan direktori yang sudah ada juga 0700 (MKdirAll tidak mengubah mode).
	if err := os.Chmod(abs, 0o700); err != nil {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Config{DataDir: abs, Token: hex.EncodeToString(b)}, nil
}

func (c *Config) Path(name string) string { return filepath.Join(c.DataDir, name) }
