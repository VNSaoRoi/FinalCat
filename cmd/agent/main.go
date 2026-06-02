package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"finalcat/internal/client"
	"finalcat/internal/config"
)

func main() {
	var upstreams string
	var bindListen string
	path := flag.String("path", "/ws/agent", "WebSocket path")
	flag.StringVar(&upstreams, "r", "", "reverse: server host:port (comma-separated failover list)")
	flag.StringVar(&bindListen, "l", "", "bind: local listen host:port")
	flag.Parse()

	eps := config.ParseEndpoints(upstreams)
	if len(eps) == 0 && bindListen == "" {
		log.Fatal("usage: agent -r host:31747  OR  agent -l 0.0.0.0:18229  OR both (hybrid)")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := client.Run(ctx, client.Config{
		Upstreams:  eps,
		BindListen: bindListen,
		Path:       *path,
	}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
