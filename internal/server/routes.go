package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"finalcat/internal/protocol"
)

type RouteRecord struct {
	ID          string    `json:"id"`
	AgentID     string    `json:"agent_id"`
	AgentHost   string    `json:"agent_hostname,omitempty"`
	Kind        string    `json:"kind"`
	ListenAddr  string    `json:"listen_addr"`
	Target      string    `json:"target,omitempty"`
	State       string    `json:"state"`
	Message     string    `json:"message,omitempty"`
	BindOn      string    `json:"bind_on,omitempty"` // agent | server
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RouteManager struct {
	mu         sync.RWMutex
	routes     map[string]*RouteRecord
	socksSrv   map[string]*socksServerRoute
	tunnels    map[string]*activeTunnel
	tunnelWait map[string]*tunnelWaiter
	tunnelMu   sync.Mutex
	hub        *Hub
}

func NewRouteManager(hub *Hub) *RouteManager {
	return &RouteManager{
		routes:     make(map[string]*RouteRecord),
		socksSrv:   make(map[string]*socksServerRoute),
		tunnels:    make(map[string]*activeTunnel),
		tunnelWait: make(map[string]*tunnelWaiter),
		hub:        hub,
	}
}

func (rm *RouteManager) Snapshot() []RouteRecord {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	out := make([]RouteRecord, 0, len(rm.routes))
	for _, r := range rm.routes {
		out = append(out, *r)
	}
	return out
}

func (rm *RouteManager) Get(id string) (*RouteRecord, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	r, ok := rm.routes[id]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

func (rm *RouteManager) OpenSocks(agentID, bindAddr string) (*RouteRecord, error) {
	if bindAddr == "" {
		return nil, fmt.Errorf("bind_addr required")
	}
	if !rm.hub.AgentOnline(agentID) {
		return nil, fmt.Errorf("agent offline")
	}
	id := newRouteID()
	rec := &RouteRecord{
		ID:         id,
		AgentID:    agentID,
		Kind:       protocol.RouteKindSocks,
		ListenAddr: bindAddr,
		BindOn:     "agent",
		State:      protocol.RouteStatePending,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if c, ok := rm.hub.reg.Get(agentID); ok {
		rec.AgentHost = c.Hostname
	}
	rm.mu.Lock()
	rm.routes[id] = rec
	rm.mu.Unlock()
	if !rm.hub.SendAgent(agentID, protocol.RouteOpenSocks{
		Type: protocol.TypeRouteOpenSocks, RouteID: id, BindAddr: bindAddr,
	}) {
		rm.remove(id)
		return nil, fmt.Errorf("failed to send route command to agent")
	}
	return rec, nil
}

func (rm *RouteManager) OpenForward(agentID, listenAddr, targetHost string, targetPort int) (*RouteRecord, error) {
	if listenAddr == "" || targetHost == "" || targetPort <= 0 || targetPort > 65535 {
		return nil, fmt.Errorf("listen_addr, target_host, target_port required")
	}
	if !rm.hub.AgentOnline(agentID) {
		return nil, fmt.Errorf("agent offline")
	}
	target := fmt.Sprintf("%s:%d", targetHost, targetPort)
	id := newRouteID()
	rec := &RouteRecord{
		ID:         id,
		AgentID:    agentID,
		Kind:       protocol.RouteKindForward,
		ListenAddr: listenAddr,
		Target:     target,
		State:      protocol.RouteStatePending,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if c, ok := rm.hub.reg.Get(agentID); ok {
		rec.AgentHost = c.Hostname
	}
	rm.mu.Lock()
	rm.routes[id] = rec
	rm.mu.Unlock()
	if !rm.hub.SendAgent(agentID, protocol.RouteOpenForward{
		Type:       protocol.TypeRouteOpenForward,
		RouteID:    id,
		ListenAddr: listenAddr,
		TargetHost: targetHost,
		TargetPort: targetPort,
	}) {
		rm.remove(id)
		return nil, fmt.Errorf("failed to send route command to agent")
	}
	return rec, nil
}

func (rm *RouteManager) Close(id string) error {
	rm.mu.RLock()
	rec, ok := rm.routes[id]
	rm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("route not found")
	}
	if rec.State == protocol.RouteStateClosed {
		return nil
	}
	if rec.Kind == protocol.RouteKindSocksServer {
		rm.closeSocksServer(id, "closed by operator", protocol.RouteStateClosed)
		return nil
	}
	if rm.hub.AgentOnline(rec.AgentID) {
		rm.hub.SendAgent(rec.AgentID, protocol.RouteClose{
			Type: protocol.TypeRouteClose, RouteID: id,
		})
	}
	rm.setState(id, protocol.RouteStateClosed, "closed by operator")
	return nil
}

func (rm *RouteManager) HandleEvent(agentID string, ev *protocol.RouteEvent) {
	if ev == nil || ev.RouteID == "" {
		return
	}
	rm.mu.Lock()
	rec, ok := rm.routes[ev.RouteID]
	if !ok {
		rec = &RouteRecord{
			ID:        ev.RouteID,
			AgentID:   agentID,
			Kind:      ev.Kind,
			CreatedAt: time.Now().UTC(),
		}
		rm.routes[ev.RouteID] = rec
	}
	if ev.ListenAddr != "" {
		rec.ListenAddr = ev.ListenAddr
	}
	if ev.Target != "" {
		rec.Target = ev.Target
	}
	if ev.Kind != "" {
		rec.Kind = ev.Kind
	}
	if ev.State != "" {
		rec.State = ev.State
	}
	if ev.Message != "" {
		rec.Message = ev.Message
	}
	if ev.BindOn != "" {
		rec.BindOn = ev.BindOn
	}
	rec.UpdatedAt = time.Now().UTC()
	if rec.AgentHost == "" {
		if c, ok := rm.hub.reg.Get(agentID); ok {
			rec.AgentHost = c.Hostname
		}
	}
	rm.mu.Unlock()
}

func (rm *RouteManager) AgentDisconnected(agentID string) {
	rm.mu.RLock()
	var serverRoutes []string
	for id, r := range rm.routes {
		if r.AgentID == agentID && r.Kind == protocol.RouteKindSocksServer && r.State != protocol.RouteStateClosed {
			serverRoutes = append(serverRoutes, id)
		}
	}
	rm.mu.RUnlock()
	for _, id := range serverRoutes {
		rm.closeSocksServer(id, "agent disconnected", protocol.RouteStateClosed)
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()
	now := time.Now().UTC()
	for _, r := range rm.routes {
		if r.AgentID == agentID && r.State != protocol.RouteStateClosed {
			r.State = protocol.RouteStateClosed
			r.Message = "agent disconnected"
			r.UpdatedAt = now
		}
	}
}

func (rm *RouteManager) remove(id string) {
	rm.mu.Lock()
	delete(rm.routes, id)
	rm.mu.Unlock()
}

func (rm *RouteManager) setState(id, state, message string) {
	rm.mu.Lock()
	if r, ok := rm.routes[id]; ok {
		r.State = state
		r.Message = message
		r.UpdatedAt = time.Now().UTC()
	}
	rm.mu.Unlock()
}

func newRouteID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type UISnapshot struct {
	Clients []ClientRecord `json:"clients"`
	Routes  []RouteRecord  `json:"routes"`
}

func (h *Hub) uiSnapshotJSON() ([]byte, error) {
	snap := UISnapshot{
		Clients: h.reg.Snapshot(),
		Routes:  h.routes.Snapshot(),
	}
	return json.Marshal(snap)
}
