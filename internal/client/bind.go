package client

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"

	"finalcat/internal/protocol"
)

var bindUpgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func runBindOnly(ctx context.Context, cfg Config) error {
	log.Printf("bind mode listening on %s path=%s", cfg.BindListen, cfg.Path)
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.Path, func(w http.ResponseWriter, r *http.Request) {
		c, err := bindUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		remote, _, _ := net.SplitHostPort(r.RemoteAddr)
		go func() {
			if err := serveInboundControl(ctx, c, cfg, protocol.AgentModeBind, "", remote); err != nil {
				log.Printf("bind session: %v", err)
			}
		}()
	})
	srv := &http.Server{Addr: cfg.BindListen, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return ctx.Err()
	}
	return err
}

func runHybrid(ctx context.Context, cfg Config) error {
	var wg sync.WaitGroup
	errCh := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := runBindOnly(ctx, cfg); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("bind: %w", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := runReverse(ctx, cfg); err != nil && ctx.Err() == nil {
			errCh <- fmt.Errorf("reverse: %w", err)
		}
	}()
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return ctx.Err()
	}
}

func bindListeners(cfg Config) []protocol.ListenerInfo {
	if cfg.BindListen == "" {
		return nil
	}
	return []protocol.ListenerInfo{{
		Address: cfg.BindListen,
		Role:    "downstream_control",
		State:   "listening",
	}}
}
