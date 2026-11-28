package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shantz14/idle-kings/internal/bot"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

// healthyWithin is how recently the game loop must have finished a tick
// for /healthz to pass. Ticks run every 15s and are capped at 2 minutes.
const healthyWithin = 3 * time.Minute

func main() {
	os.Exit(run())
}

// run is main without os.Exit, so deferred closes (the database's WAL
// checkpoint above all) always happen.
func run() int {
	slog.Info("idle kings", "version", version)
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		slog.Error("DISCORD_TOKEN is required")
		return 1
	}
	dbPath := os.Getenv("IDLE_KINGS_DB")
	if dbPath == "" {
		dbPath = "idle-kings.db"
	}
	timings := game.DefaultTimings
	if os.Getenv("IDLE_KINGS_FAST") == "1" {
		timings = game.FastTimings
		slog.Info("fast mode: parties and senate days last minutes, not hours")
	}

	st, err := store.Open(dbPath)
	if err != nil {
		slog.Error("open db", "path", dbPath, "err", err)
		return 1
	}
	defer st.Close()

	b, err := bot.New(token, st, timings)
	if err != nil {
		slog.Error("create bot", "err", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if addr := os.Getenv("IDLE_KINGS_HEALTH_ADDR"); addr != "" {
		srv := healthServer(addr, b)
		defer srv.Close()
	}
	if err := b.Run(ctx); err != nil {
		slog.Error("bot stopped", "err", err)
		return 1
	}
	slog.Info("stopped cleanly")
	return 0
}

// healthServer serves /healthz: 200 while the game loop is ticking, 503 once
// it has stalled, so an orchestrator can restart a stuck bot.
func healthServer(addr string, b *bot.Bot) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if !b.Healthy(healthyWithin) {
			http.Error(w, "game loop stalled", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok " + version + "\n"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("health server", "addr", addr, "err", err)
		}
	}()
	slog.Info("health check listening", "addr", addr)
	return srv
}
