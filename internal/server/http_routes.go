package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *App) mountRouteAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/routes", s.auth.Wrap(s.handleRoutes))
	mux.HandleFunc("/api/routes/socks", s.auth.Wrap(s.handleRouteOpenSocks))
	mux.HandleFunc("/api/routes/forward", s.auth.Wrap(s.handleRouteOpenForward))
	mux.HandleFunc("/api/routes/", s.auth.Wrap(s.handleRouteByID))
}

func (s *App) handleRoutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.hub.routes.Snapshot())
}

func (s *App) handleRouteOpenSocks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		AgentID  string `json:"agent_id"`
		BindAddr string `json:"bind_addr"`
		BindOn   string `json:"bind_on"` // agent (default) | server
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.AgentID == "" {
		http.Error(w, "agent_id required", http.StatusBadRequest)
		return
	}
	bindOn := strings.ToLower(strings.TrimSpace(body.BindOn))
	if bindOn == "" {
		bindOn = "agent"
	}
	if body.BindAddr == "" {
		if bindOn == "server" {
			body.BindAddr = "127.0.0.1:1080"
		} else {
			body.BindAddr = "0.0.0.0:1080"
		}
	}
	var rec *RouteRecord
	var err error
	switch bindOn {
	case "server":
		rec, err = s.hub.routes.OpenSocksServer(body.AgentID, body.BindAddr)
	case "agent":
		rec, err = s.hub.routes.OpenSocks(body.AgentID, body.BindAddr)
	default:
		http.Error(w, "bind_on must be agent or server", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.hub.notifyUI()
	writeJSON(w, rec)
}

func (s *App) handleRouteOpenForward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		AgentID       string `json:"agent_id"`
		EgressAgentID string `json:"egress_agent_id"`
		ListenAddr    string `json:"listen_addr"`
		TargetHost    string `json:"target_host"`
		TargetPort    int    `json:"target_port"`
		Mode          string `json:"mode"` // smart (default) | direct
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.AgentID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if body.ListenAddr == "" {
		body.ListenAddr = "0.0.0.0:4444"
	}
	mode := strings.ToLower(strings.TrimSpace(body.Mode))
	if mode == "" {
		mode = "smart"
	}
	var rec *RouteRecord
	var err error
	switch mode {
	case "direct":
		rec, err = s.hub.routes.OpenForward(body.AgentID, body.ListenAddr, body.TargetHost, body.TargetPort)
	default:
		egress := strings.TrimSpace(body.EgressAgentID)
		if egress == "" {
			egress = body.AgentID
		}
		rec, err = s.hub.routes.OpenForwardSmart(body.AgentID, egress, body.ListenAddr, body.TargetHost, body.TargetPort)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.hub.notifyUI()
	writeJSON(w, rec)
}

func (s *App) handleRouteByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/routes/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.hub.routes.Close(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.hub.notifyUI()
	writeJSON(w, map[string]any{"ok": true, "id": id})
}
