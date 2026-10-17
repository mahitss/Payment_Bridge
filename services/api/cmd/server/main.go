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

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/logger"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
	"claimbridge/services/api/internal/server"
)

func main() {
	// 1. Initialize Structured Redacted Logger
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}
	appLogger := logger.InitLogger(logLevel, os.Stdout)
	slog.SetDefault(appLogger)

	slog.Info("Starting ClaimBridge API Gateway...")

	// 2. Load and Strictly Validate Configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		slog.Error("Configuration validation failed (Fail-Closed)", slog.String("error", err.Error()))
		os.Exit(1)
	}

	slog.Info("Configuration loaded successfully", slog.String("details", cfg.String()))

	// 3. Initialize Policy Engine
	policyEngine := policy.NewEngine(cfg.PolicyEngineBinaryPath)

	// 4. Initialize PayPal Sandbox Client
	paypalClient, err := paypal.NewClient(cfg, nil)
	if err != nil {
		slog.Error("Failed to initialize PayPal client", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// 5. Initialize Server & Routes
	srv := server.NewServer(cfg, policyEngine, paypalClient)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 6. Graceful Shutdown Handler
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("HTTP Server listening", slog.String("addr", httpServer.Addr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP Server failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	<-stop
	slog.Info("Shutting down HTTP Server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", slog.String("error", err.Error()))
	} else {
		slog.Info("ClaimBridge API server stopped safely.")
	}
}
