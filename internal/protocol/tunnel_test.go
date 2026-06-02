package protocol

import (
	"bytes"
	"testing"
)

func TestTunnelFrameRoundTrip(t *testing.T) {
	key, hexID, err := NewTunnelKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(hexID) != 16 {
		t.Fatalf("hex id len=%d", len(hexID))
	}
	payload := []byte("hello-tunnel")
	frame := WrapTunnelFrame(key, payload)
	k2, p2, ok := UnwrapTunnelFrame(frame)
	if !ok || !bytes.Equal(key[:], k2[:]) || !bytes.Equal(payload, p2) {
		t.Fatalf("roundtrip failed ok=%v", ok)
	}
}

func TestTunnelKeyFromHex(t *testing.T) {
	key, id, _ := NewTunnelKey()
	k2, err := TunnelKeyFromHex(id)
	if err != nil || key != k2 {
		t.Fatalf("from hex: %v", err)
	}
}
