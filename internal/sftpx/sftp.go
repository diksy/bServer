package sftpx

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"

	"bserver/internal/sshx"
	"bserver/internal/store"
)

type Entry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime string `json:"modTime"`
	IsDir   bool   `json:"isDir"`
}

type Lister struct {
	Dialer *sshx.Dialer
	Store  *store.Store
}

func (l *Lister) open(ctx context.Context, serverID string) (*sftp.Client, func(), error) {
	sv, err := l.Store.GetServer(serverID)
	if err != nil {
		return nil, nil, err
	}
	conn, err := l.Dialer.Dial(ctx, sv)
	if err != nil {
		return nil, nil, err
	}
	cl, err := sftp.NewClient(conn.Client)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return cl, func() { cl.Close(); conn.Close() }, nil
}

func (l *Lister) List(ctx context.Context, serverID, dir string) (string, []Entry, error) {
	cl, closeFn, err := l.open(ctx, serverID)
	if err != nil {
		return "", nil, err
	}
	defer closeFn()
	if dir == "" {
		dir = "."
	}
	abs, err := cl.RealPath(dir)
	if err != nil {
		abs = dir
	}
	items, err := cl.ReadDir(abs)
	if err != nil {
		return abs, nil, err
	}
	out := make([]Entry, 0, len(items))
	for _, it := range items {
		out = append(out, Entry{
			Name:    it.Name(),
			Size:    it.Size(),
			Mode:    it.Mode().String(),
			ModTime: it.ModTime().UTC().Format(time.RFC3339),
			IsDir:   it.IsDir(),
		})
	}
	return abs, out, nil
}

func (l *Lister) Mkdir(ctx context.Context, serverID, dir string) error {
	cl, closeFn, err := l.open(ctx, serverID)
	if err != nil {
		return err
	}
	defer closeFn()
	return cl.MkdirAll(dir)
}

func (l *Lister) Upload(ctx context.Context, serverID, remotePath string, src io.Reader) (int64, error) {
	cl, closeFn, err := l.open(ctx, serverID)
	if err != nil {
		return 0, err
	}
	defer closeFn()
	if err := cl.MkdirAll(path.Dir(remotePath)); err != nil {
		// abaikan: folder mungkin sudah ada / tanpa izin
		_ = err
	}
	f, err := cl.Create(remotePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(f, src)
}

func (l *Lister) Download(ctx context.Context, serverID, remotePath string, dst io.Writer) (int64, error) {
	cl, closeFn, err := l.open(ctx, serverID)
	if err != nil {
		return 0, err
	}
	defer closeFn()
	f, err := cl.Open(remotePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(dst, f)
}

// Read membaca isi file remote untuk editor. Bila file lebih besar dari maxBytes,
// kembalikan error agar klien memakai jalur Download (streaming).
func (l *Lister) Read(ctx context.Context, serverID, remotePath string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	cl, closeFn, err := l.open(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	f, err := cl.Open(remotePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file terlalu besar untuk diedit (maks %d MB), gunakan Download", maxBytes/(1<<20))
	}
	return data, nil
}

// Write menimpa isi file remote dengan content (dibuat bila belum ada).
func (l *Lister) Write(ctx context.Context, serverID, remotePath string, content []byte) (int64, error) {
	cl, closeFn, err := l.open(ctx, serverID)
	if err != nil {
		return 0, err
	}
	defer closeFn()
	if err := cl.MkdirAll(path.Dir(remotePath)); err != nil {
		// abaikan: folder mungkin sudah ada / tanpa izin
		_ = err
	}
	f, err := cl.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var written int64
	for written < int64(len(content)) {
		n, err := f.Write(content[written:])
		written += int64(n)
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, f.Close()
}

// Stat mengembalikan ukuran file remote (untuk header Content-Length).
func (l *Lister) Stat(ctx context.Context, serverID, remotePath string) (os.FileInfo, error) {
	cl, closeFn, err := l.open(ctx, serverID)
	if err != nil {
		return nil, err
	}
	defer closeFn()
	return cl.Stat(remotePath)
}
