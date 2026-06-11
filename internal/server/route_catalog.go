package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"finalcat/internal/protocol"
)

// DesiredRoute is persisted operator intent — restored after agent reconnect.
type DesiredRoute struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	ListenAddr string `json:"listen_addr"`
	Target     string `json:"target,omitempty"`
	TargetHost string `json:"target_host,omitempty"`
	TargetPort int    `json:"target_port,omitempty"`
	BindOn     string `json:"bind_on,omitempty"`
}

type CatalogAgent struct {
	PersistentID string         `json:"persistent_id"`
	Hostname     string         `json:"hostname"`
	OSUser       string         `json:"os_user,omitempty"`
	LastClientID string         `json:"last_client_id,omitempty"`
	LastSeen     time.Time      `json:"last_seen"`
	Routes       []DesiredRoute `json:"routes"`
}

type routeCatalogFile struct {
	Agents map[string]*CatalogAgent `json:"agents"`
}

type RouteCatalog struct {
	mu   sync.Mutex
	path string
	data routeCatalogFile
}

func LoadRouteCatalog(path string) (*RouteCatalog, error) {
	c := &RouteCatalog{
		path: path,
		data: routeCatalogFile{Agents: make(map[string]*CatalogAgent)},
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if len(b) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(b, &c.data); err != nil {
		return nil, fmt.Errorf("parse route catalog: %w", err)
	}
	if c.data.Agents == nil {
		c.data.Agents = make(map[string]*CatalogAgent)
	}
	return c, nil
}

func (c *RouteCatalog) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.saveLocked()
}

func (c *RouteCatalog) saveLocked() error {
	if c.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *RouteCatalog) agentLocked(pid string) *CatalogAgent {
	a, ok := c.data.Agents[pid]
	if !ok {
		a = &CatalogAgent{PersistentID: pid, Routes: []DesiredRoute{}}
		c.data.Agents[pid] = a
	}
	return a
}

func (c *RouteCatalog) TouchAgent(pid, clientID, hostname, osUser string) {
	if pid == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.agentLocked(pid)
	a.LastClientID = clientID
	a.Hostname = hostname
	a.OSUser = osUser
	a.LastSeen = time.Now().UTC()
}

func (c *RouteCatalog) UpsertRoute(pid string, dr DesiredRoute) {
	if pid == "" || dr.ID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.agentLocked(pid)
	for i, r := range a.Routes {
		if r.ID == dr.ID {
			a.Routes[i] = dr
			return
		}
	}
	a.Routes = append(a.Routes, dr)
}

func (c *RouteCatalog) RemoveRoute(pid, routeID string) {
	if pid == "" || routeID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	a, ok := c.data.Agents[pid]
	if !ok {
		return
	}
	out := a.Routes[:0]
	for _, r := range a.Routes {
		if r.ID != routeID {
			out = append(out, r)
		}
	}
	a.Routes = out
}

func (c *RouteCatalog) Routes(pid string) []DesiredRoute {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, ok := c.data.Agents[pid]
	if !ok || len(a.Routes) == 0 {
		return nil
	}
	out := make([]DesiredRoute, len(a.Routes))
	copy(out, a.Routes)
	return out
}

func desiredFromRecord(rec *RouteRecord) DesiredRoute {
	dr := DesiredRoute{
		ID:         rec.ID,
		Kind:       rec.Kind,
		ListenAddr: rec.ListenAddr,
		Target:     rec.Target,
		BindOn:     rec.BindOn,
	}
	if rec.Kind == protocol.RouteKindForward && rec.Target != "" {
		host, port := splitTargetHostPort(rec.Target)
		dr.TargetHost = host
		dr.TargetPort = port
	}
	return dr
}

func splitTargetHostPort(target string) (string, int) {
	i := len(target) - 1
	for i >= 0 && target[i] != ':' {
		i--
	}
	if i <= 0 {
		return target, 0
	}
	host := target[:i]
	var port int
	_, _ = fmt.Sscanf(target[i+1:], "%d", &port)
	return host, port
}

// DerivePersistentID fingerprints an agent when it has no on-disk persistent_id (legacy agents).
func DerivePersistentID(hostname, osName, goarch, osUser string, localIPs []string) string {
	ips := append([]string(nil), localIPs...)
	sort.Strings(ips)
	raw := fmt.Sprintf("%s|%s|%s|%s|%s", hostname, osName, goarch, osUser, strings.Join(ips, ","))
	sum := sha256.Sum256([]byte(raw))
	return "fp:" + fmt.Sprintf("%x", sum)[:32]
}
