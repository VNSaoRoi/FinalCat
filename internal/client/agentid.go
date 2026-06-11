package client

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// LoadPersistentID returns a stable agent identity stored on disk (survives reconnects).
func LoadPersistentID() string {
	path := persistentIDPath()
	if b, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(b))
		if validPersistentID(id) {
			return id
		}
	}
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	id := hex.EncodeToString(raw[:])
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(id+"\n"), 0o600)
	return id
}

func persistentIDPath() string {
	if d := os.Getenv("APPDATA"); d != "" {
		return filepath.Join(d, "FinalCat", "agent.id")
	}
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Join(h, ".finalcat", "agent.id")
	}
	return "agent.id"
}

func validPersistentID(id string) bool {
	if len(id) < 8 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == ':':
		default:
			return false
		}
	}
	return true
}
