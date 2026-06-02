package server

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

type ListenerRecord struct {
	Address string `json:"address"`
	Role    string `json:"role,omitempty"`
	State   string `json:"state,omitempty"`
}

type ClientRecord struct {
	ID                 string           `json:"ID"`
	Hostname           string           `json:"Hostname"`
	OS                 string           `json:"OS"`
	GOArch             string           `json:"GOArch,omitempty"`
	OSUser             string           `json:"OSUser,omitempty"`
	LocalIPs           []string         `json:"LocalIPs,omitempty"`
	Listeners          []ListenerRecord `json:"Listeners,omitempty"`
	UpstreamEndpoints  []string         `json:"UpstreamEndpoints,omitempty"`
	ActiveUpstreamUsed string    `json:"ActiveUpstreamUsed,omitempty"`
	AgentMode          string    `json:"AgentMode,omitempty"`
	RemoteAddr         string    `json:"RemoteAddr,omitempty"`
	ConnectedAt        time.Time `json:"ConnectedAt"`
	LastSeen           time.Time `json:"LastSeen"`
	Revision           int64     `json:"Revision"`
	Online             bool      `json:"Online"`
	Listening          bool      `json:"Listening,omitempty"`
}

type Registry struct {
	mu      sync.RWMutex
	rev     atomic.Int64
	clients map[string]*ClientRecord
}

func NewRegistry() *Registry {
	return &Registry{clients: make(map[string]*ClientRecord)}
}

func (r *Registry) Register(id string, reg *ClientRecord) *ClientRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	rec, ok := r.clients[id]
	if !ok {
		rec = &ClientRecord{ID: id, ConnectedAt: now}
		r.clients[id] = rec
	}
	rec.Hostname = reg.Hostname
	rec.OS = reg.OS
	rec.GOArch = reg.GOArch
	rec.OSUser = reg.OSUser
	rec.LocalIPs = append([]string(nil), reg.LocalIPs...)
	rec.Listeners = append([]ListenerRecord(nil), reg.Listeners...)
	rec.UpstreamEndpoints = append([]string(nil), reg.UpstreamEndpoints...)
	rec.ActiveUpstreamUsed = reg.ActiveUpstreamUsed
	rec.AgentMode = reg.AgentMode
	rec.RemoteAddr = reg.RemoteAddr
	rec.Online = true
	rec.Listening = reg.Listening
	rec.LastSeen = now
	rec.Revision = r.rev.Add(1)
	return rec
}

func (r *Registry) Touch(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[id]; ok {
		c.LastSeen = time.Now().UTC()
		c.Online = true
		c.Revision = r.rev.Add(1)
	}
}

func (r *Registry) SetOffline(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[id]; ok {
		c.Online = false
		c.LastSeen = time.Now().UTC()
		c.Revision = r.rev.Add(1)
	}
}

func (r *Registry) SetUpstream(id string, eps []string) (*ClientRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[id]
	if !ok {
		return nil, false
	}
	c.UpstreamEndpoints = append([]string(nil), eps...)
	c.Revision = r.rev.Add(1)
	cp := *c
	return &cp, true
}

func (r *Registry) Get(id string) (*ClientRecord, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[id]
	if !ok {
		return nil, false
	}
	cp := *c
	cp.LocalIPs = append([]string(nil), c.LocalIPs...)
	cp.Listeners = append([]ListenerRecord(nil), c.Listeners...)
	cp.UpstreamEndpoints = append([]string(nil), c.UpstreamEndpoints...)
	return &cp, true
}

func (r *Registry) Snapshot() []ClientRecord {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ClientRecord, 0, len(r.clients))
	for _, c := range r.clients {
		cp := *c
		cp.LocalIPs = append([]string(nil), c.LocalIPs...)
		cp.Listeners = append([]ListenerRecord(nil), c.Listeners...)
		cp.UpstreamEndpoints = append([]string(nil), c.UpstreamEndpoints...)
		out = append(out, cp)
	}
	return out
}

func (r *Registry) SnapshotJSON() ([]byte, error) {
	return json.Marshal(r.Snapshot())
}
