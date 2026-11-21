// Command echo-server é um serviço de teste para reverse proxies, ingress controllers e
// API gateways: responde a qualquer método em qualquer path devolvendo o que recebeu.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/luizcarlosfaria/echo-server/internal/app"
	"github.com/luizcarlosfaria/echo-server/internal/config"
	"github.com/luizcarlosfaria/echo-server/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "echo-server:", err)
		os.Exit(1)
	}
}

func run() error {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	options, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	signals, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	// Cancelado no shutdown: encerra os WebSockets do painel, que o http.Server não acompanha.
	stopping, stop := context.WithCancel(context.Background())
	defer stop()

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", options.Port),
		Handler:           app.New(options, store.New(options.MaxRequests), stopping),
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelDebug),
	}
	server.RegisterOnShutdown(stop)

	failed := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()

	slog.Info(fmt.Sprintf("Echo '%s' keeping up to %d requests (body up to %d bytes). Dashboard at %s",
		options.AppName, options.MaxRequests, options.MaxBodyBytes, options.DashboardPath), "port", options.Port)

	select {
	case err := <-failed:
		return err
	case <-signals.Done():
	}

	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}
