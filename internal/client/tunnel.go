package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"finalcat/internal/protocol"
)

type tunnelManager struct {
	mu       sync.Mutex
	ws       *wsWriter
	parent   context.Context
	tunnels  map[string]*tunnelSession
}

type tunnelSession struct {
	key     protocol.TunnelKey
	routeID string
	remote  net.Conn
	cancel  context.CancelFunc
}

func newTunnelManager(ctx context.Context, ws *wsWriter) *tunnelManager {
	return &tunnelManager{
		ws:      ws,
		parent:  ctx,
		tunnels: make(map[string]*tunnelSession),
	}
}

func (m *tunnelManager) closeAll() {
	m.mu.Lock()
	list := make([]*tunnelSession, 0, len(m.tunnels))
	for _, t := range m.tunnels {
		list = append(list, t)
	}
	m.tunnels = make(map[string]*tunnelSession)
	m.mu.Unlock()
	for _, t := range list {
		t.cancel()
		_ = t.remote.Close()
	}
}

func (m *tunnelManager) handleText(data []byte) bool {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &head) != nil {
		return false
	}
	switch head.Type {
	case protocol.TypeRouteTunnelOpen:
		var msg protocol.RouteTunnelOpen
		if json.Unmarshal(data, &msg) != nil || msg.TunnelID == "" {
			return true
		}
		go m.openTunnel(msg)
		return true
	case protocol.TypeRouteTunnelClose:
		var msg protocol.RouteTunnelClose
		if json.Unmarshal(data, &msg) != nil {
			return true
		}
		if msg.TunnelID != "" {
			m.closeTunnel(msg.TunnelID)
		} else if msg.RouteID != "" {
			m.closeRouteTunnels(msg.RouteID)
		}
		return true
	default:
		return false
	}
}

func (m *tunnelManager) handleBinary(data []byte) {
	key, payload, ok := protocol.UnwrapTunnelFrame(data)
	if !ok {
		return
	}
	id := keyHex(key)
	m.mu.Lock()
	t, ok := m.tunnels[id]
	m.mu.Unlock()
	if !ok || t.remote == nil {
		return
	}
	if len(payload) == 0 {
		return
	}
	_, _ = t.remote.Write(payload)
}

func (m *tunnelManager) openTunnel(msg protocol.RouteTunnelOpen) {
	key, err := protocol.TunnelKeyFromHex(msg.TunnelID)
	if err != nil {
		m.sendAck(msg, false, err.Error())
		return
	}
	target := net.JoinHostPort(msg.TargetHost, fmt.Sprintf("%d", msg.TargetPort))
	remote, err := net.DialTimeout("tcp", target, 15*time.Second)
	if err != nil {
		log.Printf("tunnel dial %s: %v", target, err)
		m.sendAck(msg, false, err.Error())
		return
	}
	ctx, cancel := context.WithCancel(m.parent)
	s := &tunnelSession{key: key, routeID: msg.RouteID, remote: remote, cancel: cancel}
	m.mu.Lock()
	m.tunnels[msg.TunnelID] = s
	m.mu.Unlock()
	m.sendAck(msg, true, "")
	log.Printf("tunnel open id=%s route=%s -> %s", msg.TunnelID, msg.RouteID, target)

	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := remote.Read(buf)
			if n > 0 {
				frame := protocol.WrapTunnelFrame(key, buf[:n])
				_ = m.ws.WriteBinary(frame)
			}
			if err != nil {
				break
			}
		}
		m.closeTunnel(msg.TunnelID)
		closeMsg, _ := json.Marshal(protocol.RouteTunnelClose{
			Type: protocol.TypeRouteTunnelClose, RouteID: msg.RouteID, TunnelID: msg.TunnelID,
		})
		_ = m.ws.WriteText(closeMsg)
	}()

	go func() {
		<-ctx.Done()
		_ = remote.Close()
	}()
}

func (m *tunnelManager) sendAck(msg protocol.RouteTunnelOpen, ok bool, message string) {
	ack, _ := json.Marshal(protocol.RouteTunnelAck{
		Type:     protocol.TypeRouteTunnelAck,
		RouteID:  msg.RouteID,
		TunnelID: msg.TunnelID,
		OK:       ok,
		Message:  message,
	})
	_ = m.ws.WriteText(ack)
}

func (m *tunnelManager) closeTunnel(id string) {
	m.mu.Lock()
	t, ok := m.tunnels[id]
	if ok {
		delete(m.tunnels, id)
	}
	m.mu.Unlock()
	if ok {
		t.cancel()
		_ = t.remote.Close()
	}
}

func (m *tunnelManager) closeRouteTunnels(routeID string) {
	m.mu.Lock()
	var ids []string
	for id, t := range m.tunnels {
		if t.routeID == routeID {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.closeTunnel(id)
	}
}

func keyHex(k protocol.TunnelKey) string {
	return fmt.Sprintf("%x", k[:])
}
