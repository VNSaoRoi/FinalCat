package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/gorilla/websocket"

	"finalcat/internal/protocol"
)

type BindAttachRequest struct {
	BindHost string `json:"bind_host"`
	BindPort int    `json:"bind_port"`
	Path     string `json:"path,omitempty"`
}

type BindAttachResponse struct {
	OK       bool   `json:"ok"`
	ClientID string `json:"client_id,omitempty"`
	Message  string `json:"message,omitempty"`
}

func (h *Hub) AttachBind(ctx context.Context, host string, port int, path string) (string, error) {
	if host == "" || port <= 0 || port > 65535 {
		return "", fmt.Errorf("bind_host and bind_port required")
	}
	if path == "" {
		path = "/ws/agent"
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	u := url.URL{Scheme: "ws", Host: addr, Path: path}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("dial bind agent: %w", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		_ = conn.Close()
		return "", fmt.Errorf("read register: %w", err)
	}
	var reg protocol.Register
	if json.Unmarshal(raw, &reg) != nil || reg.Type != protocol.TypeRegister {
		_ = conn.Close()
		return "", fmt.Errorf("expected register frame")
	}
	if reg.AgentMode == "" {
		reg.AgentMode = protocol.AgentModeBind
	}
	remote := host
	id := h.startAgentSession(conn, &reg, remote)
	if id == "" {
		return "", fmt.Errorf("failed to start session")
	}
	return id, nil
}
