package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"

	"mserp/internal/db"
	"mserp/internal/repository"
)

func main() {
	_ = godotenv.Load(".env.local", ".env", "/etc/mserp/mserp.env")
	if len(os.Args) != 2 || strings.TrimSpace(os.Args[1]) == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/telegram-manager <existing-username>")
		os.Exit(2)
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	ctx := context.Background()
	pool, err := db.NewPool(ctx, databaseURL)
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	err = repository.NewAssistantRepository(pool).BootstrapManager(ctx, os.Args[1])
	if errors.Is(err, repository.ErrNotFound) {
		fail(errors.New("active ERP user was not found"))
	}
	if err != nil {
		fail(err)
	}
	fmt.Printf("Telegram manager approved: %s\n", os.Args[1])
}

func fail(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }
