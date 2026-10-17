// Test http server for dangling healthcheck
package main

import (
	"io"
	"log"
	"net/http"
)

// A handler that consumes the request but never responds.
func hangHandler(w http.ResponseWriter, r *http.Request) {
	// Drain the request body so the client doesn’t get a reset.
	// We ignore any read error – the client may close early.
	_, _ = io.Copy(io.Discard, r.Body)
	_ = r.Body.Close()

	log.Println("incoming request accepted, blocking response")

	// Block forever – do NOT call w.WriteHeader or w.Write.
	select {} // blocks indefinitely
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", hangHandler)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	log.Println("Hanging HTTP server listening on :8080")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
