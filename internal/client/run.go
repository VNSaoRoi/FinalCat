package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/user"
	"runtime"
	"time"

	"github.com/gorilla/websocket"

	"finalcat/internal/protocol"
)

func runReverse(ctx context.Context, cfg Config) error {
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	offset := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		eps := effectiveUpstreams(cfg)
		if len(eps) == 0 {
			return fmt.Errorf("no upstreams")
		}
		up := eps[offset%len(eps)]
		offset++
		err := dialReverse(ctx, dialer, cfg, up)
		if err == ErrReconnect {
			continue
		}
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			log.Printf("reverse session: %v; retry in 30s", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
		}
	}
}

func dialReverse(ctx context.Context, dialer websocket.Dialer, cfg Config, upstream string) error {
	u := url.URL{Scheme: "ws", Host: upstream, Path: cfg.Path}
	conn, _, err := dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	mode := protocol.AgentModeReverse
	if cfg.BindListen != "" {
		mode = protocol.AgentModeHybrid
	}
	return serveInboundControl(ctx, conn, cfg, mode, upstream, "")
}

func serveInboundControl(ctx context.Context, conn *websocket.Conn, cfg Config, mode, activeUpstream, remoteAddr string) error {
	defer conn.Close()

	hostname, _ := os.Hostname()
	ou, _ := user.Current()
	osUser := ""
	if ou != nil {
		osUser = ou.Username
	}
	eps := effectiveUpstreams(cfg)
	if activeUpstream == "" && len(eps) > 0 {
		activeUpstream = eps[0]
	}
	ws := newWSWriter(conn)
	reg, _ := json.Marshal(protocol.Register{
		Type:               protocol.TypeRegister,
		Hostname:           hostname,
		OS:                 runtime.GOOS,
		GOArch:             runtime.GOARCH,
		OSUser:             osUser,
		LocalIPs:           localIPs(),
		Listeners:          bindListeners(cfg),
		UpstreamEndpoints:  eps,
		ActiveUpstreamUsed: activeUpstream,
		PersistentID:       LoadPersistentID(),
		AgentMode:          mode,
	})
	if err := ws.WriteText(reg); err != nil {
		return err
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	var ack protocol.Ack
	if json.Unmarshal(raw, &ack) != nil || ack.ClientID == "" {
		return fmt.Errorf("invalid ack")
	}
	log.Printf("registered id=%s mode=%s upstream=%s remote=%s", ack.ClientID, mode, activeUpstream, remoteAddr)

	tunnels := newTunnelManager(ctx, ws)
	defer tunnels.closeAll()

	routes := newRouteManager(ctx, ws, tunnels, ack.ClientID)
	defer routes.closeAll()

	relay := newRelayManager(ctx, ws, ack.ClientID)
	defer relay.closeAll()

	forwardSmart := newForwardSmartManager(ctx, ws, ack.ClientID)
	defer forwardSmart.closeAll()

	done := make(chan struct{})
	defer close(done)
	go heartbeatLoop(ws, ack.ClientID, osUser, done)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if msgType == websocket.BinaryMessage {
			forwardSmart.handleBinary(data)
			tunnels.handleBinary(data)
			continue
		}
		var head struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &head) != nil {
			continue
		}
		switch head.Type {
		case protocol.TypePing:
			var p protocol.Ping
			if json.Unmarshal(data, &p) != nil {
				continue
			}
			pong, _ := json.Marshal(protocol.Pong{Type: protocol.TypePong, Seq: p.Seq, ClientID: ack.ClientID})
			_ = ws.WriteText(pong)
		case protocol.TypeSetUpstream:
			var su protocol.SetUpstream
			if json.Unmarshal(data, &su) != nil || len(su.Endpoints) == 0 {
				continue
			}
			SetDynamicUpstreams(su.Endpoints)
			log.Printf("upstream update rev=%d: %v", su.Revision, su.Endpoints)
			return ErrReconnect
		default:
			if relay.handle(data) {
				continue
			}
			if forwardSmart.handle(data) {
				continue
			}
			if routes.handle(data) {
				continue
			}
			if tunnels.handleText(data) {
				continue
			}
		}
	}
}

func heartbeatLoop(ws *wsWriter, clientID, osUser string, done <-chan struct{}) {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			hb, _ := json.Marshal(protocol.Heartbeat{
				Type: protocol.TypeHeartbeat, ClientID: clientID,
				LocalIPs: localIPs(), OSUser: osUser,
			})
			_ = ws.WriteText(hb)
		}
	}
}

func localIPs() []string {
	ifaces, _ := net.Interfaces()
	var out []string
	seen := make(map[string]struct{})
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			s := ip.String()
			if _, ok := seen[s]; !ok {
				seen[s] = struct{}{}
				out = append(out, s)
			}
		}
	}
	return out
}
