package server

import (
	"testing"
	"time"
)

func TestFindIDByPersistentIDPrefersOnline(t *testing.T) {
	r := NewRegistry()
	r.Register("old-offline", &ClientRecord{
		PersistentID: "pid-1", Hostname: "h", Online: false,
		LastSeen: time.Now().UTC().Add(-time.Hour),
	})
	r.SetOffline("old-offline")
	r.Register("live", &ClientRecord{
		PersistentID: "pid-1", Hostname: "h", Online: true,
	})
	id, ok := r.FindIDByPersistentID("pid-1")
	if !ok || id != "live" {
		t.Fatalf("got id=%q ok=%v", id, ok)
	}
}

func TestPurgeDuplicatePersistentID(t *testing.T) {
	r := NewRegistry()
	r.Register("keep", &ClientRecord{PersistentID: "pid-1", Hostname: "a"})
	r.Register("dup1", &ClientRecord{PersistentID: "pid-1", Hostname: "a"})
	r.Register("dup2", &ClientRecord{PersistentID: "pid-1", Hostname: "a"})
	r.Register("other", &ClientRecord{PersistentID: "pid-2", Hostname: "b"})

	r.PurgeDuplicatePersistentID("keep", "pid-1")

	if len(r.Snapshot()) != 2 {
		t.Fatalf("clients=%d", len(r.Snapshot()))
	}
	if _, ok := r.Get("dup1"); ok {
		t.Fatal("dup1 should be removed")
	}
	if _, ok := r.Get("keep"); !ok {
		t.Fatal("keep should remain")
	}
}

func TestDedupeAllPersistentIDs(t *testing.T) {
	r := NewRegistry()
	r.Register("a", &ClientRecord{PersistentID: "pid", Hostname: "h1"})
	r.Register("b", &ClientRecord{PersistentID: "pid", Hostname: "h2"})
	r.Register("c", &ClientRecord{PersistentID: "other", Hostname: "x"})
	r.DedupeAllPersistentIDs()
	if len(r.Snapshot()) != 2 {
		t.Fatalf("clients=%d", len(r.Snapshot()))
	}
}

func TestReconnectReusesClientID(t *testing.T) {
	reg := NewRegistry()
	catalog, _ := LoadRouteCatalog(t.TempDir() + "/cat.json")
	hub := NewHub(reg, catalog)

	pid := "stable-pid-abc"
	reg.Register("client-1", &ClientRecord{
		PersistentID: pid, Hostname: "DC01", OSUser: "admin",
	})
	reg.SetOffline("client-1")

	id, ok := reg.FindIDByPersistentID(pid)
	if !ok || id != "client-1" {
		t.Fatalf("find=%q ok=%v", id, ok)
	}

	reg.Register("client-1", &ClientRecord{
		PersistentID: pid, Hostname: "DC01", OSUser: "admin",
	})
	reg.PurgeDuplicatePersistentID("client-1", pid)

	if len(reg.Snapshot()) != 1 {
		t.Fatalf("expected 1 client, got %d", len(reg.Snapshot()))
	}
	_ = hub
}
