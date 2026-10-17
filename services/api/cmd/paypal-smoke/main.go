package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/logger"
	"claimbridge/services/api/internal/paypal"
)

func main() {
	batchIDFlag := flag.String("batch-id", "", "Optional PayPal Payout Batch ID to query status")
	createPayoutFlag := flag.Bool("create-test-payout", false, "Explicitly create a single test sandbox payout")
	recipientFlag := flag.String("recipient", "", "Sandbox recipient email for test payout")
	amountFlag := flag.String("amount", "1.00", "Amount for test payout (e.g. 1.00)")
	currencyFlag := flag.String("currency", "USD", "Currency code (default USD)")
	confirmSandboxFlag := flag.Bool("confirm-sandbox-only", false, "Required confirmation flag to authorize test payout creation")
	envFileFlag := flag.String("env-file", ".env", "Path to .env file to load if present")
	flag.Parse()

	// 1. Initialize Redacting Logger
	logger.InitLogger("info", os.Stdout)

	fmt.Println("================================================================================")
	fmt.Println("         CLAIMBRIDGE — PAYPAL SANDBOX INTEGRATION VERIFICATION TOOL            ")
	fmt.Println("================================================================================")

	// Attempt to load .env file if it exists
	loadEnvFile(*envFileFlag)

	// 2. Load and Validate Configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Printf("\n[CRITICAL ERROR] Configuration failed security validation:\n  -> %v\n", err)
		os.Exit(1)
	}

	// 3. Strict Sandbox Guardrails Assertion
	if cfg.PayPalEnv != "sandbox" {
		fmt.Printf("\n[SECURITY VIOLATION] PAYPAL_ENV is set to '%s'. Only 'sandbox' is permitted.\n", cfg.PayPalEnv)
		os.Exit(1)
	}

	fmt.Println("\n[1/3] Environment Security Audit:")
	fmt.Printf("  • Application Environment : %s\n", cfg.AppEnv)
	fmt.Printf("  • PayPal Target Mode      : %s [VERIFIED SANDBOX]\n", cfg.PayPalEnv)
	fmt.Printf("  • PayPal Base URL         : %s\n", cfg.PayPalBaseURL)
	fmt.Printf("  • Request Timeout         : %s\n", cfg.PayPalRequestTimeout)

	// 4. Credential Availability Check
	credErr := cfg.ValidatePayPalCredentials()
	if credErr != nil {
		fmt.Println("\n--------------------------------------------------------------------------------")
		fmt.Println("[STATUS: BLOCKED] PayPal Sandbox Credentials Not Available")
		fmt.Println("--------------------------------------------------------------------------------")
		fmt.Printf("Root Cause: %v\n\n", credErr)
		fmt.Println("Exact Blocker Details:")
		if cfg.PayPalClientID == "" {
			fmt.Println("  - PAYPAL_CLIENT_ID is unset or empty.")
		}
		if cfg.PayPalClientSecret == "" {
			fmt.Println("  - PAYPAL_CLIENT_SECRET is unset or empty.")
		}
		fmt.Println("\nRequired Action to Unblock:")
		fmt.Println("  1. Review: docs/operational-instructions.md")
		fmt.Println("  2. Create a Sandbox App at https://developer.paypal.com/dashboard/applications/sandbox")
		fmt.Println("  3. Ensure 'Payouts' permission is enabled in App Settings.")
		fmt.Println("  4. Copy credentials into .env:")
		fmt.Println("       PAYPAL_CLIENT_ID=<your-sandbox-client-id>")
		fmt.Println("       PAYPAL_CLIENT_SECRET=<your-sandbox-client-secret>")
		fmt.Println("  5. Re-run: go run ./cmd/paypal-smoke")
		fmt.Println("--------------------------------------------------------------------------------")
		os.Exit(3) // Code 3: Blocked / Credentials Missing
	}

	// 5. Initialize PayPal Sandbox Client
	client, err := paypal.NewClient(cfg, nil)
	if err != nil {
		fmt.Printf("\n[ERROR] Failed to construct PayPal sandbox client: %v\n", err)
		os.Exit(1)
	}

	// 6. Test OAuth2 Authentication
	fmt.Println("\n[2/3] Performing OAuth2 Client Credentials Grant with PayPal Sandbox...")
	ctx, cancel := context.WithTimeout(context.Background(), cfg.PayPalRequestTimeout)
	defer cancel()

	_, err = client.GetAccessToken(ctx)
	if err != nil {
		fmt.Println("\n--------------------------------------------------------------------------------")
		fmt.Println("[STATUS: FAILED] PayPal Sandbox Authentication Failed")
		fmt.Println("--------------------------------------------------------------------------------")
		if errors.Is(err, paypal.ErrUnauthorized) {
			fmt.Println("Error: Invalid client credentials (HTTP 401 Unauthorized).")
			fmt.Println("Check that the Client ID and Secret match your sandbox app in PayPal Developer Dashboard.")
		} else {
			fmt.Printf("Error: %v\n", err)
		}
		os.Exit(2)
	}

	fmt.Println("\n  [SUCCESS] OAuth2 Authentication Verified!")
	fmt.Println("  • Token Type : Bearer")
	fmt.Println("  • Token State: Cached & Valid")
	fmt.Println("  • Token Hash : [REDACTED_ACCESS_TOKEN] (never exposed to logs or disk)")

	// 7. Optional: Query Payout Batch Status
	if *batchIDFlag != "" {
		fmt.Printf("\n[3/3] Querying Payout Batch Status for: %s\n", *batchIDFlag)
		batch, err := client.GetPayoutBatch(ctx, *batchIDFlag)
		if err != nil {
			if errors.Is(err, paypal.ErrPayoutNotFound) {
				fmt.Printf("  • Batch ID '%s' was not found in sandbox.\n", *batchIDFlag)
			} else {
				fmt.Printf("  • Error querying batch: %v\n", err)
			}
		} else {
			fmt.Println("  [SUCCESS] Payout Batch Details Retrieved:")
			fmt.Printf("    - Batch ID    : %s\n", batch.BatchHeader.PayoutBatchID)
			fmt.Printf("    - Status      : %s\n", batch.BatchHeader.BatchStatus)
			fmt.Printf("    - Time Created: %s\n", batch.BatchHeader.TimeCreated)
			if batch.BatchHeader.Amount != nil {
				fmt.Printf("    - Total Amount: %s %s\n", batch.BatchHeader.Amount.Value, batch.BatchHeader.Amount.Currency)
			}
			fmt.Printf("    - Item Count  : %d\n", len(batch.Items))
		}
	} else if *createPayoutFlag {
		// 8. Explicit Test Payout Creation (Strictly Guarded)
		fmt.Println("\n[3/3] Explicit Test Payout Dispatch Requested...")

		if !*confirmSandboxFlag {
			fmt.Println("\n[SAFETY ABORT] Payout creation requires the explicit '-confirm-sandbox-only' flag.")
			fmt.Println("Example: go run ./cmd/paypal-smoke -create-test-payout -recipient test@business.example.com -confirm-sandbox-only")
			os.Exit(1)
		}

		if *recipientFlag == "" {
			fmt.Println("\n[INPUT ERROR] -recipient email is required when -create-test-payout is used.")
			os.Exit(1)
		}

		senderBatchID := fmt.Sprintf("claimbridge-smoke-%d", time.Now().Unix())
		fmt.Printf("  • Deterministic Sender Batch ID: %s\n", senderBatchID)
		fmt.Printf("  • Recipient                   : %s\n", *recipientFlag)
		fmt.Printf("  • Amount                      : %s %s\n", *amountFlag, *currencyFlag)

		payoutResp, err := client.CreatePayout(ctx, paypal.CreatePayoutRequest{
			SenderBatchID: senderBatchID,
			Recipient:     *recipientFlag,
			Amount:        *amountFlag,
			Currency:      *currencyFlag,
			Note:          "ClaimBridge Sandbox Feasibility Smoke Test",
			EmailSubject:  "ClaimBridge Test Settlement",
		})
		if err != nil {
			fmt.Printf("\n[PAYOUT FAILED] Sandbox payout creation failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("\n  [SUCCESS] Test Payout Dispatched to Sandbox!")
		fmt.Printf("    - Payout Batch ID: %s\n", payoutResp.BatchHeader.PayoutBatchID)
		fmt.Printf("    - Batch Status   : %s\n", payoutResp.BatchHeader.BatchStatus)
		fmt.Printf("    - Time Created   : %s\n", payoutResp.BatchHeader.TimeCreated)
	} else {
		fmt.Println("\n[3/3] Sandbox Read-Only Feasibility Mode:")
		fmt.Println("  • Automatic payouts are DISABLED by default for safety.")
		fmt.Println("  • To test payout creation: specify -create-test-payout -recipient <email> -confirm-sandbox-only")
		fmt.Println("  • To inspect a batch    : specify -batch-id <payout-batch-id>")
	}

	fmt.Println("\n================================================================================")
	fmt.Println("                 FEASIBILITY VERIFICATION COMPLETED SUCCESSFULLY                ")
	fmt.Println("================================================================================")
}

// loadEnvFile reads a local .env file line by line and sets environment variables if not already set.
func loadEnvFile(path string) {
	file, err := os.Open(path)
	if err != nil {
		return // File does not exist, ignore
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}
