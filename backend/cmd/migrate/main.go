package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	platformpostgres "jarvis/backend/internal/platform/postgres"
	"jarvis/backend/internal/platform/postgres/migrations"
)

var ErrInvalidCommand = errors.New("migrate: expected exactly one command: up or down")

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Getenv); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, getenv func(string) string) error {
	if len(arguments) != 1 || (arguments[0] != "up" && arguments[0] != "down") {
		return ErrInvalidCommand
	}

	config, err := platformpostgres.LoadConfig(getenv)
	if err != nil {
		return err
	}
	pool, err := platformpostgres.Open(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close()
	operationContext, cancelOperation := context.WithTimeout(ctx, config.OperationTimeout)
	defer cancelOperation()

	connection, err := pool.Acquire(operationContext)
	if err != nil {
		return errors.New("migrate: database connection acquisition failed")
	}
	defer connection.Release()

	if arguments[0] == "down" {
		return migrations.Down(operationContext, connection.Conn())
	}
	return migrations.Up(operationContext, connection.Conn())
}
