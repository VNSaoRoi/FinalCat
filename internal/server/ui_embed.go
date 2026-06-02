package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var webRoot embed.FS

func uiFS() http.FileSystem {
	sub, err := fs.Sub(webRoot, "web")
	if err != nil {
		return http.FS(webRoot)
	}
	return http.FS(sub)
}
