package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"finalcat/internal/protocol"
)

const (
	serverPingInterval = 60 * time.Second
	serverPingMissDead = 10
	agentSendBuf       = 128
)

var agentUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

type agentOutMsg struct {
	msgType int
	data    []byte
}

type agentSession struct {
	id          string
	conn        *websocket.Conn
	send        chan agentOutMsg
	mu          sync.Mutex
	pingSeq     int64
	awaitingAck bool
	missStreak  int
}

type Hub struct {
	reg    *Registry
	routes *RouteManager
	relays *RelayManager

	mu     sync.RWMutex
	agents map[string]*agentSession
	ui     map[*websocket.Conn]struct{}
	uib    chan []byte
}

func NewHub(reg *Registry, catalog *RouteCatalog, controlAdvertise string) *Hub {
	h := &Hub{
		reg:    reg,
		agents: make(map[string]*agentSession),
		ui:     make(map[*websocket.Conn]struct{}),
		uib:    make(chan []byte, 8),
	}
	h.routes = NewRouteManager(h, catalog)
	h.relays = NewRelayManager(h, controlAdvertise)
	return h
}

func (h *Hub) PersistentIDFor(agentID string) string {
	c, ok := h.reg.Get(agentID)
	if !ok {
		return ""
	}
	return c.PersistentID
}

func (h *Hub) notifyUI() {
	b, err := h.uiSnapshotJSON()
	if err != nil {
		return
	}
	select {
	case h.uib <- b:
	default:
	}
}

func (h *Hub) AgentOnline(id string) bool {
	h.mu.RLock()
	_, ok := h.agents[id]
	h.mu.RUnlock()
	return ok
}

func (h *Hub) SendAgent(id string, v any) bool {
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return h.sendAgentRaw(id, websocket.TextMessage, b)
}

func (h *Hub) SendAgentBinary(id string, data []byte) bool {
	return h.sendAgentRaw(id, websocket.BinaryMessage, data)
}

func (h *Hub) sendAgentRaw(id string, msgType int, b []byte) bool {
	h.mu.RLock()
	s, ok := h.agents[id]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case s.send <- agentOutMsg{msgType: msgType, data: b}:
		return true
	default:
		log.Printf("agent %s: send queue full", id)
		return false
	}
}

func (h *Hub) PushUpstream(id string, eps []string, rev int64) bool {
	return h.SendAgent(id, protocol.SetUpstream{
		Type: protocol.TypeSetUpstream, Endpoints: eps, Revision: rev,
	})
}

func (h *Hub) notePong(s *agentSession, seq int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.awaitingAck && seq == s.pingSeq {
		s.awaitingAck = false
		s.missStreak = 0
	}
}

func resolvePersistentID(reg *protocol.Register) string {
	if id := strings.TrimSpace(reg.PersistentID); id != "" {
		return id
	}
	return DerivePersistentID(reg.Hostname, reg.OS, reg.GOArch, reg.OSUser, reg.LocalIPs)
}

func recordFromRegister(reg *protocol.Register, remoteAddr string) *ClientRecord {
	rec := &ClientRecord{
		PersistentID:       resolvePersistentID(reg),
		Hostname:           reg.Hostname,
		OS:                 reg.OS,
		GOArch:             reg.GOArch,
		OSUser:             reg.OSUser,
		LocalIPs:           append([]string(nil), reg.LocalIPs...),
		UpstreamEndpoints:  append([]string(nil), reg.UpstreamEndpoints...),
		ActiveUpstreamUsed: reg.ActiveUpstreamUsed,
		AgentMode:          reg.AgentMode,
		RemoteAddr:         remoteAddr,
	}
	for _, l := range reg.Listeners {
		rec.Listeners = append(rec.Listeners, ListenerRecord{
			Address: l.Address, Role: l.Role, State: l.State,
		})
	}
	if reg.AgentMode == protocol.AgentModeBind && len(rec.Listeners) > 0 {
		rec.Listening = true
	}
	return rec
}

func (h *Hub) closeSessionsForPersistentID(pid, exceptID string) {
	if pid == "" {
		return
	}
	h.mu.RLock()
	type pair struct {
		id string
		s  *agentSession
	}
	var candidates []pair
	for sid, s := range h.agents {
		if sid != exceptID {
			candidates = append(candidates, pair{sid, s})
		}
	}
	h.mu.RUnlock()
	for _, p := range candidates {
		if c, ok := h.reg.Get(p.id); ok && c.PersistentID == pid {
			_ = p.s.conn.Close()
		}
	}
}

// startAgentSession registers agent and runs control loop. Returns client id.
func (h *Hub) startAgentSession(conn *websocket.Conn, reg *protocol.Register, remoteAddr string) string {
	incoming := recordFromRegister(reg, remoteAddr)
	pid := incoming.PersistentID

	id := ""
	if pid != "" {
		if existing, ok := h.reg.FindIDByPersistentID(pid); ok {
			id = existing
		}
	}
	if id == "" {
		id = randomID()
	}

	h.closeSessionsForPersistentID(pid, id)
	h.reg.PurgeDuplicatePersistentID(id, pid)

	snap := h.reg.Register(id, incoming)
	if pid != "" && id != "" {
		log.Printf("agent session: persistent_id=%s client_id=%s hostname=%s", pid, id, snap.Hostname)
	}
	ack, _ := json.Marshal(protocol.Ack{
		Type: protocol.TypeAck, ClientID: id, Revision: snap.Revision, Message: "registered",
	})
	_ = conn.WriteMessage(websocket.TextMessage, ack)

	s := &agentSession{id: id, conn: conn, send: make(chan agentOutMsg, agentSendBuf)}
	h.mu.Lock()
	if old, ok := h.agents[id]; ok && old != s {
		close(old.send)
		_ = old.conn.Close()
	}
	h.agents[id] = s
	h.mu.Unlock()

	if snap.PersistentID != "" {
		h.routes.OnAgentConnected(id, snap.PersistentID, snap.Hostname, snap.OSUser)
	}
	h.relays.OnAgentConnected(id)

	h.notifyUI()

	go h.runAgentLoop(s)
	return id
}

func (h *Hub) HandleAgentWS(conn *websocket.Conn, remoteAddr string) {
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		_ = conn.Close()
		return
	}
	var reg protocol.Register
	if json.Unmarshal(raw, &reg) != nil || reg.Type != protocol.TypeRegister {
		_ = conn.Close()
		return
	}
	if reg.AgentMode == "" {
		reg.AgentMode = protocol.AgentModeReverse
	}
	h.startAgentSession(conn, &reg, remoteAddr)
}

func (h *Hub) runAgentLoop(s *agentSession) {
	id := s.id
	defer func() {
		h.mu.Lock()
		if cur, ok := h.agents[id]; ok && cur == s {
			delete(h.agents, id)
		}
		h.mu.Unlock()
		close(s.send)
		h.reg.SetOffline(id)
		h.routes.AgentDisconnected(id)
		h.relays.AgentDisconnected(id)
		h.notifyUI()
		_ = s.conn.Close()
	}()

	done := make(chan struct{})
	go h.agentWritePump(s, done)
	go h.agentPingLoop(s, done)
	defer close(done)

	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		msgType, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType == websocket.BinaryMessage {
			h.routes.HandleTunnelBinary(id, data)
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &head) != nil {
			continue
		}
		switch head.Type {
		case protocol.TypePong:
			var p protocol.Pong
			if json.Unmarshal(data, &p) == nil && p.ClientID == id {
				h.notePong(s, p.Seq)
				h.reg.Touch(id)
				h.notifyUI()
			}
		case protocol.TypeHeartbeat:
			var hb protocol.Heartbeat
			if json.Unmarshal(data, &hb) == nil {
				h.reg.ApplyHeartbeat(id, hb.LocalIPs, hb.OSUser)
			} else {
				h.reg.Touch(id)
			}
			h.notifyUI()
		case protocol.TypeRouteEvent:
			var ev protocol.RouteEvent
			if json.Unmarshal(data, &ev) == nil {
				h.routes.HandleEvent(id, &ev)
				h.notifyUI()
			}
		case protocol.TypeRouteTunnelAck:
			var ack protocol.RouteTunnelAck
			if json.Unmarshal(data, &ack) == nil {
				h.routes.HandleTunnelAck(id, &ack)
			}
		case protocol.TypeRouteTunnelClose:
			var msg protocol.RouteTunnelClose
			if json.Unmarshal(data, &msg) == nil && msg.TunnelID != "" {
				h.routes.closeTunnelFromAgent(msg.TunnelID)
			}
		case protocol.TypeRelayEvent:
			var ev protocol.RelayEvent
			if json.Unmarshal(data, &ev) == nil {
				h.relays.HandleEvent(id, &ev)
				h.notifyUI()
			}
		case protocol.TypeForwardTunnelConnect:
			var msg protocol.ForwardTunnelConnect
			if json.Unmarshal(data, &msg) == nil {
				h.routes.HandleForwardTunnelConnect(id, &msg)
			}
		}
	}
}

func (h *Hub) agentWritePump(s *agentSession, done <-chan struct{}) {
	for {
		select {
		case <-done:
			return
		case b, ok := <-s.send:
			if !ok {
				return
			}
			_ = s.conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
			if err := s.conn.WriteMessage(b.msgType, b.data); err != nil {
				return
			}
		}
	}
}

func (h *Hub) agentPingLoop(s *agentSession, done <-chan struct{}) {
	t := time.NewTicker(serverPingInterval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			s.mu.Lock()
			if s.awaitingAck {
				s.missStreak++
				if s.missStreak >= serverPingMissDead {
					s.mu.Unlock()
					_ = s.conn.Close()
					return
				}
			}
			s.pingSeq++
			seq := s.pingSeq
			s.awaitingAck = true
			s.mu.Unlock()
			if !h.SendAgent(s.id, protocol.Ping{Type: protocol.TypePing, Seq: seq}) {
				s.mu.Lock()
				s.awaitingAck = false
				s.mu.Unlock()
			}
		}
	}
}

func (h *Hub) RegisterUI(c *websocket.Conn) {
	h.mu.Lock()
	h.ui[c] = struct{}{}
	h.mu.Unlock()
	if b, err := h.uiSnapshotJSON(); err == nil {
		_ = c.WriteMessage(websocket.TextMessage, b)
	}
}

func (h *Hub) UnregisterUI(c *websocket.Conn) {
	h.mu.Lock()
	delete(h.ui, c)
	h.mu.Unlock()
}

func (h *Hub) RunUIBroadcast() {
	for b := range h.uib {
		h.mu.RLock()
		conns := make([]*websocket.Conn, 0, len(h.ui))
		for c := range h.ui {
			conns = append(conns, c)
		}
		h.mu.RUnlock()
		for _, c := range conns {
			_ = c.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
				h.mu.Lock()
				delete(h.ui, c)
				h.mu.Unlock()
				_ = c.Close()
			}
		}
	}
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
