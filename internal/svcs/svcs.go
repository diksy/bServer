// Package svcs membaca status service systemd dan container Docker pada
// server remote melalui SSH (satu koneksi per perintah).
package svcs

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"

	"bserver/internal/sshx"
	"bserver/internal/store"
)

// maxLines membatasi jumlah baris yang diproses agar respons tetap ringan
// pada server dengan ratusan unit/container.
const maxLines = 300

// Service adalah satu unit systemd hasil `systemctl list-units`.
type Service struct {
	Name        string `json:"name"`
	Active      string `json:"active"`
	Sub         string `json:"sub"`
	Description string `json:"description"`
}

// Container adalah satu container hasil `docker ps -a`.
type Container struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	Status string `json:"status"`
}

type Lister struct {
	Dialer *sshx.Dialer
	Store  *store.Store
}

// run membuka koneksi SSH, menjalankan satu perintah, lalu menutupnya.
func (l *Lister) run(ctx context.Context, serverID, cmd string) (string, error) {
	sv, err := l.Store.GetServer(serverID)
	if err != nil {
		return "", err
	}
	conn, err := l.Dialer.Dial(ctx, sv)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	out, err := conn.Output(cmd)
	if err != nil {
		// Beri petunjuk bila perintahnya memang tidak ada di server
		// (mis. docker belum terpasang) — stderr tidak ikut pada Output().
		var ee *ssh.ExitError
		if errors.As(err, &ee) {
			if st := ee.ExitStatus(); st == 127 || st == 126 {
				return "", fmt.Errorf("%w (perintah tidak ditemukan atau tidak bisa dijalankan)", err)
			}
		}
		return "", err
	}
	return string(out), nil
}

// Systemd mengembalikan daftar unit service beserta status aktifnya.
func (l *Lister) Systemd(ctx context.Context, serverID string) ([]Service, error) {
	out, err := l.run(ctx, serverID, "systemctl list-units --type=service --no-pager --no-legend --plain")
	if err != nil {
		return nil, fmt.Errorf("systemd tidak tersedia: %w", err)
	}
	list := make([]Service, 0, 32)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "To show all") {
			continue
		}
		f := strings.Fields(line)
		// Baris marker (mis. "●") dan footer "-- N loaded units listed" dilewati.
		if len(f) > 0 && (f[0] == "●" || f[0] == "*") {
			f = f[1:]
		}
		if len(f) < 4 || strings.HasPrefix(f[0], "--") {
			continue
		}
		desc := ""
		if len(f) > 4 {
			desc = strings.Join(f[4:], " ")
		}
		list = append(list, Service{Name: f[0], Active: f[2], Sub: f[3], Description: desc})
		if len(list) >= maxLines {
			break
		}
	}
	return list, nil
}

// Docker mengembalikan daftar container (termasuk yang berhenti).
func (l *Lister) Docker(ctx context.Context, serverID string) ([]Container, error) {
	out, err := l.run(ctx, serverID, `docker ps -a --format '{{.Names}}	{{.Image}}	{{.Status}}'`)
	if err != nil {
		return nil, fmt.Errorf("docker tidak tersedia: %w", err)
	}
	list := make([]Container, 0, 16)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Sebagian versi docker mengeluarkan "\t" apa adanya, bukan tab nyata.
		line = strings.ReplaceAll(line, `\t`, "\t")
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		list = append(list, Container{
			Name:   strings.TrimSpace(parts[0]),
			Image:  strings.TrimSpace(parts[1]),
			Status: strings.TrimSpace(strings.Join(parts[2:], "\t")),
		})
		if len(list) >= maxLines {
			break
		}
	}
	return list, nil
}
