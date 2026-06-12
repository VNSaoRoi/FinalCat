package server

import (
	"fmt"
	"time"

	"finalcat/internal/protocol"
)

type smartRouteMeta struct {
	listenAgentID string
	egressAgentID string
	targetHost    string
	targetPort    int
}

func (rm *RouteManager) OpenForwardSmart(listenAgentID, egressAgentID, listenAddr, targetHost string, targetPort int) (*RouteRecord, error) {
	return rm.openForwardSmart(listenAgentID, egressAgentID, newRouteID(), listenAddr, targetHost, targetPort)
}

func (rm *RouteManager) openForwardSmart(listenAgentID, egressAgentID, routeID, listenAddr, targetHost string, targetPort int) (*RouteRecord, error) {
	if listenAddr == "" || targetHost == "" || targetPort <= 0 || targetPort > 65535 {
		return nil, fmt.Errorf("listen_addr, target_host, target_port required")
	}
	if egressAgentID == "" {
		egressAgentID = listenAgentID
	}
	if !rm.hub.AgentOnline(listenAgentID) {
		return nil, fmt.Errorf("listen agent offline")
	}
	if !rm.hub.AgentOnline(egressAgentID) {
		return nil, fmt.Errorf("egress agent offline")
	}
	target := fmt.Sprintf("%s:%d", targetHost, targetPort)
	rec := &RouteRecord{
		ID:            routeID,
		AgentID:       listenAgentID,
		EgressAgentID: egressAgentID,
		Kind:          protocol.RouteKindForwardSmart,
		ListenAddr:    listenAddr,
		Target:        target,
		State:         protocol.RouteStatePending,
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	if c, ok := rm.hub.reg.Get(listenAgentID); ok {
		rec.AgentHost = c.Hostname
	}
	rm.mu.Lock()
	rm.routes[routeID] = rec
	if rm.smartRoutes == nil {
		rm.smartRoutes = make(map[string]*smartRouteMeta)
	}
	rm.smartRoutes[routeID] = &smartRouteMeta{
		listenAgentID: listenAgentID,
		egressAgentID: egressAgentID,
		targetHost:    targetHost,
		targetPort:    targetPort,
	}
	rm.mu.Unlock()
	if !rm.hub.SendAgent(listenAgentID, protocol.RouteOpenForwardSmart{
		Type: protocol.TypeRouteOpenForwardSmart, RouteID: routeID, ListenAddr: listenAddr,
	}) {
		rm.removeSmartRoute(routeID)
		return nil, fmt.Errorf("failed to send route command to listen agent")
	}
	rm.persistRoute(listenAgentID, rec)
	return rec, nil
}

func (rm *RouteManager) removeSmartRoute(routeID string) {
	rm.mu.Lock()
	delete(rm.routes, routeID)
	delete(rm.smartRoutes, routeID)
	rm.mu.Unlock()
}

func (rm *RouteManager) HandleForwardTunnelConnect(listenAgentID string, msg *protocol.ForwardTunnelConnect) {
	if msg == nil || msg.RouteID == "" || msg.TunnelID == "" {
		return
	}
	rm.mu.RLock()
	meta, ok := rm.smartRoutes[msg.RouteID]
	rm.mu.RUnlock()
	if !ok || meta.listenAgentID != listenAgentID {
		rm.sendForwardTunnelReady(listenAgentID, msg.RouteID, msg.TunnelID, false, "unknown route")
		return
	}
	key, err := protocol.TunnelKeyFromHex(msg.TunnelID)
	if err != nil {
		rm.sendForwardTunnelReady(listenAgentID, msg.RouteID, msg.TunnelID, false, err.Error())
		return
	}
	wait := rm.registerTunnelWait(msg.TunnelID)
	if !rm.hub.SendAgent(meta.egressAgentID, protocol.RouteTunnelOpen{
		Type:       protocol.TypeRouteTunnelOpen,
		RouteID:    msg.RouteID,
		TunnelID:   msg.TunnelID,
		TargetHost: meta.targetHost,
		TargetPort: meta.targetPort,
	}) {
		rm.dropTunnelWait(msg.TunnelID)
		rm.sendForwardTunnelReady(listenAgentID, msg.RouteID, msg.TunnelID, false, "egress unreachable")
		return
	}
	var ack tunnelAckResult
	select {
	case ack = <-wait:
	case <-time.After(20 * time.Second):
		rm.dropTunnelWait(msg.TunnelID)
		rm.sendForwardTunnelReady(listenAgentID, msg.RouteID, msg.TunnelID, false, "egress tunnel timeout")
		return
	}
	if !ack.ok {
		rm.sendForwardTunnelReady(listenAgentID, msg.RouteID, msg.TunnelID, false, ack.message)
		return
	}
	rm.mu.Lock()
	rm.tunnels[msg.TunnelID] = &activeTunnel{
		listenAgentID: listenAgentID,
		egressAgentID: meta.egressAgentID,
		routeID:       msg.RouteID,
		tunnelID:      msg.TunnelID,
		tunnelKey:     key,
	}
	rm.mu.Unlock()
	rm.sendForwardTunnelReady(listenAgentID, msg.RouteID, msg.TunnelID, true, "")
}

func (rm *RouteManager) sendForwardTunnelReady(listenAgentID, routeID, tunnelID string, ok bool, message string) {
	rm.hub.SendAgent(listenAgentID, protocol.ForwardTunnelReady{
		Type: protocol.TypeForwardTunnelReady, RouteID: routeID, TunnelID: tunnelID, OK: ok, Message: message,
	})
}

func (rm *RouteManager) closeSmartTunnelsForRoute(routeID string) {
	rm.mu.Lock()
	var ids []string
	for id, t := range rm.tunnels {
		if t.routeID == routeID && t.listenAgentID != "" {
			ids = append(ids, id)
		}
	}
	rm.mu.Unlock()
	for _, id := range ids {
		rm.mu.RLock()
		t := rm.tunnels[id]
		rm.mu.RUnlock()
		if t != nil {
			rm.closeAgentTunnel(t.egressAgentID, routeID, id)
		}
	}
}
