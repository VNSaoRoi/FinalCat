package server

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"

	"finalcat/internal/protocol"
)

type relayDesired struct {
	ListenIP   string
	ListenPort int
}

type RelayManager struct {
	hub    *Hub
	target string
	mu     sync.Mutex
	want   map[string]relayDesired
}

func NewRelayManager(h *Hub, controlAdvertise string) *RelayManager {
	return &RelayManager{
		hub:    h,
		target: controlAdvertise,
		want:   make(map[string]relayDesired),
	}
}

func (rm *RelayManager) ControlAdvertise() string {
	return rm.target
}

func (rm *RelayManager) Open(agentID, listenIP string, listenPort int) error {
	if listenPort <= 0 {
		listenPort = protocol.DefaultRelayPort
	}
	if listenPort > 65535 {
		return fmt.Errorf("invalid listen_port")
	}
	if listenIP == "" {
		return fmt.Errorf("listen_ip required")
	}
	if !rm.hub.AgentOnline(agentID) {
		return fmt.Errorf("agent offline")
	}
	rec, ok := rm.hub.reg.Get(agentID)
	if !ok {
		return fmt.Errorf("unknown client")
	}
	if listenIP != "0.0.0.0" && !agentHasIP(rec, listenIP) {
		return fmt.Errorf("listen_ip not on agent")
	}
	listenAddr := net.JoinHostPort(listenIP, strconv.Itoa(listenPort))
	if !rm.hub.SendAgent(agentID, protocol.RelayOpen{
		Type:       protocol.TypeRelayOpen,
		ListenAddr: listenAddr,
		TargetAddr: rm.target,
	}) {
		return fmt.Errorf("push to agent failed")
	}
	rm.mu.Lock()
	rm.want[agentID] = relayDesired{ListenIP: listenIP, ListenPort: listenPort}
	rm.mu.Unlock()
	rm.hub.reg.SetRelayPivot(agentID, &RelayPivotRecord{
		Active:     false,
		ListenIP:   listenIP,
		ListenPort: listenPort,
		ListenAddr: listenAddr,
		Target:     rm.target,
	})
	return nil
}

func (rm *RelayManager) Close(agentID string) error {
	if !rm.hub.AgentOnline(agentID) {
		rm.mu.Lock()
		delete(rm.want, agentID)
		rm.mu.Unlock()
		rm.hub.reg.SetRelayPivot(agentID, nil)
		return nil
	}
	if !rm.hub.SendAgent(agentID, protocol.RelayClose{Type: protocol.TypeRelayClose}) {
		return fmt.Errorf("push to agent failed")
	}
	rm.mu.Lock()
	delete(rm.want, agentID)
	rm.mu.Unlock()
	rm.hub.reg.SetRelayPivot(agentID, nil)
	return nil
}

func (rm *RelayManager) OnAgentConnected(agentID string) {
	rm.mu.Lock()
	d, ok := rm.want[agentID]
	rm.mu.Unlock()
	if !ok {
		return
	}
	_ = rm.Open(agentID, d.ListenIP, d.ListenPort)
}

func (rm *RelayManager) AgentDisconnected(agentID string) {
	rec, ok := rm.hub.reg.Get(agentID)
	if !ok || rec.RelayPivot == nil {
		return
	}
	rp := *rec.RelayPivot
	rp.Active = false
	rm.hub.reg.SetRelayPivot(agentID, &rp)
}

func (rm *RelayManager) HandleEvent(agentID string, ev *protocol.RelayEvent) {
	switch ev.State {
	case protocol.RelayStateActive:
		host, portStr, err := net.SplitHostPort(ev.ListenAddr)
		if err != nil {
			host = ev.ListenAddr
		}
		port, _ := strconv.Atoi(portStr)
		target := ev.TargetAddr
		if target == "" {
			target = rm.target
		}
		rm.hub.reg.SetRelayPivot(agentID, &RelayPivotRecord{
			Active:     true,
			ListenIP:   host,
			ListenPort: port,
			ListenAddr: ev.ListenAddr,
			Target:     target,
		})
	case protocol.RelayStateClosed:
		rm.mu.Lock()
		_, keep := rm.want[agentID]
		rm.mu.Unlock()
		if !keep {
			rm.hub.reg.SetRelayPivot(agentID, nil)
		}
	case protocol.RelayStateError:
		rm.hub.reg.SetRelayPivot(agentID, &RelayPivotRecord{
			Active:     false,
			ListenAddr: ev.ListenAddr,
			Target:     ev.TargetAddr,
		})
	}
}

func agentHasIP(rec *ClientRecord, ip string) bool {
	for _, local := range rec.LocalIPs {
		if local == ip {
			return true
		}
	}
	remote := strings.TrimSpace(rec.RemoteAddr)
	if remote != "" {
		host := remote
		if h, _, err := net.SplitHostPort(remote); err == nil {
			host = h
		}
		if host == ip {
			return true
		}
	}
	return false
}
