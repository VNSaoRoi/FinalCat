package server

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"finalcat/internal/protocol"
	"finalcat/internal/socks5"
)

type socksServerRoute struct {
	cancel context.CancelFunc
	ln     net.Listener
}

type tunnelWaiter struct {
	ch chan tunnelAckResult
}

type tunnelAckResult struct {
	ok      bool
	message string
}

type activeTunnel struct {
	egressAgentID string
	listenAgentID string
	routeID       string
	client        net.Conn
	tunnelID      string
	tunnelKey     protocol.TunnelKey
}

func (rm *RouteManager) OpenSocksServer(agentID, bindAddr string) (*RouteRecord, error) {
	return rm.openSocksServer(agentID, newRouteID(), bindAddr)
}

func (rm *RouteManager) openSocksServer(agentID, routeID, bindAddr string) (*RouteRecord, error) {
	if bindAddr == "" {
		bindAddr = "127.0.0.1:1080"
	}
	if !rm.hub.AgentOnline(agentID) {
		return nil, fmt.Errorf("agent offline")
	}
	id := routeID
	rec := &RouteRecord{
		ID:         id,
		AgentID:    agentID,
		Kind:       protocol.RouteKindSocksServer,
		ListenAddr: bindAddr,
		BindOn:     "server",
		State:      protocol.RouteStatePending,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	if c, ok := rm.hub.reg.Get(agentID); ok {
		rec.AgentHost = c.Hostname
	}

	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	sr := &socksServerRoute{cancel: cancel, ln: ln}

	rm.mu.Lock()
	rm.routes[id] = rec
	rm.socksSrv[id] = sr
	rm.mu.Unlock()

	go rm.serveSocksGateway(ctx, id, agentID, ln)

	rec.State = protocol.RouteStateActive
	rec.UpdatedAt = time.Now().UTC()
	log.Printf("route socks_server active id=%s listen=%s via agent=%s", id, bindAddr, agentID)
	rm.persistRoute(agentID, rec)
	rm.hub.notifyUI()
	return rec, nil
}

func (rm *RouteManager) serveSocksGateway(ctx context.Context, routeID, agentID string, ln net.Listener) {
	defer func() {
		_ = ln.Close()
		rm.closeSocksServer(routeID, "listener stopped", protocol.RouteStateClosed)
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				rm.closeSocksServer(routeID, err.Error(), protocol.RouteStateError)
				return
			}
		}
		go rm.handleSocksClient(ctx, routeID, agentID, c)
	}
}

func (rm *RouteManager) handleSocksClient(ctx context.Context, routeID, agentID string, c net.Conn) {
	defer c.Close()
	host, port, err := socks5.Negotiate(c)
	if err != nil {
		log.Printf("socks_server negotiate: %v", err)
		return
	}
	key, tunnelHex, err := protocol.NewTunnelKey()
	if err != nil {
		_ = socks5.ReplyFail(c, 0x01)
		return
	}
	wait := rm.registerTunnelWait(tunnelHex)
	if !rm.hub.SendAgent(agentID, protocol.RouteTunnelOpen{
		Type:       protocol.TypeRouteTunnelOpen,
		RouteID:    routeID,
		TunnelID:   tunnelHex,
		TargetHost: host,
		TargetPort: port,
	}) {
		rm.dropTunnelWait(tunnelHex)
		_ = socks5.ReplyFail(c, 0x01)
		return
	}
	var ack tunnelAckResult
	select {
	case ack = <-wait:
	case <-time.After(20 * time.Second):
		rm.dropTunnelWait(tunnelHex)
		_ = socks5.ReplyFail(c, 0x04)
		return
	case <-ctx.Done():
		rm.dropTunnelWait(tunnelHex)
		return
	}
	if !ack.ok {
		_ = socks5.ReplyFail(c, 0x05)
		return
	}
	if err := socks5.ReplyOK(c); err != nil {
		rm.closeAgentTunnel(agentID, routeID, tunnelHex)
		return
	}

	rm.mu.Lock()
	rm.tunnels[tunnelHex] = &activeTunnel{
		egressAgentID: agentID, routeID: routeID, client: c, tunnelID: tunnelHex, tunnelKey: key,
	}
	rm.mu.Unlock()

	buf := make([]byte, 32*1024)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			frame := protocol.WrapTunnelFrame(key, buf[:n])
			if !rm.hub.SendAgentBinary(agentID, frame) {
				break
			}
		}
		if err != nil {
			break
		}
	}
	rm.closeAgentTunnel(agentID, routeID, tunnelHex)
}

