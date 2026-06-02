package protocol

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const TunnelKeyLen = 8

// TunnelKey is the 8-byte tunnel id carried in binary WS frames.
type TunnelKey [TunnelKeyLen]byte

func NewTunnelKey() (TunnelKey, string, error) {
	var k TunnelKey
	if _, err := rand.Read(k[:]); err != nil {
		return k, "", err
	}
	return k, hex.EncodeToString(k[:]), nil
}

func TunnelKeyFromHex(s string) (TunnelKey, error) {
	var k TunnelKey
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != TunnelKeyLen {
		return k, fmt.Errorf("invalid tunnel id")
	}
	copy(k[:], b)
	return k, nil
}

func WrapTunnelFrame(key TunnelKey, payload []byte) []byte {
	out := make([]byte, TunnelKeyLen+len(payload))
	copy(out[:TunnelKeyLen], key[:])
	copy(out[TunnelKeyLen:], payload)
	return out
}

func UnwrapTunnelFrame(data []byte) (TunnelKey, []byte, bool) {
	if len(data) < TunnelKeyLen {
		return TunnelKey{}, nil, false
	}
	var k TunnelKey
	copy(k[:], data[:TunnelKeyLen])
	return k, data[TunnelKeyLen:], true
}
