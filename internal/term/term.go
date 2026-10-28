package term

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"
	"golang.org/x/crypto/ssh"

	"bserver/internal/sshx"
	"bserver/internal/store"
)

// completeUTF8Len mengembalikan panjang awalan b yang seluruhnya terdiri dari
// rune UTF-8 utuh. Sisa byte (rune terpotong) disimpan pemanggil untuk
// digabungkan dengan pembacaan berikutnya.
func completeUTF8Len(b []byte) int {
	i := 0
	for i < len(b) {
		if b[i] < utf8.RuneSelf {
			i++
			continue
		}
		if !utf8.FullRune(b[i:]) {
			return i
		}
		_, size := utf8.DecodeRune(b[i:])
		i += size
	}
	return i
}

type ctrl struct {
	T     string `json:"t"`
	Data  string `json:"d,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	Cols  int    `json:"cols,omitempty"`
	Code  int    `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

type writer struct {
	mu  sync.Mutex
	con *websocket.Conn
	ctx context.Context
}

func (w *writer) send(c ctrl) error {
	b, _ := json.Marshal(c)
	w.mu.Lock()
	defer w.mu.Unlock()
	ctx, cancel := context.WithTimeout(w.ctx, 10*time.Second)
	defer cancel()
	return w.con.Write(ctx, websocket.MessageText, b)
}

// Serve menjalankan satu sesi PTY untuk server tersimpan yang dipetakan ke
// koneksi WebSocket.
func Serve(ctx context.Context, con *websocket.Conn, d *sshx.Dialer, st *store.Store, sv *store.Server, cols, rows int) {
	serve(ctx, con, func() (*sshx.Conn, error) { return d.Dial(ctx, sv) }, cols, rows)
}

// ServeAdhoc menjalankan sesi PTY memakai kredensial ad-hoc dari form (tidak
// disimpan ke inventaris).
func ServeAdhoc(ctx context.Context, con *websocket.Conn, d *sshx.Dialer, in store.ServerInput, cols, rows int) {
	serve(ctx, con, func() (*sshx.Conn, error) { return d.DialInput(ctx, in) }, cols, rows)
}

// serve berisi inti sesi PTY; dial menentukan bagaimana koneksi SSH dibuat.
func serve(ctx context.Context, con *websocket.Conn, dial func() (*sshx.Conn, error), cols, rows int) {
	w := &writer{con: con, ctx: ctx}
	defer con.Close(websocket.StatusNormalClosure, "selesai")

	client, err := dial()
	if err != nil {
		w.send(ctrl{T: "error", Error: err.Error()})
		return
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		w.send(ctrl{T: "error", Error: err.Error()})
		return
	}
	defer sess.Close()

	if cols <= 0 {
		cols = 100
	}
	if rows <= 0 {
		rows = 30
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		w.send(ctrl{T: "error", Error: "gagal minta PTY: " + err.Error()})
		return
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		w.send(ctrl{T: "error", Error: err.Error()})
		return
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		w.send(ctrl{T: "error", Error: err.Error()})
		return
	}
	if err := sess.Shell(); err != nil {
		w.send(ctrl{T: "error", Error: "gagal membuka shell: " + err.Error()})
		return
	}

	var once sync.Once
	done := make(chan struct{})
	go func() {
		// remote -> ws
		buf := make([]byte, 32*1024)
		var pending []byte // sisa byte yang belum membentuk rune UTF-8 utuh
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, 0, len(pending)+n)
				chunk = append(chunk, pending...)
				chunk = append(chunk, buf[:n]...)
				cut := completeUTF8Len(chunk)
				if cut > 0 {
					if e := w.send(ctrl{T: "out", Data: string(chunk[:cut])}); e != nil {
						break
					}
				}
				pending = append(pending[:0], chunk[cut:]...)
				if len(pending) > utf8.UTFMax {
					pending = pending[:0] // data bukan UTF-8 valid; jangan menahan
				}
			}
			if err != nil {
				if len(pending) > 0 {
					w.send(ctrl{T: "out", Data: string(pending)})
				}
				break
			}
		}
		once.Do(func() { close(done) })
	}()

	// ws -> remote
	for {
		_, data, err := con.Read(ctx)
		if err != nil {
			break
		}
		var c ctrl
		if json.Unmarshal(data, &c) != nil {
			continue
		}
		switch c.T {
		case "in":
			if _, err := io.WriteString(stdin, c.Data); err != nil {
				goto finish
			}
		case "resize":
			sess.WindowChange(c.Rows, c.Cols)
		case "ping":
			w.send(ctrl{T: "pong"})
		}
	}
finish:
	sess.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	w.send(ctrl{T: "exit"})
}