func (rm *RouteManager) registerTunnelWait(id string) chan tunnelAckResult {
	ch := make(chan tunnelAckResult, 1)
	rm.tunnelMu.Lock()
	rm.tunnelWait[id] = &tunnelWaiter{ch: ch}
	rm.tunnelMu.Unlock()
	return ch
}

func (rm *RouteManager) dropTunnelWait(id string) {
	rm.tunnelMu.Lock()
	delete(rm.tunnelWait, id)
	rm.tunnelMu.Unlock()
}

func (rm *RouteManager) HandleTunnelAck(agentID string, ack *protocol.RouteTunnelAck) {
	rm.tunnelMu.Lock()
	w, ok := rm.tunnelWait[ack.TunnelID]
	if ok {
		delete(rm.tunnelWait, ack.TunnelID)
	}
	rm.tunnelMu.Unlock()
	if !ok {
		return
	}
	select {
	case w.ch <- tunnelAckResult{ok: ack.OK, message: ack.Message}:
	default:
	}
}

func (rm *RouteManager) HandleTunnelBinary(agentID string, data []byte) {
	key, payload, ok := protocol.UnwrapTunnelFrame(data)
	if !ok {
		return
	}
	id := fmt.Sprintf("%x", key[:])
	rm.mu.RLock()
	t, ok := rm.tunnels[id]
	rm.mu.RUnlock()
	if !ok || len(payload) == 0 {
		return
	}
	if t.egressAgentID == agentID {
		if t.listenAgentID != "" {
			_ = rm.hub.SendAgentBinary(t.listenAgentID, data)
			return
		}
		if t.client != nil {
			_, _ = t.client.Write(payload)
		}
		return
	}
	if t.listenAgentID == agentID && t.egressAgentID != "" {
		frame := protocol.WrapTunnelFrame(t.tunnelKey, payload)
		_ = rm.hub.SendAgentBinary(t.egressAgentID, frame)
	}
}

func (rm *RouteManager) closeAgentTunnel(egressAgentID, routeID, tunnelID string) {
	rm.mu.Lock()
	t, ok := rm.tunnels[tunnelID]
	if ok {
		delete(rm.tunnels, tunnelID)
	}
	rm.mu.Unlock()
	if ok && t.client != nil {
		_ = t.client.Close()
	}
	if ok && t.listenAgentID != "" {
		rm.hub.SendAgent(t.listenAgentID, protocol.RouteTunnelClose{
			Type: protocol.TypeRouteTunnelClose, RouteID: routeID, TunnelID: tunnelID,
		})
	}
	rm.hub.SendAgent(egressAgentID, protocol.RouteTunnelClose{
		Type: protocol.TypeRouteTunnelClose, RouteID: routeID, TunnelID: tunnelID,
	})
}

func (rm *RouteManager) closeTunnelFromAgent(tunnelID string) {
	rm.mu.Lock()
	t, ok := rm.tunnels[tunnelID]
	if ok {
		delete(rm.tunnels, tunnelID)
	}
	rm.mu.Unlock()
	if ok && t.client != nil {
		_ = t.client.Close()
	}
}

func (rm *RouteManager) closeSocksServer(routeID, message, state string) {
	rm.mu.Lock()
	sr, ok := rm.socksSrv[routeID]
	if ok {
		delete(rm.socksSrv, routeID)
	}
	rec, recOK := rm.routes[routeID]
	agentID := ""
	if recOK {
		agentID = rec.AgentID
		rec.State = state
		rec.Message = message
		rec.UpdatedAt = time.Now().UTC()
	}
	var tunnelIDs []string
	for id, t := range rm.tunnels {
		if t.routeID == routeID {
			tunnelIDs = append(tunnelIDs, id)
		}
	}
	rm.mu.Unlock()

	if ok && sr != nil {
		sr.cancel()
		_ = sr.ln.Close()
	}
	for _, tid := range tunnelIDs {
		rm.closeAgentTunnel(agentID, routeID, tid)
	}
	if recOK && agentID != "" {
		rm.hub.SendAgent(agentID, protocol.RouteClose{
			Type: protocol.TypeRouteClose, RouteID: routeID,
		})
	}
	rm.hub.notifyUI()
}
