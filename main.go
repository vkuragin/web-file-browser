package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	drives, err := discoverDrives()
	if err != nil {
		log.Fatal(err)
	}
	port := os.Getenv("FILE_EXPLORER_PORT")
	if port == "" {
		port = "8765"
	}
	server := &http.Server{
		Addr:              "127.0.0.1:" + port,
		Handler:           newExplorer(drives).routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	log.Printf("File Explorer running at http://localhost:%s/ (drives: %s)", port, strings.Join(driveNames(drives), ", "))
	log.Printf("Press Ctrl+C to stop.")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	log.Printf("File Explorer stopped; port %s released.", port)
}
