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

type forwardSmartManager struct {
	mu       sync.Mutex
	clientID string
	ws       *wsWriter
	parent   context.Context
	routes   map[string]context.CancelFunc
	sessions map[string]*smartListenSession
	pending  map[string]chan forwardTunnelReadyResult
}

type smartListenSession struct {
	key     protocol.TunnelKey
	conn    net.Conn
	cancel  context.CancelFunc
	routeID string
}

type forwardTunnelReadyResult struct {
	ok      bool
	message string
}

func newForwardSmartManager(ctx context.Context, ws *wsWriter, clientID string) *forwardSmartManager {
	return &forwardSmartManager{
		clientID: clientID,
		ws:       ws,
		parent:   ctx,
		routes:   make(map[string]context.CancelFunc),
		sessions: make(map[string]*smartListenSession),
		pending:  make(map[string]chan forwardTunnelReadyResult),
	}
}

func (m *forwardSmartManager) closeAll() {
	m.mu.Lock()
	routes := m.routes
	m.routes = make(map[string]context.CancelFunc)
	sessions := m.sessions
	m.sessions = make(map[string]*smartListenSession)
	m.mu.Unlock()
	for _, cancel := range routes {
		cancel()
	}
	for _, s := range sessions {
		s.cancel()
		_ = s.conn.Close()
	}
}

func (m *forwardSmartManager) handle(data []byte) bool {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &head) != nil {
		return false
	}
	switch head.Type {
	case protocol.TypeRouteOpenForwardSmart:
		var msg protocol.RouteOpenForwardSmart
		if json.Unmarshal(data, &msg) != nil || msg.RouteID == "" || msg.ListenAddr == "" {
			return true
		}
		go m.openListen(msg.RouteID, msg.ListenAddr)
		return true
	case protocol.TypeRouteClose:
		var msg protocol.RouteClose
		if json.Unmarshal(data, &msg) != nil || msg.RouteID == "" {
			return true
		}
		m.closeRoute(msg.RouteID)
		return true
	case protocol.TypeForwardTunnelReady:
		var msg protocol.ForwardTunnelReady
		if json.Unmarshal(data, &msg) != nil || msg.TunnelID == "" {
			return true
		}
		m.mu.Lock()
		ch, ok := m.pending[msg.TunnelID]
		if ok {
			delete(m.pending, msg.TunnelID)
		}
		m.mu.Unlock()
		if ok {
			select {
			case ch <- forwardTunnelReadyResult{ok: msg.OK, message: msg.Message}:
			default:
			}
		}
		return true
	case protocol.TypeRouteTunnelClose:
		var msg protocol.RouteTunnelClose
		if json.Unmarshal(data, &msg) != nil || msg.TunnelID == "" {
			return true
		}
		m.closeSession(msg.TunnelID)
		return true
	default:
		return false
	}
}

func (m *forwardSmartManager) handleBinary(data []byte) {
	key, payload, ok := protocol.UnwrapTunnelFrame(data)
	if !ok {
		return
	}
	id := fmt.Sprintf("%x", key[:])
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok || s.conn == nil || len(payload) == 0 {
		return
	}
	_, _ = s.conn.Write(payload)
}

func (m *forwardSmartManager) openListen(routeID, listenAddr string) {
	m.closeRoute(routeID)
	ctx, cancel := context.WithCancel(m.parent)
	m.mu.Lock()
	m.routes[routeID] = cancel
	m.mu.Unlock()

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		cancel()
		m.mu.Lock()
		delete(m.routes, routeID)
		m.mu.Unlock()
		m.emit(routeID, protocol.RouteStateError, listenAddr, "", err.Error())
		return
	}
	m.emit(routeID, protocol.RouteStateActive, listenAddr, "", "")
	log.Printf("route forward_smart listen id=%s addr=%s", routeID, listenAddr)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	go func() {
		for {
			in, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					m.closeRoute(routeID)
					m.emit(routeID, protocol.RouteStateError, listenAddr, "", err.Error())
					return
				}
			}
			go m.serveConn(ctx, routeID, in)
		}
	}()
}

func (m *forwardSmartManager) serveConn(parent context.Context, routeID string, conn net.Conn) {
	defer conn.Close()
	key, tunnelHex, err := protocol.NewTunnelKey()
	if err != nil {
		return
	}
	wait := make(chan forwardTunnelReadyResult, 1)
	m.mu.Lock()
	m.pending[tunnelHex] = wait
	m.mu.Unlock()

	connect, _ := json.Marshal(protocol.ForwardTunnelConnect{
		Type: protocol.TypeForwardTunnelConnect, RouteID: routeID, TunnelID: tunnelHex,
	})
	if err := m.ws.WriteText(connect); err != nil {
		m.mu.Lock()
		delete(m.pending, tunnelHex)
		m.mu.Unlock()
		return
	}

	var ready forwardTunnelReadyResult
	select {
	case ready = <-wait:
	case <-time.After(25 * time.Second):
		m.mu.Lock()
		delete(m.pending, tunnelHex)
		m.mu.Unlock()
		return
	case <-parent.Done():
		m.mu.Lock()
		delete(m.pending, tunnelHex)
		m.mu.Unlock()
		return
	}
	if !ready.ok {
		return
	}

	_, cancel := context.WithCancel(parent)
	s := &smartListenSession{key: key, conn: conn, cancel: cancel, routeID: routeID}
	m.mu.Lock()
	m.sessions[tunnelHex] = s
	m.mu.Unlock()
	defer func() {
		m.closeSession(tunnelHex)
		closeMsg, _ := json.Marshal(protocol.RouteTunnelClose{
			Type: protocol.TypeRouteTunnelClose, RouteID: routeID, TunnelID: tunnelHex,
		})
		_ = m.ws.WriteText(closeMsg)
	}()

	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			frame := protocol.WrapTunnelFrame(key, buf[:n])
			_ = m.ws.WriteBinary(frame)
		}
		if err != nil {
			break
		}
	}
}

func (m *forwardSmartManager) closeSession(tunnelID string) {
	m.mu.Lock()
	s, ok := m.sessions[tunnelID]
	if ok {
		delete(m.sessions, tunnelID)
	}
	m.mu.Unlock()
	if ok {
		s.cancel()
		_ = s.conn.Close()
	}
}

func (m *forwardSmartManager) closeRoute(routeID string) {
	m.mu.Lock()
	cancel, ok := m.routes[routeID]
	if ok {
		delete(m.routes, routeID)
	}
	var ids []string
	for id, s := range m.sessions {
		if s.routeID == routeID {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	if ok {
		cancel()
	}
	for _, id := range ids {
		m.closeSession(id)
	}
}

func (m *forwardSmartManager) emit(routeID, state, listenAddr, target, message string) {
	ev, _ := json.Marshal(protocol.RouteEvent{
		Type:       protocol.TypeRouteEvent,
		RouteID:    routeID,
		ClientID:   m.clientID,
		Kind:       protocol.RouteKindForwardSmart,
		State:      state,
		ListenAddr: listenAddr,
		Target:     target,
		Message:    message,
	})
	_ = m.ws.WriteText(ev)
}
