package sshx

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"bserver/internal/store"
)

type Dialer struct {
	Store *store.Store
}

type Conn struct {
	*ssh.Client
	extra         []*Conn
	Fingerprint   string
	HostKeyAlgo   string
}

func (c *Conn) Close() error {
	err := c.Client.Close()
	for _, e := range c.extra {
		e.Close()
	}
	return err
}

// Output menjalankan perintah satu-shot pada koneksi ini.
func (c *Conn) Output(cmd string) ([]byte, error) {
	sess, err := c.Client.NewSession()
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	return sess.Output(cmd)
}

type captured struct{ algo, fp string }

// Dial membuka koneksi SSH ke server (maks 1 hop jump host) dengan TOFU host key.
func (d *Dialer) Dial(ctx context.Context, sv *store.Server) (*Conn, error) {
	t, err := d.Store.Resolve(sv)
	if err != nil {
		return nil, err
	}
	jump, err := d.jumpFor(ctx, sv.ID, sv.JumpServerID)
	if err != nil {
		return nil, err
	}
	conn, err := d.dial(ctx, t, sv.Host, sv.Port, jump)
	if err != nil {
		return nil, err
	}
	if conn.Fingerprint != "" {
		d.Store.SetHostKey(sv.ID, conn.HostKeyAlgo, conn.Fingerprint)
	}
	return conn, nil
}

