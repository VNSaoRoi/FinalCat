package server

import (
	"testing"

	"finalcat/internal/protocol"
)

func testRouteManager(t *testing.T) *RouteManager {
	t.Helper()
	reg := NewRegistry()
	catalog, err := LoadRouteCatalog(t.TempDir() + "/route-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(reg, catalog)
	return hub.routes
}

func TestRouteManagerHandleEvent(t *testing.T) {
	rm := testRouteManager(t)

	rm.HandleEvent("agent1", &protocol.RouteEvent{
		Type:       protocol.TypeRouteEvent,
		RouteID:    "r1",
		Kind:       protocol.RouteKindForward,
		State:      protocol.RouteStateActive,
		ListenAddr: "0.0.0.0:4444",
		Target:     "10.0.0.5:3389",
	})

	snap := rm.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("routes: %d", len(snap))
	}
	if snap[0].State != protocol.RouteStateActive {
		t.Fatalf("state=%s", snap[0].State)
	}
	if snap[0].AgentID != "agent1" {
		t.Fatalf("agent=%s", snap[0].AgentID)
	}

	rm.setState("r1", protocol.RouteStateClosed, "test")
	rec, ok := rm.Get("r1")
	if !ok || rec.State != protocol.RouteStateClosed {
		t.Fatalf("close state=%v ok=%v", rec, ok)
	}
}

func TestRouteManagerAgentDisconnectedSuspendsLiveRoutes(t *testing.T) {
	rm := testRouteManager(t)
	reg := rm.hub.reg

	reg.Register("a1", &ClientRecord{PersistentID: "pid-a1", Hostname: "host"})

	rm.mu.Lock()
	rm.routes["r1"] = &RouteRecord{
		ID: "r1", AgentID: "a1", Kind: protocol.RouteKindForward,
		ListenAddr: "0.0.0.0:4444", Target: "10.0.0.5:3389",
		State: protocol.RouteStateActive,
	}
	rm.mu.Unlock()

	rm.AgentDisconnected("a1")

	if _, ok := rm.Get("r1"); ok {
		t.Fatal("live route should be removed on disconnect")
	}
	saved := rm.catalog.Routes("pid-a1")
	if len(saved) != 1 || saved[0].ID != "r1" {
		t.Fatalf("catalog routes=%+v", saved)
	}
}

func TestRouteManagerRestoreDesired(t *testing.T) {
	rm := testRouteManager(t)
	reg := rm.hub.reg

	reg.Register("a1", &ClientRecord{PersistentID: "pid-restore", Hostname: "host"})
	rm.hub.mu.Lock()
	rm.hub.agents["a1"] = &agentSession{id: "a1", send: make(chan agentOutMsg, 4)}
	rm.hub.mu.Unlock()

	rm.catalog.UpsertRoute("pid-restore", DesiredRoute{
		ID: "r1", Kind: protocol.RouteKindForward,
		ListenAddr: "0.0.0.0:4444", TargetHost: "10.0.0.5", TargetPort: 3389,
	})

	rm.RestoreDesired("a1", "pid-restore")

	rm.mu.RLock()
	rec, ok := rm.routes["r1"]
	rm.mu.RUnlock()
	if !ok {
		t.Fatal("route not restored")
	}
	if rec.State != protocol.RouteStatePending {
		t.Fatalf("state=%s", rec.State)
	}
	if rec.AgentID != "a1" {
		t.Fatalf("agent=%s", rec.AgentID)
	}
}
