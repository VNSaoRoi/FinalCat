package client

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"sync"

	"finalcat/internal/protocol"
)

type relayManager struct {
	mu       sync.Mutex
	clientID string
	ws       *wsWriter
	parent   context.Context
	cancel   context.CancelFunc
	active   bool
}

func newRelayManager(ctx context.Context, ws *wsWriter, clientID string) *relayManager {
	return &relayManager{clientID: clientID, ws: ws, parent: ctx}
}

func (m *relayManager) closeAll() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.active = false
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (m *relayManager) handle(data []byte) bool {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(data, &head) != nil {
		return false
	}
	switch head.Type {
	case protocol.TypeRelayOpen:
		var msg protocol.RelayOpen
		if json.Unmarshal(data, &msg) != nil || msg.ListenAddr == "" || msg.TargetAddr == "" {
			return true
		}
		go m.open(msg.ListenAddr, msg.TargetAddr)
		return true
	case protocol.TypeRelayClose:
		m.closeAll()
		m.emit(protocol.RelayStateClosed, "", "", "closed by server")
		return true
	default:
		return false
	}
}

func (m *relayManager) open(listenAddr, targetAddr string) {
	m.closeAll()

	ctx, cancel := context.WithCancel(m.parent)
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		cancel()
		m.emit(protocol.RelayStateError, listenAddr, targetAddr, err.Error())
		return
	}

	m.mu.Lock()
	m.cancel = cancel
	m.active = true
	m.mu.Unlock()

	m.emit(protocol.RelayStateActive, listenAddr, targetAddr, "")
	log.Printf("relay pivot active listen=%s -> %s", listenAddr, targetAddr)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	go func() {
		for {
			in, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					m.closeAll()
					m.emit(protocol.RelayStateError, listenAddr, targetAddr, err.Error())
					return
				}
			}
			go m.forwardOnce(ctx, in, targetAddr)
		}
	}()
}

func (m *relayManager) forwardOnce(ctx context.Context, in net.Conn, target string) {
	defer in.Close()
	dialer := net.Dialer{}
	out, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		log.Printf("relay dial %s: %v", target, err)
		return
	}
	defer out.Close()
	relayTCP(in, out)
}

func (m *relayManager) emit(state, listenAddr, targetAddr, message string) {
	ev, err := json.Marshal(protocol.RelayEvent{
		Type:       protocol.TypeRelayEvent,
		ClientID:   m.clientID,
		State:      state,
		ListenAddr: listenAddr,
		TargetAddr: targetAddr,
		Message:    message,
	})
	if err != nil {
		return
	}
	_ = m.ws.WriteText(ev)
}
