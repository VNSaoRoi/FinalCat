package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

func (s *App) handleBindAttach(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req BindAttachRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	port := req.BindPort
	if port <= 0 {
		port = 31747
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	id, err := s.hub.AttachBind(ctx, req.BindHost, port, req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, BindAttachResponse{OK: true, ClientID: id, Message: "bind agent attached"})
}
