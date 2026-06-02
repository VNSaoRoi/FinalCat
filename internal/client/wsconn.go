package client

import (
	"sync"

	"github.com/gorilla/websocket"
)

// wsWriter serializes writes to a WebSocket (gorilla/websocket is not concurrent-safe).
type wsWriter struct {
	c  *websocket.Conn
	mu sync.Mutex
}

func newWSWriter(c *websocket.Conn) *wsWriter {
	return &wsWriter{c: c}
}

func (w *wsWriter) WriteText(b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.WriteMessage(websocket.TextMessage, b)
}

func (w *wsWriter) WriteBinary(b []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.WriteMessage(websocket.BinaryMessage, b)
}
