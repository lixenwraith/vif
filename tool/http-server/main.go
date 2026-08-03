package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	dir := flag.String("dir", ".", "directory to serve")
	flag.Parse()

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("Serving %q on http://localhost%s\n", *dir, addr)
	fmt.Printf("Ctrl+C to stop\n")

	http.Handle("/", http.FileServer(http.Dir(*dir)))
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("HTTP server crashed: %v", err)
	}
}
