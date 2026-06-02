package client

import (
	"context"
	"errors"
	"sync"
)

var (
	ErrReconnect      = errors.New("reconnect")
	upstreamMu        sync.RWMutex
	dynamicUpstreams  []string
)

type Config struct {
	Upstreams  []string
	BindListen string // host:port for bind/hybrid listen
	Path       string
}

func SetDynamicUpstreams(eps []string) {
	upstreamMu.Lock()
	dynamicUpstreams = append([]string(nil), eps...)
	upstreamMu.Unlock()
}

func effectiveUpstreams(cfg Config) []string {
	upstreamMu.RLock()
	defer upstreamMu.RUnlock()
	if len(dynamicUpstreams) > 0 {
		return append([]string(nil), dynamicUpstreams...)
	}
	return append([]string(nil), cfg.Upstreams...)
}

// Run starts reverse, bind-only, or hybrid mode based on flags.
func Run(ctx context.Context, cfg Config) error {
	if cfg.Path == "" {
		cfg.Path = "/ws/agent"
	}
	hasReverse := len(cfg.Upstreams) > 0
	hasBind := cfg.BindListen != ""

	switch {
	case hasReverse && hasBind:
		return runHybrid(ctx, cfg)
	case hasBind:
		return runBindOnly(ctx, cfg)
	case hasReverse:
		return runReverse(ctx, cfg)
	default:
		return errors.New("need -r and/or -l")
	}
}
