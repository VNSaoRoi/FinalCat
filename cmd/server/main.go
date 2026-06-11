package main

import (
	"flag"
	"log"

	"finalcat/internal/server"
)

func main() {
	control := flag.String("l", "0.0.0.0:31747", "agent control listen")
	admin := flag.String("admin", "127.0.0.1:31891", "operator UI+REST (loopback only)")
	password := flag.String("p", "", "operator UI password (optional)")
	dataDir := flag.String("data", "finalcat-data", "server data directory (route catalog persistence)")
	flag.Parse()

	adminAddr, err := server.ParseAdminListen(*admin)
	if err != nil {
		log.Fatal(err)
	}

	app := server.NewApp(*control, adminAddr, *password, *dataDir)
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
