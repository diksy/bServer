package httpapi

import (
	"sync"

	"github.com/coder/websocket"
)

// wsHub mencatat koneksi WebSocket terminal yang aktif agar dapat diputus
// saat vault dikunci (manual maupun auto-lock).
type wsHub struct {
	mu    sync.Mutex
	conns map[*websocket.Conn]struct{}
}

func newWSHub() *wsHub {
	return &wsHub{conns: make(map[*websocket.Conn]struct{})}
}

func (h *wsHub) add(c *websocket.Conn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
}

func (h *wsHub) remove(c *websocket.Conn) {
	h.mu.Lock()
	delete(h.conns, c)
	h.mu.Unlock()
}

func (h *wsHub) closeAll(reason string) {
	h.mu.Lock()
	list := make([]*websocket.Conn, 0, len(h.conns))
	for c := range h.conns {
		list = append(list, c)
	}
	h.mu.Unlock()
	for _, c := range list {
		_ = c.Close(websocket.StatusPolicyViolation, reason)
	}
}
