package protocol

const (
	TypeRegister         = "register"
	TypeAck              = "ack"
	TypePing             = "ping"
	TypePong             = "pong"
	TypeSetUpstream      = "set_upstream"
	TypeHeartbeat        = "heartbeat"
	TypeRouteOpenSocks   = "route_open_socks"
	TypeRouteOpenForward = "route_open_forward"
	TypeRouteClose       = "route_close"
	TypeRouteEvent       = "route_event"
	TypeRouteTunnelOpen  = "route_tunnel_open"
	TypeRouteTunnelAck   = "route_tunnel_ack"
	TypeRouteTunnelClose = "route_tunnel_close"

	AgentModeReverse = "reverse"
	AgentModeBind    = "bind"
	AgentModeHybrid  = "hybrid"

	RouteKindSocks       = "socks"
	RouteKindSocksServer = "socks_server"
	RouteKindForward     = "forward"
	RouteStatePending = "pending"
	RouteStateActive  = "active"
	RouteStateClosed  = "closed"
	RouteStateError   = "error"
)

type ListenerInfo struct {
	Address string `json:"address"`
	Role    string `json:"role,omitempty"`
	State   string `json:"state,omitempty"`
}

type Register struct {
	Type               string         `json:"type"`
	Hostname           string         `json:"hostname"`
	OS                 string         `json:"os"`
	GOArch             string         `json:"goarch,omitempty"`
	OSUser             string         `json:"os_user,omitempty"`
	LocalIPs           []string       `json:"local_ips,omitempty"`
	Listeners          []ListenerInfo `json:"listeners,omitempty"`
	UpstreamEndpoints  []string       `json:"upstream_endpoints,omitempty"`
	ActiveUpstreamUsed string         `json:"active_upstream_used,omitempty"`
	PersistentID       string         `json:"persistent_id,omitempty"`
	AgentMode          string         `json:"agent_mode,omitempty"`
}

type Ack struct {
	Type     string `json:"type"`
	ClientID string `json:"client_id"`
	Revision int64  `json:"revision"`
	Message  string `json:"message,omitempty"`
}

type Ping struct {
	Type string `json:"type"`
	Seq  int64  `json:"seq"`
}

type Pong struct {
	Type     string `json:"type"`
	Seq      int64  `json:"seq"`
	ClientID string `json:"client_id"`
}

type SetUpstream struct {
	Type      string   `json:"type"`
	Endpoints []string `json:"endpoints"`
	Revision  int64    `json:"revision"`
}

type Heartbeat struct {
	Type     string   `json:"type"`
	ClientID string   `json:"client_id"`
	LocalIPs []string `json:"local_ips,omitempty"`
	OSUser   string   `json:"os_user,omitempty"`
}

type RouteOpenSocks struct {
	Type     string `json:"type"`
	RouteID  string `json:"route_id"`
	BindAddr string `json:"bind_addr"`
}

type RouteOpenForward struct {
	Type       string `json:"type"`
	RouteID    string `json:"route_id"`
	ListenAddr string `json:"listen_addr"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
}

type RouteClose struct {
	Type    string `json:"type"`
	RouteID string `json:"route_id"`
}

type RouteEvent struct {
	Type       string `json:"type"`
	RouteID    string `json:"route_id"`
	ClientID   string `json:"client_id"`
	Kind       string `json:"kind"`
	State      string `json:"state"`
	ListenAddr string `json:"listen_addr,omitempty"`
	Target     string `json:"target,omitempty"`
	Message    string `json:"message,omitempty"`
	BindOn     string `json:"bind_on,omitempty"`
}

type RouteTunnelOpen struct {
	Type       string `json:"type"`
	RouteID    string `json:"route_id"`
	TunnelID   string `json:"tunnel_id"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
}

type RouteTunnelAck struct {
	Type     string `json:"type"`
	RouteID  string `json:"route_id"`
	TunnelID string `json:"tunnel_id"`
	OK       bool   `json:"ok"`
	Message  string `json:"message,omitempty"`
}

type RouteTunnelClose struct {
	Type     string `json:"type"`
	RouteID  string `json:"route_id,omitempty"`
	TunnelID string `json:"tunnel_id"`
}
