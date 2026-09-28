// Command api runs the DADO backend: a JSON API on :8080 that the Next.js
// site proxies /api/* to.
//
//	cd backend && go run ./cmd/api              # start the API
//	cd backend && go run ./cmd/api -seed-demo   # also load sample data (empty database only)
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dado/internal/config"
	"dado/internal/db"
	"dado/internal/handlers"
	"dado/internal/mail"
	"dado/internal/routes"
	"dado/internal/seed"
)

func main() {
	seedDemo := flag.Bool("seed-demo", false, "load the prototype's sample clients, jobs, trials and contacts (empty database only)")
	flag.Parse()

	if err := run(*seedDemo); err != nil {
		slog.Error("stopped", "err", err)
		os.Exit(1)
	}
}

func run(seedDemo bool) error {
	cfg, err := config.Load(".env")
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return err
	}
	defer database.Close(context.Background())

	if err := seed.Run(ctx, database, cfg); err != nil {
		return err
	}
	if seedDemo {
		if err := seed.Demo(ctx, database); err != nil {
			return err
		}
	}

	mailer := mail.New(cfg)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           routes.New(handlers.New(database, cfg, mailer), cfg.JWTSecret),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second, // image uploads can be slow
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("API listening", "addr", "http://localhost:"+cfg.Port, "uploads", cfg.UploadsEnabled())
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	mailer.Wait() // let emails already on their way finish, even if Shutdown timed out
	return err
}