// DialInput membuka koneksi memakai kredensial dari form (tanpa server tersimpan).
func (d *Dialer) DialInput(ctx context.Context, in store.ServerInput) (*Conn, error) {
	t := &store.Target{
		AuthType:   in.AuthType,
		Username:   in.Username,
		KeyPath:    deref(in.KeyPath),
		Password:   deref(in.Password),
		Passphrase: deref(in.Passphrase),
	}
	port := in.Port
	if port <= 0 {
		port = 22
	}
	jump, err := d.jumpFor(ctx, "", in.JumpServerID)
	if err != nil {
		return nil, err
	}
	return d.dial(ctx, t, strings.TrimSpace(in.Host), port, jump)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// jumpFor mengambil data jump host bila jumpID diisi. selfID dipakai untuk
// mencegah server menjadi jump host dirinya sendiri.
func (d *Dialer) jumpFor(ctx context.Context, selfID string, jumpID *string) (*store.Server, error) {
	if jumpID == nil || *jumpID == "" {
		return nil, nil
	}
	j, err := d.Store.GetServer(*jumpID)
	if err != nil {
		return nil, fmt.Errorf("jump host tidak ditemukan: %w", err)
	}
	if selfID != "" && j.ID == selfID {
		return nil, errors.New("server tidak boleh menjadi jump host dirinya sendiri")
	}
	return j, nil
}

// dial melakukan handshake SSH ke host:port, lewat jump host bila diberikan.
// Tidak menyentuh server.host_key_* (tidak ada id) — TOFU tetap dicatat di known_hosts.
func (d *Dialer) dial(ctx context.Context, t *store.Target, host string, port int, jump *store.Server) (*Conn, error) {
	var jumpConn *Conn
	var base net.Conn
	if jump != nil {
		jc, err := d.dialDirect(ctx, jump)
		if err != nil {
			return nil, fmt.Errorf("jump host %s: %w", jump.Name, err)
		}
		jumpConn = jc
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		c, err := jc.DialContext(ctx, "tcp", addr)
		if err != nil {
			jc.Close()
			return nil, fmt.Errorf("terowongan ke %s via jump host gagal: %w", addr, err)
		}
		base = c
	}

	cap := &captured{}
	cfg, err := d.clientConfig(t, host, port, cap)
	if err != nil {
		if jumpConn != nil {
			jumpConn.Close()
		}
		return nil, err
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if base == nil {
		nd := net.Dialer{Timeout: 12 * time.Second}
		raw, err := nd.DialContext(ctx, "tcp", addr)
		if err != nil {
			if jumpConn != nil {
				jumpConn.Close()
			}
			return nil, fmt.Errorf("tidak bisa menghubungi %s: %w", addr, err)
		}
		base = raw
	}
	cc, chans, reqs, err := ssh.NewClientConn(base, addr, cfg)
	if err != nil {
		base.Close()
		if jumpConn != nil {
			jumpConn.Close()
		}
		return nil, err
	}
	result := &Conn{Client: ssh.NewClient(cc, chans, reqs), Fingerprint: cap.fp, HostKeyAlgo: cap.algo}
	if jumpConn != nil {
		result.extra = append(result.extra, jumpConn)
	}
	return result, nil
}

func (d *Dialer) dialDirect(ctx context.Context, sv *store.Server) (*Conn, error) {
	t, err := d.Store.Resolve(sv)
	if err != nil {
		return nil, err
	}
	cap := &captured{}
	cfg, err := d.clientConfig(t, sv.Host, sv.Port, cap)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(sv.Host, strconv.Itoa(sv.Port))
	nd := net.Dialer{Timeout: 12 * time.Second}
	raw, err := nd.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tidak bisa menghubungi %s: %w", addr, err)
	}
	cc, chans, reqs, err := ssh.NewClientConn(raw, addr, cfg)
	if err != nil {
		raw.Close()
		return nil, err
	}
	if cap.fp != "" {
		d.Store.SetHostKey(sv.ID, cap.algo, cap.fp)
	}
	return &Conn{Client: ssh.NewClient(cc, chans, reqs), Fingerprint: cap.fp, HostKeyAlgo: cap.algo}, nil
}

func (d *Dialer) clientConfig(t *store.Target, host string, port int, cap *captured) (*ssh.ClientConfig, error) {
	var auths []ssh.AuthMethod
	switch t.AuthType {
	case "password":
		if t.Password == "" {
			return nil, errors.New("password belum disimpan untuk server ini")
		}
		auths = append(auths, ssh.Password(t.Password))
	case "key":
		am, err := keyAuth(t.KeyPath, t.Passphrase)
		if err != nil {
			return nil, err
		}
		auths = append(auths, am)
	case "agent":
		am, err := agentAuth()
		if err != nil {
			return nil, err
		}
		auths = append(auths, am)
	default:
		return nil, fmt.Errorf("metode auth tidak dikenal: %q", t.AuthType)
	}

	cb := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		algo := key.Type()
		fp := ssh.FingerprintSHA256(key)
		status, err := d.Store.CheckHostKey(host, port, algo, fp)
		if err != nil {
			return fmt.Errorf("gagal memeriksa host key: %w", err)
		}
		if status == "changed" {
			return fmt.Errorf("HOST KEY BERUBAH untuk %s:%d (%s %s) — koneksi ditolak, kemungkinan MITM", host, port, algo, fp)
		}
		cap.algo, cap.fp = algo, fp
		return nil
	}
	return &ssh.ClientConfig{
		User:            t.Username,
		Auth:            auths,
		HostKeyCallback: cb,
		Timeout:         12 * time.Second,
	}, nil
}

func keyAuth(path, passphrase string) (ssh.AuthMethod, error) {
	if path == "" {
		return nil, errors.New("path private key kosong")
	}
	b, err := os.ReadFile(ExpandHome(path))
	if err != nil {
		return nil, fmt.Errorf("private key tidak bisa dibaca: %w", err)
	}
	var signer ssh.Signer
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(b)
	}
	if err != nil {
		return nil, fmt.Errorf("private key tidak valid: %w", err)
	}
	return ssh.PublicKeys(signer), nil
}

func agentAuth() (ssh.AuthMethod, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK tidak diset (ssh-agent tidak berjalan)")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, fmt.Errorf("tidak bisa menghubungi ssh-agent: %w", err)
	}
	ag := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ag.Signers), nil
}

func ExpandHome(p string) string {
	if len(p) > 0 && p[0] == '~' {
		if h, err := os.UserHomeDir(); err == nil {
			return h + p[1:]
		}
	}
	return p
}
