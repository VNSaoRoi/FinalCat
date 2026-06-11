package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
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
	catalog    *RouteCatalog
}

func NewRouteManager(hub *Hub, catalog *RouteCatalog) *RouteManager {
	return &RouteManager{
		routes:     make(map[string]*RouteRecord),
		socksSrv:   make(map[string]*socksServerRoute),
		tunnels:    make(map[string]*activeTunnel),
		tunnelWait: make(map[string]*tunnelWaiter),
		hub:        hub,
		catalog:    catalog,
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

func (rm *RouteManager) OnAgentConnected(agentID, persistentID, hostname, osUser string) {
	if rm.catalog == nil || persistentID == "" {
		return
	}
	rm.catalog.TouchAgent(persistentID, agentID, hostname, osUser)
	_ = rm.catalog.Save()
	rm.RestoreDesired(agentID, persistentID)
}

func (rm *RouteManager) persistRoute(agentID string, rec *RouteRecord) {
	if rm.catalog == nil {
		return
	}
	pid := rm.hub.PersistentIDFor(agentID)
	if pid == "" {
		return
	}
	rm.catalog.UpsertRoute(pid, desiredFromRecord(rec))
	_ = rm.catalog.Save()
}

func (rm *RouteManager) unpersistRoute(agentID, routeID string) {
	if rm.catalog == nil {
		return
	}
	pid := rm.hub.PersistentIDFor(agentID)
	if pid == "" {
		return
	}
	rm.catalog.RemoveRoute(pid, routeID)
	_ = rm.catalog.Save()
}

func (rm *RouteManager) OpenSocks(agentID, bindAddr string) (*RouteRecord, error) {
	return rm.openSocks(agentID, newRouteID(), bindAddr)
}

func (rm *RouteManager) openSocks(agentID, routeID, bindAddr string) (*RouteRecord, error) {
	if bindAddr == "" {
		return nil, fmt.Errorf("bind_addr required")
	}
	if !rm.hub.AgentOnline(agentID) {
		return nil, fmt.Errorf("agent offline")
	}
	rec := &RouteRecord{
		ID:         routeID,
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
	rm.routes[routeID] = rec
	rm.mu.Unlock()
	if !rm.hub.SendAgent(agentID, protocol.RouteOpenSocks{
		Type: protocol.TypeRouteOpenSocks, RouteID: routeID, BindAddr: bindAddr,
	}) {
		rm.remove(routeID)
		return nil, fmt.Errorf("failed to send route command to agent")
	}
	rm.persistRoute(agentID, rec)
	return rec, nil
}

func (rm *RouteManager) OpenForward(agentID, listenAddr, targetHost string, targetPort int) (*RouteRecord, error) {
	return rm.openForward(agentID, newRouteID(), listenAddr, targetHost, targetPort)
}

func (rm *RouteManager) openForward(agentID, routeID, listenAddr, targetHost string, targetPort int) (*RouteRecord, error) {
	if listenAddr == "" || targetHost == "" || targetPort <= 0 || targetPort > 65535 {
		return nil, fmt.Errorf("listen_addr, target_host, target_port required")
	}
	if !rm.hub.AgentOnline(agentID) {
		return nil, fmt.Errorf("agent offline")
	}
	target := fmt.Sprintf("%s:%d", targetHost, targetPort)
	rec := &RouteRecord{
		ID:         routeID,
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
	rm.routes[routeID] = rec
	rm.mu.Unlock()
	if !rm.hub.SendAgent(agentID, protocol.RouteOpenForward{
		Type:       protocol.TypeRouteOpenForward,
		RouteID:    routeID,
		ListenAddr: listenAddr,
		TargetHost: targetHost,
		TargetPort: targetPort,
	}) {
		rm.remove(routeID)
		return nil, fmt.Errorf("failed to send route command to agent")
	}
	rm.persistRoute(agentID, rec)
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
		rm.unpersistRoute(rec.AgentID, id)
		return nil
	}
	if rec.Kind == protocol.RouteKindSocksServer {
		rm.closeSocksServer(id, "closed by operator", protocol.RouteStateClosed)
		rm.unpersistRoute(rec.AgentID, id)
		return nil
	}
	if rm.hub.AgentOnline(rec.AgentID) {
		rm.hub.SendAgent(rec.AgentID, protocol.RouteClose{
			Type: protocol.TypeRouteClose, RouteID: id,
		})
	}
	rm.setState(id, protocol.RouteStateClosed, "closed by operator")
	rm.unpersistRoute(rec.AgentID, id)
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

	if ev.State == protocol.RouteStateActive {
		rm.persistRoute(agentID, rec)
	}
}

// AgentDisconnected tears down live tunnels but keeps desired routes in the catalog for reconnect restore.
func (rm *RouteManager) AgentDisconnected(agentID string) {
	pid := rm.hub.PersistentIDFor(agentID)

	rm.mu.RLock()
	var serverRoutes []string
	var toSync []*RouteRecord
	for id, r := range rm.routes {
		if r.AgentID != agentID {
			continue
		}
		if r.Kind == protocol.RouteKindSocksServer && r.State != protocol.RouteStateClosed {
			serverRoutes = append(serverRoutes, id)
		}
		if r.State != protocol.RouteStateClosed {
			cp := *r
			toSync = append(toSync, &cp)
		}
		_ = id
	}
	rm.mu.RUnlock()

	if pid != "" && rm.catalog != nil {
		for _, r := range toSync {
			rm.catalog.UpsertRoute(pid, desiredFromRecord(r))
		}
		_ = rm.catalog.Save()
	}

	for _, id := range serverRoutes {
		rm.closeSocksServer(id, "agent offline", protocol.RouteStateClosed)
	}

	rm.mu.Lock()
	for id, r := range rm.routes {
		if r.AgentID == agentID {
			delete(rm.routes, id)
		}
	}
	rm.mu.Unlock()
}

// RestoreDesired re-opens routes saved for this agent identity after reconnect.
func (rm *RouteManager) RestoreDesired(agentID, persistentID string) {
	if rm.catalog == nil || persistentID == "" || !rm.hub.AgentOnline(agentID) {
		return
	}
	desired := rm.catalog.Routes(persistentID)
	if len(desired) == 0 {
		return
	}
	log.Printf("route restore: agent=%s persistent_id=%s routes=%d", agentID, persistentID, len(desired))
	for _, dr := range desired {
		rm.restoreOne(agentID, dr)
	}
}

func (rm *RouteManager) restoreOne(agentID string, dr DesiredRoute) {
	rm.mu.RLock()
	_, live := rm.routes[dr.ID]
	rm.mu.RUnlock()
	if live {
		return
	}

	var err error
	switch dr.Kind {
	case protocol.RouteKindSocks:
		if dr.BindOn == "server" {
			_, err = rm.openSocksServer(agentID, dr.ID, dr.ListenAddr)
		} else {
			_, err = rm.openSocks(agentID, dr.ID, dr.ListenAddr)
		}
	case protocol.RouteKindSocksServer:
		_, err = rm.openSocksServer(agentID, dr.ID, dr.ListenAddr)
	case protocol.RouteKindForward:
		host, port := dr.TargetHost, dr.TargetPort
		if host == "" && dr.Target != "" {
			host, port = splitTargetHostPort(dr.Target)
		}
		if host == "" || port <= 0 {
			log.Printf("route restore skip id=%s: bad target", dr.ID)
			return
		}
		_, err = rm.openForward(agentID, dr.ID, dr.ListenAddr, host, port)
	default:
		return
	}
	if err != nil {
		log.Printf("route restore id=%s: %v", dr.ID, err)
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
	h.reg.DedupeAllPersistentIDs()
	snap := UISnapshot{
		Clients: h.reg.Snapshot(),
		Routes:  h.routes.Snapshot(),
	}
	return json.Marshal(snap)
}
