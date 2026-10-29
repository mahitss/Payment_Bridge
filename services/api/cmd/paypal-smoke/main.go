package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/logger"
	"claimbridge/services/api/internal/paypal"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithClient(args, stdout, stderr, nil)
}

func runWithClient(args []string, stdout, stderr io.Writer, injectedClient *paypal.Client) int {
	fs := flag.NewFlagSet("paypal-smoke", flag.ContinueOnError)
	fs.SetOutput(stderr)

	batchIDFlag := fs.String("batch-id", "", "Optional PayPal Payout Batch ID to query status")
	itemIDFlag := fs.String("item-id", "", "Optional PayPal Payout Item ID to query item status")
	cancelItemFlag := fs.String("cancel-item", "", "Optional PayPal Payout Item ID to cancel (must be UNCLAIMED)")
	createPayoutFlag := fs.Bool("create-test-payout", false, "Explicitly create a single test sandbox payout")
	recipientFlag := fs.String("recipient", "", "Sandbox recipient email for test payout")
	amountFlag := fs.String("amount", "1.00", "Amount for test payout (e.g. 1.00)")
	currencyFlag := fs.String("currency", "USD", "Currency code (default USD)")
	confirmSandboxFlag := fs.Bool("confirm-sandbox-only", false, "Required confirmation flag to authorize test payout creation")
	envFileFlag := fs.String("env-file", "", "Path to .env file to load (defaults to standard search paths)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	// 1. Initialize Redacting Logger
	logger.InitLogger("info", stdout)

	fmt.Fprintln(stdout, "================================================================================")
	fmt.Fprintln(stdout, "         CLAIMBRIDGE — PAYPAL SANDBOX INTEGRATION VERIFICATION TOOL            ")
	fmt.Fprintln(stdout, "================================================================================")

	// Attempt to load .env file from candidate paths
	loadedPath, envErr := config.LoadEnvFiles(*envFileFlag)
	if envErr != nil {
		fmt.Fprintf(stdout, "[WARN] Failed reading environment file at %s: %v\n", loadedPath, envErr)
	} else if loadedPath != "" {
		fmt.Fprintf(stdout, "[INFO] Loaded environment configuration from: %s\n", loadedPath)
	} else {
		fmt.Fprintln(stdout, "[INFO] No local .env file found in search paths; relying on environment variables.")
	}

	// 2. Load and Validate Configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(stderr, "\n[CRITICAL ERROR] Configuration failed security validation:\n  -> %v\n", err)
		return 1
	}

	// 3. Strict Sandbox Guardrails Assertion
	if cfg.PayPalEnv != "sandbox" {
		fmt.Fprintf(stderr, "\n[SECURITY VIOLATION] PAYPAL_ENV is set to '%s'. Only 'sandbox' is permitted.\n", cfg.PayPalEnv)
		return 1
	}

	effectiveBaseURL := cfg.PayPalBaseURL
	if injectedClient != nil {
		effectiveBaseURL = injectedClient.BaseURL()
	}

	fmt.Fprintln(stdout, "\n[1/3] Environment Security Audit:")
	fmt.Fprintf(stdout, "  • Application Environment : %s\n", cfg.AppEnv)
	fmt.Fprintf(stdout, "  • PayPal Target Mode      : %s [VERIFIED SANDBOX]\n", cfg.PayPalEnv)
	fmt.Fprintf(stdout, "  • PayPal Base URL         : %s\n", effectiveBaseURL)
	fmt.Fprintf(stdout, "  • Request Timeout         : %s\n", cfg.PayPalRequestTimeout)

	// 4. Credential Availability Check
	credErr := cfg.ValidatePayPalCredentials()
	if credErr != nil {
		fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
		fmt.Fprintln(stdout, "[STATUS: BLOCKED] PayPal Sandbox Credentials Not Available")
		fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
		fmt.Fprintf(stdout, "Root Cause: %v\n\n", credErr)
		fmt.Fprintln(stdout, "Exact Blocker Details:")
		if cfg.PayPalClientID == "" {
			fmt.Fprintln(stdout, "  - PAYPAL_CLIENT_ID is unset or empty.")
		}
		if cfg.PayPalClientSecret == "" {
			fmt.Fprintln(stdout, "  - PAYPAL_CLIENT_SECRET is unset or empty.")
		}
		fmt.Fprintln(stdout, "\nRequired Action to Unblock:")
		fmt.Fprintln(stdout, "  1. Review: docs/operational-instructions.md")
		fmt.Fprintln(stdout, "  2. Create a Sandbox App at https://developer.paypal.com/dashboard/applications/sandbox")
		fmt.Fprintln(stdout, "  3. Ensure 'Payouts' permission is enabled in App Settings.")
		fmt.Fprintln(stdout, "  4. Copy credentials into .env in the repository root:")
		fmt.Fprintln(stdout, "       PAYPAL_CLIENT_ID=<your-sandbox-client-id>")
		fmt.Fprintln(stdout, "       PAYPAL_CLIENT_SECRET=<your-sandbox-client-secret>")
		fmt.Fprintln(stdout, "  5. Re-run: go run ./cmd/paypal-smoke")
		fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
		return 3 // Code 3: Blocked / Credentials Missing
	}

	// 5. Initialize PayPal Sandbox Client
	client := injectedClient
	if client == nil {
		var cErr error
		client, cErr = paypal.NewClient(cfg, nil)
		if cErr != nil {
			fmt.Fprintf(stderr, "\n[ERROR] Failed to construct PayPal sandbox client: %v\n", cErr)
			return 1
		}
	}

	// 6. Test OAuth2 Authentication
	fmt.Fprintln(stdout, "\n[2/3] Performing OAuth2 Client Credentials Grant with PayPal Sandbox...")
	ctx, cancel := context.WithTimeout(context.Background(), cfg.PayPalRequestTimeout)
	defer cancel()

	_, err = client.GetAccessToken(ctx)
	if err != nil {
		fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
		fmt.Fprintln(stdout, "[STATUS: FAILED] PayPal Sandbox Authentication Failed")
		fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
		if errors.Is(err, paypal.ErrUnauthorized) {
			fmt.Fprintln(stdout, "Error: Invalid client credentials (HTTP 401 Unauthorized).")
			fmt.Fprintln(stdout, "Check that the Client ID and Secret match your sandbox app in PayPal Developer Dashboard.")
		} else if errors.Is(err, paypal.ErrForbidden) {
			fmt.Fprintln(stdout, "Error: PayPal account or client lacks Payouts permissions (HTTP 403 Forbidden).")
			if fe, ok := paypal.AsAPIForbiddenError(err); ok && (fe.Name != "" || fe.Issue != "" || fe.DebugID != "") {
				fmt.Fprintf(stdout, "Details: %s\n", fe.Error())
			}
			fmt.Fprintln(stdout, "Action: Enable 'Payouts' permission under App Features in the PayPal Developer Dashboard.")
		} else {
			fmt.Fprintf(stdout, "Error: %v\n", err)
		}
		return 2 // Code 2: OAuth Failure
	}

	fmt.Fprintln(stdout, "\n  [SUCCESS] OAuth2 Authentication Verified!")
	fmt.Fprintln(stdout, "  • Token Type : Bearer")
	fmt.Fprintln(stdout, "  • Token State: Cached & Valid")
	fmt.Fprintf(stdout,  "  • Payouts Scope Granted : %t\n", client.HasPayoutsScope())
	fmt.Fprintln(stdout, "  • Token Hash : [REDACTED_ACCESS_TOKEN] (never exposed to logs or disk)")

	// 7. Optional: Query Payout Batch Status / Item Status / Cancel / Create
	if *batchIDFlag != "" {
		fmt.Fprintf(stdout, "\n[3/3] Querying Payout Batch Status for: %s\n", *batchIDFlag)
		batch, err := client.GetPayoutBatch(ctx, *batchIDFlag)
		if err != nil {
			fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
			fmt.Fprintf(stdout, "[STATUS: FAILED] Query for Payout Batch ID '%s' failed\n", *batchIDFlag)
			fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
			if errors.Is(err, paypal.ErrPayoutNotFound) {
				fmt.Fprintf(stdout, "Error: Batch ID '%s' was not found in sandbox (HTTP 404).\n", *batchIDFlag)
			} else if errors.Is(err, paypal.ErrForbidden) {
				fmt.Fprintln(stdout, "Error: Insufficient permission to view payout batches (HTTP 403).")
			} else {
				fmt.Fprintf(stdout, "Error querying batch: %v\n", err)
			}
			return 1
		}

		fmt.Fprintln(stdout, "  [SUCCESS] Payout Batch Details Retrieved:")
		fmt.Fprintf(stdout, "    - Batch ID    : %s\n", batch.BatchHeader.PayoutBatchID)
		fmt.Fprintf(stdout, "    - Status      : %s\n", batch.BatchHeader.BatchStatus)
		fmt.Fprintf(stdout, "    - Time Created: %s\n", batch.BatchHeader.TimeCreated)
		if batch.BatchHeader.Amount != nil {
			fmt.Fprintf(stdout, "    - Total Amount: %s %s\n", batch.BatchHeader.Amount.Value, batch.BatchHeader.Amount.Currency)
		}
		fmt.Fprintf(stdout, "    - Item Count  : %d\n", len(batch.Items))

	} else if *itemIDFlag != "" {
		fmt.Fprintf(stdout, "\n[3/3] Querying Payout Item Status for: %s\n", *itemIDFlag)
		item, err := client.GetPayoutItem(ctx, *itemIDFlag)
		if err != nil {
			fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
			fmt.Fprintf(stdout, "[STATUS: FAILED] Query for Payout Item ID '%s' failed\n", *itemIDFlag)
			fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
			if errors.Is(err, paypal.ErrPayoutNotFound) {
				fmt.Fprintf(stdout, "Error: Item ID '%s' was not found in sandbox (HTTP 404).\n", *itemIDFlag)
			} else if errors.Is(err, paypal.ErrForbidden) {
				fmt.Fprintln(stdout, "Error: Insufficient permission to view payout items (HTTP 403).")
			} else {
				fmt.Fprintf(stdout, "Error querying item: %v\n", err)
			}
			return 1
		}

		fmt.Fprintln(stdout, "  [SUCCESS] Payout Item Details Retrieved:")
		fmt.Fprintf(stdout, "    - Item ID       : %s\n", item.PayoutItemID)
		fmt.Fprintf(stdout, "    - Status        : %s\n", item.TransactionStatus)
		fmt.Fprintf(stdout, "    - Receiver      : %s\n", ai.MaskEmail(item.Receiver))
		fmt.Fprintf(stdout, "    - Amount        : %s %s\n", item.Amount.Value, item.Amount.Currency)

	} else if *cancelItemFlag != "" {
		fmt.Fprintf(stdout, "\n[3/3] Requesting Cancellation for Payout Item: %s\n", *cancelItemFlag)
		cancelled, err := client.CancelPayoutItem(ctx, *cancelItemFlag)
		if err != nil {
			fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
			fmt.Fprintf(stdout, "[STATUS: FAILED] Cancellation for Payout Item ID '%s' failed\n", *cancelItemFlag)
			fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
			fmt.Fprintf(stdout, "Error: %v\n", err)
			return 1
		}

		fmt.Fprintln(stdout, "  [SUCCESS] Payout Item Cancelled Successfully:")
		fmt.Fprintf(stdout, "    - Item ID: %s\n", cancelled.PayoutItemID)
		fmt.Fprintf(stdout, "    - Status : %s\n", cancelled.TransactionStatus)

	} else if *createPayoutFlag {
		fmt.Fprintln(stdout, "\n[3/3] Explicit Test Payout Dispatch Requested...")

		if !*confirmSandboxFlag {
			fmt.Fprintln(stderr, "\n[SAFETY ABORT] Payout creation requires the explicit '-confirm-sandbox-only' flag.")
			fmt.Fprintln(stderr, "Example: go run ./cmd/paypal-smoke -create-test-payout -recipient test@business.example.com -confirm-sandbox-only")
			return 1
		}

		if *recipientFlag == "" {
			fmt.Fprintln(stderr, "\n[INPUT ERROR] -recipient email is required when -create-test-payout is used.")
			return 1
		}

		senderBatchID := fmt.Sprintf("claimbridge-smoke-%d", time.Now().Unix())
		fmt.Fprintf(stdout, "  • Deterministic Sender Batch ID: %s\n", senderBatchID)
		fmt.Fprintf(stdout, "  • Recipient                   : %s\n", ai.MaskEmail(*recipientFlag))
		fmt.Fprintf(stdout, "  • Amount                      : %s %s\n", *amountFlag, *currencyFlag)

		payoutResp, err := client.CreatePayout(ctx, paypal.CreatePayoutRequest{
			SenderBatchID: senderBatchID,
			Recipient:     *recipientFlag,
			Amount:        *amountFlag,
			Currency:      *currencyFlag,
			Note:          "ClaimBridge Sandbox Feasibility Smoke Test",
			EmailSubject:  "ClaimBridge Test Settlement",
		})
		if err != nil {
			fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
			fmt.Fprintln(stdout, "[STATUS: FAILED] Sandbox Payout Creation Failed")
			fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
			fmt.Fprintf(stdout, "Error: %v\n", err)
			return 1
		}

		fmt.Fprintln(stdout, "\n  [SUCCESS] Test Payout Dispatched to Sandbox!")
		fmt.Fprintf(stdout, "    - Payout Batch ID: %s\n", payoutResp.BatchHeader.PayoutBatchID)
		fmt.Fprintf(stdout, "    - Batch Status   : %s\n", payoutResp.BatchHeader.BatchStatus)
		fmt.Fprintf(stdout, "    - Time Created   : %s\n", payoutResp.BatchHeader.TimeCreated)
	} else {
		fmt.Fprintln(stdout, "\n[3/3] Sandbox Read-Only Feasibility Mode:")
		fmt.Fprintln(stdout, "  • Automatic payouts are DISABLED by default for safety.")
		fmt.Fprintln(stdout, "  • To test payout creation: specify -create-test-payout -recipient <email> -confirm-sandbox-only")
		fmt.Fprintln(stdout, "  • To inspect a batch    : specify -batch-id <payout-batch-id>")
	}

	fmt.Fprintln(stdout, "\n================================================================================")
	fmt.Fprintln(stdout, "                 FEASIBILITY VERIFICATION COMPLETED SUCCESSFULLY                ")
	fmt.Fprintln(stdout, "================================================================================")
	return 0
}
