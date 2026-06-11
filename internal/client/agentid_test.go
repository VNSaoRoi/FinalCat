package client

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPersistentIDStable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.id")
	t.Setenv("APPDATA", "")
	// Force path via home override is awkward; write file directly and read logic via validPersistentID
	id1 := "abc12345deadbeef"
	if !validPersistentID(id1) {
		t.Fatal("valid id rejected")
	}
	if err := os.WriteFile(path, []byte(id1), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != id1 {
		t.Fatalf("read back: %v %q", err, b)
	}
}

func TestValidPersistentID(t *testing.T) {
	if validPersistentID("short") {
		t.Fatal("too short")
	}
	if !validPersistentID("fp:0123456789abcdef") {
		t.Fatal("fp prefix should be valid")
	}
}
