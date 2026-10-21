package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alexspnovaes/outsera-backend/internal/httpapi"
	"github.com/alexspnovaes/outsera-backend/internal/movie"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("PORT must be an integer between 1 and 65535")
	}
	csvPath := os.Getenv("CSV_PATH")
	if csvPath == "" {
		csvPath = "data/Movielist.csv"
	}
	origins := os.Getenv("CORS_ORIGINS")
	if origins == "" {
		origins = "http://localhost:5173,http://localhost:4173,http://127.0.0.1:5173,http://127.0.0.1:4173"
	}

	startup, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStartup()
	store, err := movie.NewStore(startup)
	if err != nil {
		return err
	}
	defer store.Close()
	file, err := os.Open(csvPath)
	if err != nil {
		return fmt.Errorf("open CSV %q: %w", csvPath, err)
	}
	count, importErr := store.ImportCSV(startup, file)
	closeErr := file.Close()
	if importErr != nil {
		return fmt.Errorf("import CSV: %w", importErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close CSV: %w", closeErr)
	}
	log.Printf("Imported %d movies from %s into in-memory SQLite", count, csvPath)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           httpapi.New(store, strings.Split(origins, ",")),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("API listening on http://localhost:%s", port)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		log.Print("Shutting down API")
		shutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		return nil
	}
}
