package server

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *App) mountAdminAPI(mux *http.ServeMux) {
	mux.HandleFunc("/api/auth/status", s.handleAuthStatus)
	mux.HandleFunc("/api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("/api/clients", s.auth.Wrap(s.handleClients))
	mux.HandleFunc("/api/clients/", s.auth.Wrap(s.handleClientSub))
	mux.HandleFunc("/api/jobs/bind-attach", s.auth.Wrap(s.handleBindAttach))
	s.mountRouteAPI(mux)
}

func (s *App) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	enabled := s.authEnabled()
	writeJSON(w, map[string]any{
		"auth_enabled":  enabled,
		"authenticated": !enabled || s.auth.Check(r),
	})
}

func (s *App) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !s.auth.Login(body.Password) {
		http.Error(w, "invalid password", http.StatusUnauthorized)
		return
	}
	if sa, ok := s.auth.(*SessionAuth); ok {
		tok := randomID()
		sa.SetSession(tok)
		http.SetCookie(w, &http.Cookie{
			Name: sessionCookie, Value: tok, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *App) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if sa, ok := s.auth.(*SessionAuth); ok {
			sa.ClearSession(c.Value)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *App) handleClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.hub.reg.Snapshot())
}

func (s *App) handleClientSub(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/clients/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	switch parts[1] {
	case "relay":
		s.handleClientRelay(w, r, id)
		return
	case "upstreams":
	default:
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.hub.AgentOnline(id) {
		http.Error(w, "agent offline", http.StatusBadGateway)
		return
	}
	var body struct {
		Endpoints []string `json:"endpoints"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Endpoints) == 0 {
		http.Error(w, "endpoints required", http.StatusBadRequest)
		return
	}
	rec, ok := s.hub.reg.SetUpstream(id, body.Endpoints)
	if !ok {
		http.Error(w, "unknown client", http.StatusNotFound)
		return
	}
	if !s.hub.PushUpstream(id, body.Endpoints, rec.Revision) {
		http.Error(w, "push to agent failed", http.StatusBadGateway)
		return
	}
	s.hub.notifyUI()
	writeJSON(w, map[string]any{"ok": true, "clientId": id, "revision": rec.Revision})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *App) authEnabled() bool {
	_, ok := s.auth.(NoopAuth)
	return !ok
}
