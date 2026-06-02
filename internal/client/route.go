package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"

	"finalcat/internal/protocol"
)

type routeManager struct {
	mu       sync.Mutex
	clientID string
	ws       *wsWriter
	tunnels  *tunnelManager
	parent   context.Context
	routes   map[string]routeEntry
}

type routeEntry struct {
	cancel     context.CancelFunc
	kind       string
	listenAddr string
	target     string
}

func newRouteManager(ctx context.Context, ws *wsWriter, tunnels *tunnelManager, clientID string) *routeManager {
	return &routeManager{
		clientID: clientID,
		ws:       ws,
		tunnels:  tunnels,
		parent:   ctx,
		routes:   make(map[string]routeEntry),
	}
}

func (m *routeManager) closeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.routes {
		e.cancel()
		delete(m.routes, id)
	}
}

func (m *routeManager) handle(data []byte) bool {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &head) != nil {
		return false
	}
	switch head.Type {
	case protocol.TypeRouteOpenSocks:
		var msg protocol.RouteOpenSocks
		if json.Unmarshal(data, &msg) != nil || msg.RouteID == "" || msg.BindAddr == "" {
			return true
		}
		go m.openSocks(msg.RouteID, msg.BindAddr)
		return true
	case protocol.TypeRouteOpenForward:
		var msg protocol.RouteOpenForward
		if json.Unmarshal(data, &msg) != nil || msg.RouteID == "" || msg.ListenAddr == "" {
			return true
		}
		go m.openForward(msg.RouteID, msg.ListenAddr, msg.TargetHost, msg.TargetPort)
		return true
	case protocol.TypeRouteClose:
		var msg protocol.RouteClose
		if json.Unmarshal(data, &msg) != nil || msg.RouteID == "" {
			return true
		}
		m.closeRoute(msg.RouteID, "closed by server", protocol.RouteStateClosed)
		if m.tunnels != nil {
			m.tunnels.closeRouteTunnels(msg.RouteID)
		}
		return true
	default:
		return false
	}
}

func (m *routeManager) openSocks(routeID, bindAddr string) {
	ctx, cancel := context.WithCancel(m.parent)
	if !m.register(routeID, routeEntry{cancel: cancel, kind: protocol.RouteKindSocks, listenAddr: bindAddr}) {
		cancel()
		m.emit(routeID, protocol.RouteKindSocks, protocol.RouteStateError, bindAddr, "", "route_id already active")
		return
	}
	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		m.unregister(routeID)
		cancel()
		m.emit(routeID, protocol.RouteKindSocks, protocol.RouteStateError, bindAddr, "", err.Error())
		return
	}
	m.emit(routeID, protocol.RouteKindSocks, protocol.RouteStateActive, bindAddr, "", "")
	log.Printf("route socks active id=%s listen=%s", routeID, bindAddr)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					m.closeRoute(routeID, err.Error(), protocol.RouteStateError)
					return
				}
			}
			go serveSOCKS5Client(c)
		}
	}()
}

func (m *routeManager) openForward(routeID, listenAddr, targetHost string, targetPort int) {
	if targetHost == "" || targetPort <= 0 || targetPort > 65535 {
		m.emit(routeID, protocol.RouteKindForward, protocol.RouteStateError, listenAddr, "", "invalid target")
		return
	}
	target := net.JoinHostPort(targetHost, fmt.Sprintf("%d", targetPort))
	ctx, cancel := context.WithCancel(m.parent)
	if !m.register(routeID, routeEntry{cancel: cancel, kind: protocol.RouteKindForward, listenAddr: listenAddr, target: target}) {
		cancel()
		m.emit(routeID, protocol.RouteKindForward, protocol.RouteStateError, listenAddr, target, "route_id already active")
		return
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		m.unregister(routeID)
		cancel()
		m.emit(routeID, protocol.RouteKindForward, protocol.RouteStateError, listenAddr, target, err.Error())
		return
	}
	m.emit(routeID, protocol.RouteKindForward, protocol.RouteStateActive, listenAddr, target, "")
	log.Printf("route forward active id=%s listen=%s -> %s", routeID, listenAddr, target)

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
					m.closeRoute(routeID, err.Error(), protocol.RouteStateError)
					return
				}
			}
			go m.forwardOnce(in, target)
		}
	}()
}

func (m *routeManager) forwardOnce(in net.Conn, target string) {
	defer in.Close()
	out, err := net.Dial("tcp", target)
	if err != nil {
		log.Printf("forward dial %s: %v", target, err)
		return
	}
	defer out.Close()
	relayTCP(in, out)
}

func (m *routeManager) closeRoute(routeID, message, state string) {
	m.mu.Lock()
	e, ok := m.routes[routeID]
	if ok {
		e.cancel()
		delete(m.routes, routeID)
	}
	kind, listen, target := "", "", ""
	if ok {
		kind, listen, target = e.kind, e.listenAddr, e.target
	}
	m.mu.Unlock()
	if ok {
		m.emit(routeID, kind, state, listen, target, message)
	}
}

func (m *routeManager) register(routeID string, e routeEntry) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.routes[routeID]; ok {
		return false
	}
	m.routes[routeID] = e
	return true
}

func (m *routeManager) unregister(routeID string) {
	m.mu.Lock()
	delete(m.routes, routeID)
	m.mu.Unlock()
}

func (m *routeManager) emit(routeID, kind, state, listenAddr, target, message string) {
	ev, _ := json.Marshal(protocol.RouteEvent{
		Type:       protocol.TypeRouteEvent,
		RouteID:    routeID,
		ClientID:   m.clientID,
		Kind:       kind,
		State:      state,
		ListenAddr: listenAddr,
		Target:     target,
		Message:    message,
	})
	m.mu.Lock()
	ws := m.ws
	m.mu.Unlock()
	if ws == nil {
		return
	}
	_ = ws.WriteText(ev)
}
