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

	"claimbridge/services/api/internal/gateway"
	"claimbridge/services/api/internal/logger"
)

func main() {
	appLogger := logger.InitLogger("info", os.Stdout)
	slog.SetDefault(appLogger)

	slog.Info("Starting ClaimBridge Webhook Ingress Gateway...")

	listenAddr := os.Getenv("WEBHOOK_GATEWAY_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = gateway.DefaultListenAddr
	}

	targetURL := os.Getenv("WEBHOOK_GATEWAY_TARGET_URL")
	if targetURL == "" {
		targetURL = gateway.DefaultTargetURL
	}

	gw, err := gateway.NewGateway(targetURL)
	if err != nil {
		slog.Error("Failed initializing webhook gateway", slog.String("error", err.Error()))
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           gw,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Webhook Gateway listening",
			slog.String("listen_addr", listenAddr),
			slog.String("forwarding_to", targetURL),
			slog.String("allowed_route", gateway.AllowedPath),
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Webhook Gateway failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	<-stop
	slog.Info("Shutting down Webhook Gateway gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("Webhook Gateway shutdown error", slog.String("error", err.Error()))
	} else {
		slog.Info("Webhook Gateway stopped safely.")
	}
}
