package server

import (
	"log"
	"net"
	"net/http"
	"path/filepath"

	"github.com/gorilla/websocket"
)

var uiUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

type App struct {
	hub           *Hub
	auth          Authenticator
	controlListen string
	adminListen   string
}

func NewApp(controlListen, adminListen, password, dataDir string) *App {
	reg := NewRegistry()
	catalogPath := filepath.Join(dataDir, "route-catalog.json")
	catalog, err := LoadRouteCatalog(catalogPath)
	if err != nil {
		log.Fatalf("route catalog: %v", err)
	}
	hub := NewHub(reg, catalog)
	app := &App{
		hub:           hub,
		auth:          NewSessionAuth(password),
		controlListen: controlListen,
		adminListen:   adminListen,
	}
	go hub.RunUIBroadcast()
	return app
}

func (s *App) Run() error {
	agentMux := http.NewServeMux()
	agentMux.HandleFunc("/ws/agent", func(w http.ResponseWriter, r *http.Request) {
		c, err := agentUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		remote := r.RemoteAddr
		if host, _, err := net.SplitHostPort(remote); err == nil {
			remote = host
		}
		s.hub.HandleAgentWS(c, remote)
	})

	adminMux := http.NewServeMux()
	s.mountAdminAPI(adminMux)
	adminMux.HandleFunc("/ws/ui", s.handleUIWS)
	adminMux.Handle("/", http.FileServer(uiFS()))

	go func() {
		log.Printf("agent control listening on %s", s.controlListen)
		if err := http.ListenAndServe(s.controlListen, agentMux); err != nil {
			log.Fatalf("control listen: %v", err)
		}
	}()

	log.Printf("operator admin listening on %s (loopback only)", s.adminListen)
	return http.ListenAndServe(s.adminListen, adminMux)
}

func (s *App) handleUIWS(w http.ResponseWriter, r *http.Request) {
	if s.authEnabled() && !s.auth.Check(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	c, err := uiUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.hub.RegisterUI(c)
	defer s.hub.UnregisterUI(c)
	for {
		if _, _, err := c.ReadMessage(); err != nil {
			return
		}
	}
}

// ParseAdminListen forces 127.0.0.1 for operator UI.
func ParseAdminListen(addr string) (string, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if port == "" {
		port = "31891"
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}
