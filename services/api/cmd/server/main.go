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

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/auth"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/email"
	"claimbridge/services/api/internal/investigations"
	"claimbridge/services/api/internal/logger"
	"claimbridge/services/api/internal/payouts"
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

	// 3. Initialize Database Repository (PostgreSQL with in-memory fallback)
	initCtx, initCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer initCancel()

	var repo db.Repository
	pgRepo, err := db.NewPostgresRepository(initCtx, cfg.DatabaseURL)
	if err != nil {
		slog.Warn("PostgreSQL unavailable at startup; using memory repository for development",
			slog.String("error", err.Error()),
		)
		repo = db.NewMockRepository()
	} else {
		slog.Info("Connected to PostgreSQL successfully")
		repo = pgRepo
		defer pgRepo.Close()
	}

	// 4. Initialize Email Delivery (Mailpit / SMTP)
	var mailer email.Mailer
	if cfg.SMTPHost != "" {
		slog.Info("Configured SMTP mailer",
			slog.String("host", cfg.SMTPHost),
			slog.Int("port", cfg.SMTPPort),
			slog.String("from", cfg.SMTPFrom),
		)
		mailer = email.NewSMTPMailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom, cfg.SMTPUser, cfg.SMTPPass)
	} else {
		slog.Info("No SMTP host configured; using mock mailer")
		mailer = email.NewMockMailer()
	}

	// 5. Initialize Auth Service & Handler
	authService := auth.NewService(cfg, repo, mailer)
	authHandler := auth.NewHandler(cfg, authService)

	// 6. Initialize Policy Engine
	policyEngine := policy.NewEngine(cfg.PolicyEngineBinaryPath)

	// 7. Initialize PayPal Sandbox Client
	paypalClient, err := paypal.NewClient(cfg, nil)
	if err != nil {
		slog.Error("Failed to initialize PayPal client", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// 8. Initialize Payouts Service & Handler
	payoutsService := payouts.NewService(cfg, repo, policyEngine, paypalClient)
	payoutsHandler := payouts.NewHandler(cfg, payoutsService)

	// 9. Initialize AI Provider & Investigations Handler
	aiProvider, err := ai.NewProvider(cfg)
	if err != nil {
		slog.Error("Failed to initialize AI provider", slog.String("error", err.Error()))
		os.Exit(1)
	}
	slog.Info("Initialized AI inference provider",
		slog.String("provider", aiProvider.Name()),
		slog.String("model", aiProvider.Model()),
		slog.Bool("enabled", cfg.AIEnabled),
	)

	investigationsService := investigations.NewService(cfg, repo, aiProvider, policyEngine, paypalClient)
	investigationsHandler := investigations.NewHandler(cfg, investigationsService)

	// 10. Initialize Server & Routes
	srv := server.NewServer(cfg, policyEngine, paypalClient, authHandler, payoutsHandler)
	srv.SetRepository(repo)
	srv.SetInvestigationsHandler(investigationsHandler)

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%s", cfg.Port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// 9. Graceful Shutdown Handler
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
