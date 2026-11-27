package main

import (
	"log"
	"net/http"
	"time"

	"mygo-manga/internal/demosource"
)

func main() {
	server := &http.Server{
		Addr: "127.0.0.1:8787", Handler: demosource.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("试读图源地址 http://%s/sources.json", server.Addr)
	log.Fatal(server.ListenAndServe())
}
