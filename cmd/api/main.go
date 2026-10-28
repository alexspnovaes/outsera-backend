// Importa o CSV e sobe a rota dos intervalos de prêmio.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/alexspnovaes/outsera-backend/internal/adapter/csvfile"
	"github.com/alexspnovaes/outsera-backend/internal/adapter/httpapi"
	"github.com/alexspnovaes/outsera-backend/internal/adapter/sqlite"
	"github.com/alexspnovaes/outsera-backend/internal/application"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	startup, cancelStartup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStartup()

	store, err := sqlite.Open(startup)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			slog.Error("close database", "err", err)
		}
	}()

	importer := application.NewImporter(csvfile.Reader{}, store)
	count, err := importMovies(startup, cfg.csvPath, importer)
	if err != nil {
		return err
	}
	slog.Info("movies imported", "count", count, "csv", cfg.csvPath)

	address := net.JoinHostPort(cfg.host, cfg.port)
	server := &http.Server{
		Addr:              address,
		Handler:           httpapi.New(application.NewIntervals(store)),
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
		slog.Info("api listening", "url", "http://"+address)
		serverErrors <- server.ListenAndServe()
	}()
	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		slog.Info("shutting down api")
		shutdown, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		if err := server.Shutdown(shutdown); err != nil {
			shutdownErr := fmt.Errorf("shutdown HTTP server: %w", err)
			if closeErr := server.Close(); closeErr != nil {
				return errors.Join(shutdownErr, fmt.Errorf("force close HTTP server: %w", closeErr))
			}
			return shutdownErr
		}
		return nil
	}
}

type config struct {
	host    string
	port    string
	csvPath string
}

func loadConfig() (config, error) {
	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	if net.ParseIP(host) == nil {
		return config{}, fmt.Errorf("HOST must be a valid IP address")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return config{}, fmt.Errorf("PORT must be an integer between 1 and 65535")
	}
	csvPath := os.Getenv("CSV_PATH")
	if csvPath == "" {
		csvPath = "data/Movielist.csv"
	}
	return config{host: host, port: port, csvPath: csvPath}, nil
}

func importMovies(ctx context.Context, path string, importer *application.Importer) (count int, err error) {
	// #nosec G304 -- o caminho vem da configuração do processo, não de uma requisição.
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open CSV %q: %w", path, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close CSV %q: %w", path, closeErr))
		}
	}()

	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("inspect CSV %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return 0, fmt.Errorf("CSV %q must be a regular file", path)
	}
	if info.Size() > csvfile.MaxFileSize {
		return 0, fmt.Errorf("CSV %q exceeds the %d-byte limit", path, csvfile.MaxFileSize)
	}

	count, err = importer.Import(ctx, file)
	if err != nil {
		return 0, fmt.Errorf("import CSV: %w", err)
	}
	return count, nil
}
