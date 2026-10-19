package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/seeder"
)

func main() {
	resetFlag := flag.Bool("reset", false, "Remove demo test fixtures before seeding")
	flag.Parse()

	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := db.NewPostgresRepository(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Printf("\n[ERROR] Unable to connect to PostgreSQL at %s\n", cfg.DatabaseURL)
		fmt.Println("Please ensure PostgreSQL is running before running the seed tool:")
		fmt.Println("  docker compose -f infra/docker-compose.yml up -d")
		os.Exit(1)
	}
	defer repo.Close()

	// Ensure database schema migrations are applied
	if err := repo.RunMigrations(ctx); err != nil {
		fmt.Printf("\n[ERROR] Failed applying schema migrations: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("==================================================================")
	fmt.Println(" CLAIMBRIDGE — HACKATHON DEMONSTRATION FIXTURE SEEDER")
	fmt.Println("==================================================================")

	s := seeder.New(repo)
	if err := s.Seed(ctx, seeder.Options{Reset: *resetFlag}); err != nil {
		fmt.Printf("\n[ERROR] Seeding failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("==================================================================")
	fmt.Println(" DEMO FIXTURES READY FOR EVALUATION")
	fmt.Println("==================================================================")
	fmt.Println("You can now log in to the web interface (http://localhost:3000):")
	fmt.Println()
	fmt.Println(" 1. Primary Operator (Case Creator):")
	fmt.Printf("    Email:    %s\n", seeder.OperatorEmail)
	fmt.Printf("    Password: %s\n", seeder.OperatorPass)
	fmt.Println()
	fmt.Println(" 2. Compliance Officer (Dual-Approval Signer):")
	fmt.Printf("    Email:    %s\n", seeder.OfficerEmail)
	fmt.Printf("    Password: %s\n", seeder.OfficerPass)
	fmt.Println()
	fmt.Println(" All demo cases are clearly marked: [DEMO TEST FIXTURE]")
	fmt.Println(" Provenance notice: SIMULATED DEMO DATA — NOT A LIVE PAYPAL TRANSACTION")
	fmt.Println("==================================================================")
}
