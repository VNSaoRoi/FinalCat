package server

import (
	"encoding/json"
	"net/http"

	"finalcat/internal/protocol"
)

func (s *App) handleClientRelay(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodPost:
		s.handleRelayOpen(w, r, id)
	case http.MethodDelete:
		s.handleRelayClose(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *App) handleRelayOpen(w http.ResponseWriter, r *http.Request, id string) {
	var body struct {
		ListenIP   string `json:"listen_ip"`
		ListenPort int    `json:"listen_port"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || body.ListenIP == "" {
		http.Error(w, "listen_ip required", http.StatusBadRequest)
		return
	}
	port := body.ListenPort
	if port == 0 {
		port = protocol.DefaultRelayPort
	}
	if err := s.hub.relays.Open(id, body.ListenIP, port); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.hub.notifyUI()
	writeJSON(w, map[string]any{"ok": true, "client_id": id})
}

func (s *App) handleRelayClose(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.hub.relays.Close(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.hub.notifyUI()
	writeJSON(w, map[string]any{"ok": true, "client_id": id})
}
