package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/tingz/easy-invest/internal/config"
	"github.com/tingz/easy-invest/internal/platform"
)

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) < 2 || os.Args[1] != "up" {
		fmt.Fprintln(os.Stderr, "usage: easy-invest-migrate up")
		os.Exit(2)
	}
	if err := platform.RunMigrations(context.Background(), cfg.DatabaseURL); err != nil {
		log.Error("migration failed", "error", err)
		os.Exit(1)
	}
	log.Info("migration completed")
}
