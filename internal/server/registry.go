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
	PersistentID       string           `json:"PersistentID,omitempty"`
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

// FindIDByPersistentID returns the canonical client ID for a stable agent identity.
// Prefers an online record; otherwise the most recently seen offline record.
func (r *Registry) FindIDByPersistentID(pid string) (string, bool) {
	if pid == "" {
		return "", false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var offlineID string
	var offlineSeen time.Time
	for id, c := range r.clients {
		if c.PersistentID != pid {
			continue
		}
		if c.Online {
			return id, true
		}
		if offlineID == "" || c.LastSeen.After(offlineSeen) {
			offlineID = id
			offlineSeen = c.LastSeen
		}
	}
	if offlineID != "" {
		return offlineID, true
	}
	return "", false
}

// PurgeDuplicatePersistentID removes extra registry rows sharing the same persistent_id.
func (r *Registry) PurgeDuplicatePersistentID(keepID, pid string) {
	if pid == "" || keepID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, c := range r.clients {
		if id != keepID && c.PersistentID == pid {
			delete(r.clients, id)
			r.rev.Add(1)
		}
	}
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
	if reg.PersistentID != "" {
		rec.PersistentID = reg.PersistentID
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

func (r *Registry) ApplyHeartbeat(id string, localIPs []string, osUser string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.clients[id]
	if !ok {
		return
	}
	c.LastSeen = time.Now().UTC()
	c.Online = true
	if len(localIPs) > 0 {
		c.LocalIPs = append([]string(nil), localIPs...)
	}
	if osUser != "" {
		c.OSUser = osUser
	}
	c.Revision = r.rev.Add(1)
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

// DedupeAllPersistentIDs keeps one registry row per persistent_id (online preferred, else newest).
func (r *Registry) DedupeAllPersistentIDs() {
	r.mu.Lock()
	defer r.mu.Unlock()
	groups := make(map[string][]string)
	for id, c := range r.clients {
		if c.PersistentID != "" {
			groups[c.PersistentID] = append(groups[c.PersistentID], id)
		}
	}
	changed := false
	for _, ids := range groups {
		if len(ids) < 2 {
			continue
		}
		keep := ids[0]
		for _, id := range ids[1:] {
			kc, ic := r.clients[keep], r.clients[id]
			if ic.Online && !kc.Online {
				keep = id
			} else if ic.Online == kc.Online && ic.LastSeen.After(kc.LastSeen) {
				keep = id
			}
		}
		for _, id := range ids {
			if id != keep {
				delete(r.clients, id)
				changed = true
			}
		}
	}
	if changed {
		r.rev.Add(1)
	}
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
