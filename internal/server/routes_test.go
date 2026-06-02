package server

import (
	"testing"

	"finalcat/internal/protocol"
)

func TestRouteManagerHandleEvent(t *testing.T) {
	reg := NewRegistry()
	hub := NewHub(reg)
	rm := hub.routes

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

func TestRouteManagerAgentDisconnected(t *testing.T) {
	reg := NewRegistry()
	hub := NewHub(reg)
	rm := hub.routes

	rm.mu.Lock()
	rm.routes["r1"] = &RouteRecord{
		ID: "r1", AgentID: "a1", State: protocol.RouteStateActive,
	}
	rm.mu.Unlock()

	rm.AgentDisconnected("a1")
	rec, _ := rm.Get("r1")
	if rec.State != protocol.RouteStateClosed {
		t.Fatalf("state=%s", rec.State)
	}
	if rec.Message != "agent disconnected" {
		t.Fatalf("msg=%s", rec.Message)
	}
}
