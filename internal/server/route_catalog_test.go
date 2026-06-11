package server

import (
	"path/filepath"
	"testing"

	"finalcat/internal/protocol"
)

func TestRouteCatalogPersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "route-catalog.json")
	c, err := LoadRouteCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	c.UpsertRoute("pid1", DesiredRoute{
		ID: "r1", Kind: protocol.RouteKindForward,
		ListenAddr: "0.0.0.0:4444", TargetHost: "10.0.0.5", TargetPort: 3389,
	})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2, err := LoadRouteCatalog(path)
	if err != nil {
		t.Fatal(err)
	}
	routes := c2.Routes("pid1")
	if len(routes) != 1 || routes[0].TargetPort != 3389 {
		t.Fatalf("routes=%+v", routes)
	}
	c2.RemoveRoute("pid1", "r1")
	if len(c2.Routes("pid1")) != 0 {
		t.Fatal("remove failed")
	}
}

func TestDerivePersistentIDStable(t *testing.T) {
	a := DerivePersistentID("host", "linux", "amd64", "root", []string{"10.0.0.2", "10.0.0.1"})
	b := DerivePersistentID("host", "linux", "amd64", "root", []string{"10.0.0.1", "10.0.0.2"})
	if a != b {
		t.Fatalf("unstable fingerprint: %s vs %s", a, b)
	}
}
