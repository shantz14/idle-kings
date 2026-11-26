package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/shantz14/idle-kings/internal/bot"
	"github.com/shantz14/idle-kings/internal/game"
	"github.com/shantz14/idle-kings/internal/store"
)

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		slog.Error("DISCORD_TOKEN is required")
		os.Exit(1)
	}
	dbPath := os.Getenv("IDLE_KINGS_DB")
	if dbPath == "" {
		dbPath = "idle-kings.db"
	}
	timing, senate := game.DefaultPartyTiming, game.DefaultSenateTiming
	if os.Getenv("IDLE_KINGS_FAST") == "1" {
		timing, senate = game.FastPartyTiming, game.FastSenateTiming
		slog.Info("fast mode: parties and senate days last minutes, not hours")
	}

	st, err := store.Open(dbPath)
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	b, err := bot.New(token, st, timing, senate)
	if err != nil {
		slog.Error("create bot", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := b.Run(ctx); err != nil {
		slog.Error("bot stopped", "err", err)
		os.Exit(1)
	}
}
