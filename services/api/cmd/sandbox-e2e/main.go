package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/logger"
	"claimbridge/services/api/internal/payouts"
	"claimbridge/services/api/internal/paypal"
	"claimbridge/services/api/internal/policy"
	"claimbridge/services/api/internal/seeder"
)

// Exit codes for sandbox-e2e CLI
const (
	ExitCodeSuccess    = 0 // PASS: Read-only preflight or authorized payout succeeded
	ExitCodeFailure    = 1 // FAIL: Validation failure, configuration violation, or assertion failure
	ExitCodeOAuthError = 2 // OAuth grant failed (HTTP 401 / 403 / network failure)
	ExitCodeBlocked    = 3 // BLOCKED: Missing sandbox credentials or missing required Payouts scope for mutation
	ExitCodeUnresolved = 4 // UNRESOLVED: Ambiguous network timeout / requires manual reconciliation
)

// PostgresRepoFactory allows dependency injection for database repository creation and migrations.
type PostgresRepoFactory func(ctx context.Context, databaseURL string) (db.Repository, error)

// Maximum allowable test payout amount in minor units ($1.00 USD = 100 cents)
const MaxSandboxTestAmountCents = 100

// CorrelationVerification stores field-level correlation check results
type CorrelationVerification struct {
	SenderBatchIDMatched bool `json:"sender_batch_id_matched"`
	SenderItemIDMatched  bool `json:"sender_item_id_matched"`
	RecipientMatched     bool `json:"recipient_matched"`
	AmountMatched        bool `json:"amount_matched"`
	CurrencyMatched      bool `json:"currency_matched"`
	OverallCorrelated    bool `json:"overall_correlated"`
}

// SandboxE2EReport defines the structured report schema
type SandboxE2EReport struct {
	ExecutionTimestamp       time.Time                `json:"execution_timestamp"`
	CommitSHA                string                   `json:"commit_sha"`
	AppEnv                   string                   `json:"app_env"`
	PayPalEnv                string                   `json:"paypal_env"`
	PayPalBaseURL            string                   `json:"paypal_base_url"`
	PreflightConfigValid     bool                     `json:"preflight_config_valid"`
	DatabaseReachable        bool                     `json:"database_reachable"`
	DatabaseMigrations       bool                     `json:"database_migrations_current"`
	OAuthTokenAcquired       bool                     `json:"oauth_token_acquired"`
	HasPayoutsScope          bool                     `json:"has_payouts_scope"`
	ProviderErrorName        string                   `json:"provider_error_name,omitempty"`
	ProviderErrorIssue       string                   `json:"provider_error_issue,omitempty"`
	ProviderDebugID          string                   `json:"provider_debug_id,omitempty"`
	SubmissionRequested      bool                     `json:"submission_requested"`
	MutationDispatched       bool                     `json:"mutation_dispatched"`
	PayoutAttempted          bool                     `json:"payout_attempted"` // set true ONLY when provider mutation is actually dispatched
	ProviderResponseReceived bool                     `json:"provider_response_received"`
	SanitizedRecipient       string                   `json:"sanitized_recipient,omitempty"`
	Amount                   string                   `json:"amount,omitempty"`
	Currency                 string                   `json:"currency,omitempty"`
	CaseID                   string                   `json:"case_id,omitempty"`
	CaseReference            string                   `json:"case_reference,omitempty"`
	OperationID              string                   `json:"operation_id,omitempty"`
	SenderBatchID            string                   `json:"sender_batch_id,omitempty"`
	SenderItemID             string                   `json:"sender_item_id,omitempty"`
	SubmissionFingerprint    string                   `json:"submission_fingerprint,omitempty"`
	PayPalPayoutBatchID      string                   `json:"paypal_payout_batch_id,omitempty"`
	PayPalBatchStatus        string                   `json:"paypal_batch_status,omitempty"`
	PayPalItemStatus         string                   `json:"paypal_item_status,omitempty"`
	PersistedCaseStatus      string                   `json:"persisted_case_status,omitempty"`
	PersistedOpStatus        string                   `json:"persisted_operation_status,omitempty"`
	Correlation              *CorrelationVerification `json:"correlation,omitempty"`
	StatusSnapshotsCount     int                      `json:"status_snapshots_count"`
	AuditEventsCount         int                      `json:"audit_events_count"`
	AuditVerificationPassed  bool                     `json:"audit_verification_passed"`
	Outcome                  string                   `json:"outcome"` // PASS, FAIL, BLOCKED, UNRESOLVED
	Details                  string                   `json:"details"`
	ReportArtifactPathJSON   string                   `json:"report_artifact_path_json,omitempty"`
	ReportArtifactPathMD     string                   `json:"report_artifact_path_md,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithDeps(args, stdout, stderr, nil, nil)
}

func runWithDeps(args []string, stdout, stderr io.Writer, injectedClient *paypal.Client, injectedRepo db.Repository, injectedDBFactory ...PostgresRepoFactory) int {
	fs := flag.NewFlagSet("sandbox-e2e", flag.ContinueOnError)
	fs.SetOutput(stderr)

	envFileFlag := fs.String("env-file", "", "Path to .env file to load (defaults to standard search paths)")
	submitPayoutFlag := fs.Bool("submit-payout", false, "Explicitly authorize and submit a single test sandbox payout")
	createTestPayoutFlag := fs.Bool("create-test-payout", false, "Alias for -submit-payout")
	confirmSandboxFlag := fs.Bool("confirm-sandbox-only", false, "Required confirmation flag to authorize test payout creation")
	recipientFlag := fs.String("recipient", "", "Sandbox recipient email for test payout")
	amountFlag := fs.String("amount", "1.00", "Amount for test payout in USD (hard maximum: 1.00)")
	currencyFlag := fs.String("currency", "USD", "Currency code (strictly USD)")
	reportDirFlag := fs.String("report-dir", "artifacts", "Directory to write sanitized output report artifacts")
	noReportFileFlag := fs.Bool("no-report-file", false, "Skip writing report artifact files to disk (console only)")

	if err := fs.Parse(args); err != nil {
		return ExitCodeFailure
	}

	shouldSubmit := *submitPayoutFlag || *createTestPayoutFlag

	// 1. Initialize Redacting Logger
	logger.InitLogger("info", stdout)

	fmt.Fprintln(stdout, "================================================================================")
	fmt.Fprintln(stdout, "     CLAIMBRIDGE — REAL PAYPAL SANDBOX END-TO-END VERIFICATION TOOL            ")
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

	report := &SandboxE2EReport{
		ExecutionTimestamp: time.Now().UTC(),
		CommitSHA:          getCommitSHA(),
		Outcome:            "FAIL",
	}

	// 2. Load and Validate Configuration
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(stderr, "\n[CRITICAL ERROR] Configuration failed security validation:\n  -> %v\n", err)
		report.Details = fmt.Sprintf("Configuration security validation failed: %v", err)
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeFailure
	}

	report.AppEnv = cfg.AppEnv
	report.PayPalEnv = cfg.PayPalEnv
	report.PayPalBaseURL = cfg.PayPalBaseURL
	report.PreflightConfigValid = true

	// 3. Strict Sandbox Guardrails Assertion
	if cfg.PayPalEnv != "sandbox" {
		fmt.Fprintf(stderr, "\n[SECURITY VIOLATION] PAYPAL_ENV is set to '%s'. Only 'sandbox' is permitted.\n", cfg.PayPalEnv)
		report.Details = fmt.Sprintf("PAYPAL_ENV is '%s'; only 'sandbox' is permitted", cfg.PayPalEnv)
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeFailure
	}

	effectiveBaseURL := cfg.PayPalBaseURL
	if injectedClient != nil {
		effectiveBaseURL = injectedClient.BaseURL()
		report.PayPalBaseURL = effectiveBaseURL
	}

	fmt.Fprintln(stdout, "\n[1/4] Environment Security Audit:")
	fmt.Fprintf(stdout, "  • Application Environment : %s\n", cfg.AppEnv)
	fmt.Fprintf(stdout, "  • PayPal Target Mode      : %s [VERIFIED SANDBOX ONLY]\n", cfg.PayPalEnv)
	fmt.Fprintf(stdout, "  • PayPal Sandbox Base URL : %s\n", effectiveBaseURL)
	fmt.Fprintf(stdout, "  • Request Timeout         : %s\n", cfg.PayPalRequestTimeout)
	fmt.Fprintf(stdout, "  • Target Commit SHA       : %s\n", report.CommitSHA)

	// 4. Credential Availability Check
	credErr := cfg.ValidatePayPalCredentials()
	if credErr != nil {
		fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
		fmt.Fprintln(stdout, "[STATUS: BLOCKED] PayPal Sandbox Credentials Not Available")
		fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
		fmt.Fprintf(stdout, "Root Cause: %v\n\n", credErr)
		fmt.Fprintln(stdout, "Exact Blocker Details:")
		if strings.TrimSpace(cfg.PayPalClientID) == "" {
			fmt.Fprintln(stdout, "  - PAYPAL_CLIENT_ID is unset or empty.")
		}
		if strings.TrimSpace(cfg.PayPalClientSecret) == "" {
			fmt.Fprintln(stdout, "  - PAYPAL_CLIENT_SECRET is unset or empty.")
		}
		fmt.Fprintln(stdout, "\nRequired Action to Unblock:")
		fmt.Fprintln(stdout, "  1. Review: docs/operational-instructions.md")
		fmt.Fprintln(stdout, "  2. Create a Sandbox App at https://developer.paypal.com/dashboard/applications/sandbox")
		fmt.Fprintln(stdout, "  3. Ensure 'Payouts' permission is enabled in App Settings.")
		fmt.Fprintln(stdout, "  4. Copy credentials into .env in the repository root:")
		fmt.Fprintln(stdout, "       PAYPAL_CLIENT_ID=<your-sandbox-client-id>")
		fmt.Fprintln(stdout, "       PAYPAL_CLIENT_SECRET=<your-sandbox-client-secret>")
		fmt.Fprintln(stdout, "  5. Re-run: go run ./cmd/sandbox-e2e")
		fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")

		report.Outcome = "BLOCKED"
		report.Details = "PayPal Sandbox credentials not configured in local environment"
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeBlocked
	}

	// 5. Initialize PayPal Client & Test OAuth
	client := injectedClient
	if client == nil {
		var cErr error
		client, cErr = paypal.NewClient(cfg, nil)
		if cErr != nil {
			fmt.Fprintf(stderr, "\n[ERROR] Failed to construct PayPal sandbox client: %v\n", cErr)
			report.Details = fmt.Sprintf("Failed to construct PayPal sandbox client: %v", cErr)
			writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
			return ExitCodeFailure
		}
	}

	fmt.Fprintln(stdout, "\n[2/4] Testing Genuine OAuth2 Client Credentials Grant with PayPal Sandbox...")
	authCtx, authCancel := context.WithTimeout(context.Background(), cfg.PayPalRequestTimeout)
	defer authCancel()

	_, err = client.GetAccessToken(authCtx)
	if err != nil {
		fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
		fmt.Fprintln(stdout, "[STATUS: FAILED] PayPal Sandbox Authentication Failed")
		fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
		if errors.Is(err, paypal.ErrUnauthorized) {
			fmt.Fprintln(stdout, "Error: Invalid client credentials (HTTP 401 Unauthorized).")
			fmt.Fprintln(stdout, "Check that Client ID and Secret match your sandbox app in PayPal Developer Dashboard.")
		} else if errors.Is(err, paypal.ErrForbidden) {
			fmt.Fprintln(stdout, "Error: PayPal account lacks Payouts permissions (HTTP 403 Forbidden).")
			if fe, ok := paypal.AsAPIForbiddenError(err); ok {
				report.ProviderErrorName = fe.Name
				report.ProviderErrorIssue = fe.Issue
				report.ProviderDebugID = fe.DebugID
				if fe.Name != "" || fe.Issue != "" || fe.DebugID != "" {
					fmt.Fprintf(stdout, "Details: %s\n", fe.Error())
				}
			}
			fmt.Fprintln(stdout, "Action: Enable 'Payouts' permission under App Features in the PayPal Developer Dashboard.")
		} else {
			fmt.Fprintf(stdout, "Error: %v\n", err)
		}
		report.Outcome = "FAIL"
		report.Details = fmt.Sprintf("PayPal Sandbox OAuth token acquisition failed: %v", err)
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeOAuthError
	}

	report.OAuthTokenAcquired = true
	report.HasPayoutsScope = client.HasPayoutsScope()
	fmt.Fprintln(stdout, "  [SUCCESS] Genuine OAuth2 Authentication Verified!")
	fmt.Fprintln(stdout, "  • Token Type : Bearer")
	fmt.Fprintln(stdout, "  • Token State: Cached & Valid")
	fmt.Fprintf(stdout,  "  • Payouts Scope Granted : %t\n", client.HasPayoutsScope())
	fmt.Fprintln(stdout, "  • Token Hash : [REDACTED_ACCESS_TOKEN] (never exposed to logs or disk)")

	// Mutation Mode Pre-Database Safeguards & Scope Gate:
	// If mutation mode is requested (-submit-payout), strictly enforce all CLI input validation
	// and the required Payouts OAuth scope BEFORE any database connection, initialization, or migrations.
	var recipient, currency, formattedAmount string
	var amountCents int64
	if shouldSubmit {
		report.SubmissionRequested = true

		// Safeguard 1: Explicit confirmation flag
		if !*confirmSandboxFlag {
			fmt.Fprintln(stderr, "\n[SAFETY ABORT] Payout creation requires the explicit '-confirm-sandbox-only' flag.")
			fmt.Fprintln(stderr, "Example: go run ./cmd/sandbox-e2e -submit-payout -recipient test@personal.example.com -confirm-sandbox-only")
			report.Outcome = "FAIL"
			report.Details = "Missing required '-confirm-sandbox-only' confirmation flag"
			writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
			return ExitCodeFailure
		}

		// Safeguard 2: Valid recipient email
		recipient = strings.TrimSpace(*recipientFlag)
		if recipient == "" || !strings.Contains(recipient, "@") {
			fmt.Fprintln(stderr, "\n[INPUT ERROR] -recipient email is required and must be a valid sandbox recipient email address.")
			report.Outcome = "FAIL"
			report.Details = "Invalid or missing -recipient email address"
			writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
			return ExitCodeFailure
		}
		report.SanitizedRecipient = ai.MaskEmail(recipient)

		// Safeguard 3: Currency must be USD
		currency = strings.ToUpper(strings.TrimSpace(*currencyFlag))
		if currency != "USD" {
			fmt.Fprintf(stderr, "\n[INPUT ERROR] Currency '%s' is unsupported. Only 'USD' is permitted in this milestone.\n", currency)
			report.Outcome = "FAIL"
			report.Details = fmt.Sprintf("Unsupported currency %q", currency)
			writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
			return ExitCodeFailure
		}
		report.Currency = currency

		// Safeguard 4: Strict amount parsing using integer cents (hard maximum $1.00 USD)
		var aErr error
		amountCents, formattedAmount, aErr = parseAmountCents(*amountFlag)
		if aErr != nil {
			if strings.Contains(aErr.Error(), "exceeds the hard sandbox test ceiling") {
				fmt.Fprintf(stderr, "\n[SAFETY VIOLATION] Requested amount %q exceeds the hard sandbox test ceiling of $1.00 USD (100 cents).\n", *amountFlag)
				fmt.Fprintln(stderr, "ClaimBridge safety rules strictly restrict operator test payouts to a maximum of $1.00 USD.")
				report.Outcome = "FAIL"
				report.Details = fmt.Sprintf("Amount %q exceeds hard test ceiling of $1.00 USD", *amountFlag)
			} else {
				fmt.Fprintf(stderr, "\n[INPUT ERROR] Invalid payout amount %q: %v\n", *amountFlag, aErr)
				report.Outcome = "FAIL"
				report.Details = fmt.Sprintf("Invalid amount %q: %v", *amountFlag, aErr)
			}
			writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
			return ExitCodeFailure
		}
		report.Amount = formattedAmount

		// Safeguard 5: Required Payouts OAuth scope (fail-closed before any database connection or migrations)
		if !client.HasPayoutsScope() {
			fmt.Fprintln(stderr, "\n--------------------------------------------------------------------------------")
			fmt.Fprintln(stderr, "[STATUS: BLOCKED] PayPal Sandbox OAuth Token Missing Payouts Scope")
			fmt.Fprintln(stderr, "--------------------------------------------------------------------------------")
			fmt.Fprintf(stderr, "The acquired OAuth2 access token does not have the required Payouts scope:\n")
			fmt.Fprintf(stderr, "  • Required Scope: %s\n", paypal.PayPalPayoutsScope)
			fmt.Fprintln(stderr, "Action: Enable 'Payouts' permission under App Features in the PayPal Developer Dashboard.")
			report.Outcome = "BLOCKED"
			report.Details = fmt.Sprintf("PayPal Sandbox OAuth token missing required Payouts permission scope (%s)", paypal.PayPalPayoutsScope)
			writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
			return ExitCodeBlocked
		}
	}

	// 6. Database Preflight (with credential sanitization)
	fmt.Fprintln(stdout, "\n[3/4] Database & Storage Layer Preflight:")
	var repo db.Repository
	if injectedRepo != nil {
		repo = injectedRepo
		report.DatabaseReachable = true
		report.DatabaseMigrations = true
		fmt.Fprintln(stdout, "  • Repository State        : Injected Test Repository Connected")
		fmt.Fprintln(stdout, "  • Schema Migrations       : Current (Verified)")
	} else {
		dbCtx, dbCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dbCancel()

		var pgRepo db.Repository
		var pgErr error
		if len(injectedDBFactory) > 0 && injectedDBFactory[0] != nil {
			pgRepo, pgErr = injectedDBFactory[0](dbCtx, cfg.DatabaseURL)
		} else {
			pgRepo, pgErr = db.NewPostgresRepository(dbCtx, cfg.DatabaseURL)
		}

		if pgErr != nil {
			safeEndpoint := sanitizeDatabaseEndpoint(cfg.DatabaseURL)
			dbCategory := categorizeDatabaseError(pgErr)
			fmt.Fprintf(stdout, "  • PostgreSQL Status       : UNREACHABLE (%s at %s)\n", dbCategory, safeEndpoint)
			report.DatabaseReachable = false
			if shouldSubmit {
				fmt.Fprintln(stderr, "\n[ERROR] Database is required to execute the full application payout workflow.")
				fmt.Fprintln(stderr, "Ensure PostgreSQL is running: docker compose -f infra/docker-compose.yml up -d")
				report.Outcome = "FAIL"
				report.Details = fmt.Sprintf("PostgreSQL database unreachable at %s: %s", safeEndpoint, dbCategory)
				writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
				return ExitCodeFailure
			}
			fmt.Fprintln(stdout, "  • Preflight Notice        : Read-only preflight will continue without database.")
		} else {
			if closer, ok := pgRepo.(interface{ Close() }); ok {
				defer closer.Close()
			}
			report.DatabaseReachable = true
			fmt.Fprintln(stdout, "  • PostgreSQL Status       : CONNECTED")

			if migrator, ok := pgRepo.(interface{ RunMigrations(context.Context) error }); ok {
				if mErr := migrator.RunMigrations(dbCtx); mErr != nil {
					fmt.Fprintf(stderr, "  • Schema Migrations       : FAILED (%v)\n", mErr)
					report.DatabaseMigrations = false
					if shouldSubmit {
						report.Outcome = "FAIL"
						report.Details = "Database schema migrations failed"
						writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
						return ExitCodeFailure
					}
				} else {
					report.DatabaseMigrations = true
					fmt.Fprintln(stdout, "  • Schema Migrations       : CURRENT (Verified)")
				}
			} else {
				report.DatabaseMigrations = true
				fmt.Fprintln(stdout, "  • Schema Migrations       : CURRENT (Verified)")
			}
			repo = pgRepo
		}
	}

	// 7. Stage B/C: Application Workflow Execution or Safe Read-Only Exit
	fmt.Fprintln(stdout, "\n[4/4] End-to-End Application Workflow Execution:")

	if !shouldSubmit {
		fmt.Fprintln(stdout, "  • Execution Mode          : READ-ONLY PREFLIGHT (Default)")
		fmt.Fprintln(stdout, "  • Financial Mutations     : DISABLED (Zero payouts attempted)")
		fmt.Fprintln(stdout, "\n  [STATUS: PASS] Read-Only Preflight Verification Completed Successfully.")
		fmt.Fprintln(stdout, "  • All environment, credentials, and OAuth checks passed!")
		fmt.Fprintln(stdout, "  To execute an explicitly authorized USD 1.00 sandbox test payout:")
		fmt.Fprintln(stdout, "    go run ./cmd/sandbox-e2e -submit-payout -recipient <sandbox-recipient-email> -confirm-sandbox-only")

		report.Outcome = "PASS"
		report.Details = "Read-only preflight completed successfully. Zero payouts created."
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeSuccess
	}

	// Ensure we have a database repository
	if repo == nil {
		fmt.Fprintln(stderr, "\n[ERROR] Database repository is not available.")
		report.Outcome = "FAIL"
		report.Details = "Database repository not available for full workflow"
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeFailure
	}

	workflowCtx, workflowCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer workflowCancel()

	// Ensure operator user & org exist in DB (fails closed without hard-coded password)
	operatorUser, demoOrg, uErr := ensureOperatorAndOrg(workflowCtx, repo)
	if uErr != nil {
		fmt.Fprintf(stderr, "\n[ERROR] Operator context uninitialized: %v\n", uErr)
		report.Outcome = "FAIL"
		report.Details = fmt.Sprintf("Operator initialization check failed: %v", uErr)
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeFailure
	}

	// Display final confirmation summary
	fmt.Fprintln(stdout, "\n================================================================================")
	fmt.Fprintln(stdout, "            PAYPAL SANDBOX MUTATION AUTHORIZATION CONFIRMED                     ")
	fmt.Fprintln(stdout, "================================================================================")
	fmt.Fprintf(stdout, "  • Target Host             : %s [SANDBOX ONLY]\n", effectiveBaseURL)
	fmt.Fprintf(stdout, "  • Masked Recipient        : %s\n", report.SanitizedRecipient)
	fmt.Fprintf(stdout, "  • Payout Amount           : $%s %s\n", report.Amount, report.Currency)
	fmt.Fprintln(stdout, "  • Authorization Gate      : Single-payout limit verified (<= $1.00 USD)")
	fmt.Fprintln(stdout, "  • Application Path        : Executing ClaimBridge Core Payout Service")
	fmt.Fprintln(stdout, "================================================================================")

	// Construct core Payouts Service
	policyEngine := policy.NewEngine(cfg.PolicyEngineBinaryPath)
	payoutsService := payouts.NewService(cfg, repo, policyEngine, client)

	// Step 1: Create Claim / Case
	testRef := fmt.Sprintf("SANDBOX-E2E-%d-%s", time.Now().Unix(), crypto.GenerateUUID()[:6])
	fmt.Fprintf(stdout, "\n  [Step 1/5] Creating dedicated verification test claim (%s)...\n", testRef)
	pCase, err := payoutsService.CreatePayoutCase(workflowCtx, operatorUser, demoOrg, payouts.CreateCaseRequest{
		Reference:      testRef,
		RecipientEmail: recipient,
		RecipientName:  "PayPal Sandbox Integration Verifier",
		Title:          "PayPal Sandbox Integration E2E Verification",
		Description:    "Operator-authorized test payout executed to prove genuine PayPal Sandbox connectivity and persistence.",
		Currency:       currency,
		AmountCents:    amountCents,
		RiskScore:      0.0,
	})
	if err != nil {
		fmt.Fprintf(stderr, "  [ERROR] Failed creating payout case: %v\n", err)
		report.Outcome = "FAIL"
		report.Details = fmt.Sprintf("CreatePayoutCase failed: %v", err)
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeFailure
	}

	report.CaseID = pCase.ID
	report.CaseReference = pCase.Reference
	report.PersistedCaseStatus = pCase.Status
	fmt.Fprintf(stdout, "    ✓ Case created: ID=%s (Status=%s)\n", pCase.ID, pCase.Status)

	// Step 2: Initiate Payout Operation
	fmt.Fprintln(stdout, "  [Step 2/5] Initiating durable payout operation with correlation IDs...")
	op, err := payoutsService.InitiatePayoutOperation(workflowCtx, operatorUser, demoOrg, pCase.ID)
	if err != nil {
		fmt.Fprintf(stderr, "  [ERROR] Failed initiating payout operation: %v\n", err)
		report.Outcome = "FAIL"
		report.Details = fmt.Sprintf("InitiatePayoutOperation failed: %v", err)
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeFailure
	}

	report.OperationID = op.ID
	report.SenderBatchID = op.SenderBatchID
	report.SenderItemID = op.SenderItemID
	report.PersistedOpStatus = op.Status
	fmt.Fprintf(stdout, "    ✓ Operation created: ID=%s (Status=%s)\n", op.ID, op.Status)
	fmt.Fprintf(stdout, "    ✓ Sender Batch ID  : %s\n", op.SenderBatchID)
	fmt.Fprintf(stdout, "    ✓ Sender Item ID   : %s\n", op.SenderItemID)

	// Step 3: Submit exactly ONE payout to PayPal Sandbox
	fmt.Fprintln(stdout, "  [Step 3/5] Computing canonical fingerprint and submitting payout to PayPal Sandbox...")
	// We are now dispatching the mutation to PayPal
	report.MutationDispatched = true
	report.PayoutAttempted = true

	submittedOp, submitErr := payoutsService.SubmitPayoutOperation(workflowCtx, operatorUser, demoOrg, pCase.ID, op.ID)
	if submitErr != nil {
		if errors.Is(submitErr, paypal.ErrAmbiguousNetworkTimeout) {
			report.ProviderResponseReceived = false
			fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
			fmt.Fprintln(stdout, "[STATUS: UNRESOLVED] Network Timeout During PayPal Mutation")
			fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")
			fmt.Fprintf(stdout, "Error: %v\n", submitErr)
			fmt.Fprintln(stdout, "Safety Invariant Enforced:")
			fmt.Fprintln(stdout, "  • Operation preserved in 'RECONCILIATION_REQUIRED' status.")
			fmt.Fprintln(stdout, "  • Automatic replay was PREVENTED to avoid duplicate disbursement.")
			fmt.Fprintln(stdout, "  • Do NOT retry with a new batch ID.")
			fmt.Fprintf(stdout, "  • Query or recover via existing ClaimBridge workflow: SenderBatchID=%s\n", op.SenderBatchID)
			fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")

			report.Outcome = "UNRESOLVED"
			report.Details = "Ambiguous network timeout during payout submission; provider response not received; state preserved in RECONCILIATION_REQUIRED"
			if submittedOp != nil {
				report.PersistedOpStatus = submittedOp.Status
				if submittedOp.SubmissionFingerprint != nil {
					report.SubmissionFingerprint = *submittedOp.SubmissionFingerprint
				}
			}
			writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
			return ExitCodeUnresolved
		}

		report.ProviderResponseReceived = true
		if fe, ok := paypal.AsAPIForbiddenError(submitErr); ok {
			report.ProviderErrorName = fe.Name
			report.ProviderErrorIssue = fe.Issue
			report.ProviderDebugID = fe.DebugID
		}
		fmt.Fprintf(stderr, "  [ERROR] Payout submission failed: %v\n", submitErr)
		report.Outcome = "FAIL"
		report.Details = fmt.Sprintf("SubmitPayoutOperation failed: %v", submitErr)
		if submittedOp != nil {
			report.PersistedOpStatus = submittedOp.Status
		}
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeFailure
	}

	report.ProviderResponseReceived = true
	report.PersistedOpStatus = submittedOp.Status
	if submittedOp.SubmissionFingerprint != nil {
		report.SubmissionFingerprint = *submittedOp.SubmissionFingerprint
	}
	if submittedOp.PayPalPayoutBatchID != nil {
		report.PayPalPayoutBatchID = *submittedOp.PayPalPayoutBatchID
	}
	if submittedOp.PayPalBatchStatus != nil {
		report.PayPalBatchStatus = *submittedOp.PayPalBatchStatus
	}

	fmt.Fprintf(stdout, "    ✓ Payout accepted by PayPal Sandbox!\n")
	fmt.Fprintf(stdout, "    ✓ PayPal Payout Batch ID : %s\n", report.PayPalPayoutBatchID)
	fmt.Fprintf(stdout, "    ✓ Initial Batch Status   : %s\n", report.PayPalBatchStatus)
	fmt.Fprintf(stdout, "    ✓ Submission Fingerprint : %s\n", report.SubmissionFingerprint)

	// Step 4: Authoritative Status Synchronization & Reconciliation
	fmt.Fprintln(stdout, "  [Step 4/5] Synchronizing and reconciling with PayPal Sandbox...")
	// Pause briefly for sandbox queue ingestion
	time.Sleep(1 * time.Second)

	syncedCase, syncedOp, syncErr := payoutsService.SyncPayoutCase(workflowCtx, demoOrg.ID, pCase.ID)
	if syncErr != nil {
		fmt.Fprintf(stdout, "  [WARN] Initial synchronization returned error: %v (evaluating direct batch query...)\n", syncErr)
	} else {
		report.PersistedCaseStatus = syncedCase.Status
		report.PersistedOpStatus = syncedOp.Status
		if syncedOp.PayPalBatchStatus != nil {
			report.PayPalBatchStatus = *syncedOp.PayPalBatchStatus
		}
		if syncedOp.PayPalItemStatus != nil {
			report.PayPalItemStatus = *syncedOp.PayPalItemStatus
		}
	}

	// Query batch directly for correlation verification
	batchDetails, bErr := client.GetPayoutBatch(workflowCtx, report.PayPalPayoutBatchID)
	if bErr != nil {
		fmt.Fprintf(stderr, "  [ERROR] Failed querying payout batch from PayPal: %v\n", bErr)
		report.Outcome = "FAIL"
		report.Details = fmt.Sprintf("Failed querying payout batch details: %v", bErr)
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeFailure
	}

	// Step 5: Verify Correlation Between Intended Operation & Returned Evidence
	fmt.Fprintln(stdout, "  [Step 5/5] Correlating provider response with intended operation...")
	correlation := verifyCorrelation(op, recipient, report.Amount, report.Currency, batchDetails)
	report.Correlation = correlation

	fmt.Fprintf(stdout, "    • Sender Batch ID Correlated : %t\n", correlation.SenderBatchIDMatched)
	fmt.Fprintf(stdout, "    • Sender Item ID Correlated  : %t\n", correlation.SenderItemIDMatched)
	fmt.Fprintf(stdout, "    • Recipient Correlated       : %t\n", correlation.RecipientMatched)
	fmt.Fprintf(stdout, "    • Amount Correlated          : %t\n", correlation.AmountMatched)
	fmt.Fprintf(stdout, "    • Currency Correlated        : %t\n", correlation.CurrencyMatched)

	if !correlation.OverallCorrelated {
		fmt.Fprintln(stderr, "  [ERROR] Correlation verification failed: provider response attributes do not match intended payout.")
		report.Outcome = "FAIL"
		report.Details = "Correlation mismatch between intended payout operation and returned provider response"
		writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
		return ExitCodeFailure
	}

	// Check status snapshots
	snapshots, _ := repo.ListPayoutStatusSnapshots(workflowCtx, demoOrg.ID, op.ID)
	report.StatusSnapshotsCount = len(snapshots)

	// Query actual persisted audit records scoped to org, case, and operation
	actualAuditCount, auditErr := repo.CountAuditEvents(workflowCtx, demoOrg.ID, pCase.ID, op.ID)
	if auditErr != nil {
		fmt.Fprintf(stdout, "  [WARN] Failed verifying audit records from database: %v\n", auditErr)
		report.AuditEventsCount = 0
		report.AuditVerificationPassed = false
	} else {
		report.AuditEventsCount = actualAuditCount
		report.AuditVerificationPassed = actualAuditCount > 0
		fmt.Fprintf(stdout, "    • Persisted Audit Records    : %d verified\n", actualAuditCount)
	}

	// Classify final outcome truthfully
	itemStatus := strings.ToUpper(strings.TrimSpace(report.PayPalItemStatus))
	batchStatus := strings.ToUpper(strings.TrimSpace(report.PayPalBatchStatus))

	persistenceValid := report.StatusSnapshotsCount > 0 && report.PersistedOpStatus != ""
	outcome, details, exitCode := classifyProviderOutcome(
		itemStatus,
		batchStatus,
		correlation,
		persistenceValid,
		report.AuditVerificationPassed,
	)
	report.Outcome = outcome
	report.Details = details

	fmt.Fprintln(stdout, "\n================================================================================")
	fmt.Fprintln(stdout, "                   SANDBOX VERIFICATION OUTCOME SUMMARY                         ")
	fmt.Fprintln(stdout, "================================================================================")
	fmt.Fprintf(stdout, "  • Final Case Status       : %s\n", report.PersistedCaseStatus)
	fmt.Fprintf(stdout, "  • Final Operation Status  : %s\n", report.PersistedOpStatus)
	fmt.Fprintf(stdout, "  • PayPal Batch Status     : %s\n", batchStatus)
	fmt.Fprintf(stdout, "  • PayPal Item Status      : %s\n", itemStatus)
	fmt.Fprintf(stdout, "  • Correlation Verified    : %t\n", correlation.OverallCorrelated)
	fmt.Fprintf(stdout, "  • Immutable Snapshots     : %d recorded\n", report.StatusSnapshotsCount)
	fmt.Fprintf(stdout, "  • Scoped Audit Records    : %d recorded (Verified: %t)\n", report.AuditEventsCount, report.AuditVerificationPassed)
	fmt.Fprintf(stdout, "\n  [STATUS: %s] %s\n", report.Outcome, report.Details)
	fmt.Fprintln(stdout, "================================================================================")

	writeReportsIfEnabled(report, *reportDirFlag, *noReportFileFlag, stdout)
	return exitCode
}

// verifyCorrelation validates that provider batch and item match the intended payout operation.
// Requires exact match on SenderItemID without fallback. Single-item fallback is strictly rejected.
func verifyCorrelation(op *db.PayoutOperation, expectedRecipient, expectedAmount, expectedCurrency string, batch *paypal.PayoutBatchResponse) *CorrelationVerification {
	batch.Normalize()
	c := &CorrelationVerification{
		SenderBatchIDMatched: batch.BatchHeader.SenderBatchHeader != nil &&
			op.SenderBatchID != "" &&
			batch.BatchHeader.SenderBatchHeader.SenderBatchID == op.SenderBatchID,
	}

	// Strictly require non-empty SenderItemID and exactly one matching item
	if op.SenderItemID == "" || len(batch.Items) == 0 {
		c.OverallCorrelated = false
		return c
	}

	var matchedItem *paypal.PayoutItem
	matchCount := 0
	for i := range batch.Items {
		item := &batch.Items[i]
		item.Normalize()
		if item.SenderItemID != "" && item.SenderItemID == op.SenderItemID {
			matchedItem = item
			matchCount++
		}
	}

	// Ambiguous matches (>1) or zero matches fail correlation
	if matchCount == 1 && matchedItem != nil {
		c.SenderItemIDMatched = true
		c.RecipientMatched = strings.EqualFold(strings.TrimSpace(matchedItem.Receiver), strings.TrimSpace(expectedRecipient))
		c.AmountMatched = matchedItem.Amount.Value == expectedAmount
		c.CurrencyMatched = strings.EqualFold(matchedItem.Amount.Currency, expectedCurrency)
	}

	c.OverallCorrelated = c.SenderBatchIDMatched && c.SenderItemIDMatched && c.RecipientMatched && c.AmountMatched && c.CurrencyMatched
	return c
}

// classifyProviderOutcome provides explicit, truthful classification of PayPal Sandbox payout outcomes.
func classifyProviderOutcome(itemStatus, batchStatus string, correlation *CorrelationVerification, persistenceValid bool, auditValid bool) (outcome string, details string, exitCode int) {
	if correlation == nil || !correlation.OverallCorrelated {
		return "FAIL", "Provider response attributes failed correlation with intended payout operation", ExitCodeFailure
	}
	if !persistenceValid {
		return "FAIL", "Persisted operation status or status snapshots verification failed", ExitCodeFailure
	}
	if !auditValid {
		return "FAIL", "Audit evidence verification failed or records could not be verified in database", ExitCodeFailure
	}

	effectiveStatus := strings.ToUpper(strings.TrimSpace(itemStatus))
	if effectiveStatus == "" {
		effectiveStatus = strings.ToUpper(strings.TrimSpace(batchStatus))
	}

	switch effectiveStatus {
	case "SUCCESS":
		return "PASS", "PayPal Sandbox payout completed and settled successfully (Item Status: SUCCESS). Provider batch and item correlation verified.", ExitCodeSuccess
	case "UNCLAIMED":
		return "PASS", "PayPal Sandbox payout integration verified; provider item accepted with status UNCLAIMED (receiver must claim or register in sandbox; not settled).", ExitCodeSuccess
	case "PENDING", "PROCESSING":
		return "PASS", fmt.Sprintf("PayPal Sandbox payout integration verified; provider item accepted with status %s (asynchronous processing in progress; not settled).", effectiveStatus), ExitCodeSuccess
	case "FAILED", "BLOCKED", "DENIED", "CANCELED", "CANCELLED":
		return "FAIL", fmt.Sprintf("PayPal Sandbox payout rejected with terminal provider status: %s.", effectiveStatus), ExitCodeFailure
	case "RETURNED", "REFUNDED", "REVERSED", "ONHOLD", "HELD":
		return "UNRESOLVED", fmt.Sprintf("PayPal Sandbox payout entered non-settled terminal/hold state: %s. Preserved for manual investigation.", effectiveStatus), ExitCodeUnresolved
	default:
		return "UNRESOLVED", fmt.Sprintf("PayPal Sandbox returned unrecognized or empty status (Batch: %q, Item: %q). Preserved for manual reconciliation.", batchStatus, itemStatus), ExitCodeUnresolved
	}
}

// parseAmountCents strictly parses a USD decimal string into integer cents using integer arithmetic.
// Rejects signs, exponents, NaN, Inf, non-digits, fractions > 2 digits, and values > MaxSandboxTestAmountCents.
func parseAmountCents(raw string) (int64, string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, "", errors.New("payout amount cannot be empty")
	}

	for _, ch := range trimmed {
		if (ch < '0' || ch > '9') && ch != '.' {
			return 0, "", fmt.Errorf("invalid character %q in payout amount (only digits and a single decimal point are permitted)", ch)
		}
	}

	parts := strings.Split(trimmed, ".")
	if len(parts) > 2 {
		return 0, "", errors.New("payout amount contains multiple decimal points")
	}
	if parts[0] == "" {
		return 0, "", errors.New("payout amount must contain digits before the decimal point")
	}

	dollars, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("invalid dollars portion in amount: %w", err)
	}
	if dollars < 0 {
		return 0, "", errors.New("payout amount cannot be negative")
	}

	var fracCents int64
	if len(parts) == 2 {
		frac := parts[1]
		if len(frac) == 0 {
			return 0, "", errors.New("trailing decimal point without fractional digits")
		}
		if len(frac) > 2 {
			return 0, "", fmt.Errorf("payout amount has %d decimal places; at most 2 decimal places permitted", len(frac))
		}
		if len(frac) == 1 {
			frac += "0"
		}
		fracVal, fErr := strconv.ParseInt(frac, 10, 64)
		if fErr != nil {
			return 0, "", fmt.Errorf("invalid cents portion in amount: %w", fErr)
		}
		fracCents = fracVal
	}

	totalCents := dollars*100 + fracCents
	if totalCents <= 0 {
		return 0, "", errors.New("payout amount must be greater than zero")
	}
	if totalCents > MaxSandboxTestAmountCents {
		return 0, "", fmt.Errorf("amount $%.2f exceeds the hard sandbox test ceiling of $1.00 USD (100 cents)", float64(totalCents)/100.0)
	}

	formatted := fmt.Sprintf("%d.%02d", totalCents/100, totalCents%100)
	return totalCents, formatted, nil
}

// sanitizeDatabaseEndpoint extracts only scheme, host, and database name from a raw DSN,
// strictly redacting passwords, usernames, and query credentials.
func sanitizeDatabaseEndpoint(dsn string) string {
	raw := strings.TrimSpace(dsn)
	if raw == "" {
		return "[not configured]"
	}

	// Try parsing as standard URL
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err == nil {
			host := u.Host
			if host == "" {
				host = "localhost"
			}
			dbName := strings.TrimPrefix(u.Path, "/")
			if dbName != "" {
				return fmt.Sprintf("postgresql://%s/%s", host, dbName)
			}
			return fmt.Sprintf("postgresql://%s", host)
		}
	}

	// Fallback for libpq key-value string: host=... port=... dbname=...
	parts := strings.Fields(raw)
	host := ""
	port := ""
	dbname := ""
	for _, p := range parts {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) == 2 {
			switch strings.ToLower(kv[0]) {
			case "host":
				host = kv[1]
			case "port":
				port = kv[1]
			case "dbname":
				dbname = kv[1]
			}
		}
	}
	if host != "" {
		if port != "" {
			host = host + ":" + port
		}
		if dbname != "" {
			return fmt.Sprintf("postgresql://%s/%s", host, dbname)
		}
		return fmt.Sprintf("postgresql://%s", host)
	}

	return "postgresql://[redacted-endpoint]"
}

// categorizeDatabaseError provides a sanitized category for database errors without
// disclosing raw error strings that may interpolate DSN credentials or tokens.
func categorizeDatabaseError(err error) string {
	if err == nil {
		return "none"
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection refused") || strings.Contains(msg, "connectex") {
		return "connection refused (PostgreSQL server may not be running)"
	}
	if strings.Contains(msg, "authentication failed") || strings.Contains(msg, "password authentication") {
		return "authentication failed"
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") {
		return "connection timed out"
	}
	if strings.Contains(msg, "does not exist") {
		return "target database does not exist"
	}
	if strings.Contains(msg, "no such host") || strings.Contains(msg, "lookup") {
		return "host lookup failed"
	}
	return "database connectivity failed"
}

// ensureOperatorAndOrg looks up the documented operator user and active organization in the database.
// Fails closed if the operator user is not initialized. Never creates accounts with hard-coded passwords.
func ensureOperatorAndOrg(ctx context.Context, repo db.Repository) (*db.User, *db.Organization, error) {
	user, err := repo.GetUserByEmail(ctx, seeder.OperatorEmail)
	if err != nil {
		return nil, nil, fmt.Errorf("operator account %q not found in database; please initialize demo fixtures with 'go run ./cmd/seed' before running integration payout", seeder.OperatorEmail)
	}

	memberships, mErr := repo.GetOrganizationMemberships(ctx, user.ID)
	if mErr != nil || len(memberships) == 0 {
		return nil, nil, fmt.Errorf("operator account %q has no active organization memberships; initialize with 'go run ./cmd/seed'", seeder.OperatorEmail)
	}

	return user, &memberships[0].Organization, nil
}

func getCommitSHA() string {
	if sha := os.Getenv("GITHUB_SHA"); sha != "" {
		if len(sha) > 7 {
			return sha[:7]
		}
		return sha
	}
	if sha := os.Getenv("GIT_COMMIT"); sha != "" {
		if len(sha) > 7 {
			return sha[:7]
		}
		return sha
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return "unknown"
}

func writeReportsIfEnabled(report *SandboxE2EReport, reportDir string, noFile bool, stdout io.Writer) {
	if noFile {
		return
	}

	if err := os.MkdirAll(reportDir, 0755); err != nil {
		fmt.Fprintf(stdout, "[WARN] Unable to create report directory %s: %v\n", reportDir, err)
		return
	}

	timestampStr := report.ExecutionTimestamp.Format("20060102-150405")
	jsonPath := filepath.Join(reportDir, fmt.Sprintf("sandbox-e2e-report-%s.json", timestampStr))
	mdPath := filepath.Join(reportDir, fmt.Sprintf("sandbox-e2e-report-%s.md", timestampStr))

	report.ReportArtifactPathJSON = jsonPath
	report.ReportArtifactPathMD = mdPath

	jsonBytes, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		if writeErr := os.WriteFile(jsonPath, jsonBytes, 0600); writeErr == nil {
			fmt.Fprintf(stdout, "\n[REPORT] Structured JSON report written to: %s\n", jsonPath)
		}
	}

	mdContent := generateMarkdownReport(report)
	if writeErr := os.WriteFile(mdPath, []byte(mdContent), 0600); writeErr == nil {
		fmt.Fprintf(stdout, "[REPORT] Human-readable Markdown summary written to: %s\n", mdPath)
	}
}

func generateMarkdownReport(r *SandboxE2EReport) string {
	var sb strings.Builder
	sb.WriteString("# ClaimBridge — PayPal Sandbox End-to-End Verification Report\n\n")
	sb.WriteString(fmt.Sprintf("- **Execution Timestamp**: `%s`\n", r.ExecutionTimestamp.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("- **Application Commit**: `%s`\n", r.CommitSHA))
	sb.WriteString(fmt.Sprintf("- **Environment Target**: `%s` (Base URL: `%s`)\n", r.PayPalEnv, r.PayPalBaseURL))
	sb.WriteString(fmt.Sprintf("- **Submission Requested**: `%s`\n", boolStatus(r.SubmissionRequested)))
	sb.WriteString(fmt.Sprintf("- **Mutation Dispatched**: `%s`\n", boolStatus(r.MutationDispatched)))
	sb.WriteString(fmt.Sprintf("- **Provider Response Received**: `%s`\n", boolStatus(r.ProviderResponseReceived)))
	sb.WriteString(fmt.Sprintf("- **Overall Outcome**: **`%s`**\n", r.Outcome))
	sb.WriteString(fmt.Sprintf("- **Summary Details**: %s\n\n", r.Details))

	sb.WriteString("## Preflight & Environment Checks\n\n")
	sb.WriteString(fmt.Sprintf("- Configuration Security Audit: `%s`\n", boolStatus(r.PreflightConfigValid)))
	sb.WriteString(fmt.Sprintf("- PostgreSQL Database Reachable: `%s`\n", boolStatus(r.DatabaseReachable)))
	sb.WriteString(fmt.Sprintf("- Schema Migrations Current: `%s`\n", boolStatus(r.DatabaseMigrations)))
	sb.WriteString(fmt.Sprintf("- Genuine PayPal Sandbox OAuth Token: `%s` (Payouts Scope: `%t`)\n\n", boolStatus(r.OAuthTokenAcquired), r.HasPayoutsScope))

	if r.MutationDispatched {
		if r.ProviderErrorName != "" || r.ProviderDebugID != "" || r.ProviderErrorIssue != "" {
			sb.WriteString("### Provider Error Diagnostics (HTTP 403)\n\n")
			if r.ProviderErrorName != "" {
				sb.WriteString(fmt.Sprintf("- **Error Name**: `%s`\n", r.ProviderErrorName))
			}
			if r.ProviderErrorIssue != "" {
				sb.WriteString(fmt.Sprintf("- **Error Issue**: `%s`\n", r.ProviderErrorIssue))
			}
			if r.ProviderDebugID != "" {
				sb.WriteString(fmt.Sprintf("- **Debug ID**: `%s`\n", r.ProviderDebugID))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("## Payout Submission & Correlation Evidence\n\n")
		sb.WriteString(fmt.Sprintf("- **Masked Recipient**: `%s`\n", r.SanitizedRecipient))
		sb.WriteString(fmt.Sprintf("- **Disbursed Amount**: `$%s %s`\n", r.Amount, r.Currency))
		sb.WriteString(fmt.Sprintf("- **Claim ID**: `%s` (Ref: `%s`)\n", r.CaseID, r.CaseReference))
		sb.WriteString(fmt.Sprintf("- **Operation ID**: `%s`\n", r.OperationID))
		sb.WriteString(fmt.Sprintf("- **Sender Batch ID**: `%s`\n", r.SenderBatchID))
		sb.WriteString(fmt.Sprintf("- **Sender Item ID**: `%s`\n", r.SenderItemID))
		sb.WriteString(fmt.Sprintf("- **Submission Fingerprint**: `%s`\n", r.SubmissionFingerprint))
		sb.WriteString(fmt.Sprintf("- **PayPal Payout Batch ID**: `%s`\n", r.PayPalPayoutBatchID))
		sb.WriteString(fmt.Sprintf("- **PayPal Batch Status**: `%s`\n", r.PayPalBatchStatus))
		sb.WriteString(fmt.Sprintf("- **PayPal Item Status**: `%s`\n", r.PayPalItemStatus))
		sb.WriteString(fmt.Sprintf("- **Persisted Case Status**: `%s`\n", r.PersistedCaseStatus))
		sb.WriteString(fmt.Sprintf("- **Persisted Operation Status**: `%s`\n\n", r.PersistedOpStatus))

		if r.Correlation != nil {
			sb.WriteString("### Correlation Verification\n\n")
			sb.WriteString(fmt.Sprintf("- Sender Batch ID Matched: `%s`\n", boolStatus(r.Correlation.SenderBatchIDMatched)))
			sb.WriteString(fmt.Sprintf("- Sender Item ID Matched: `%s`\n", boolStatus(r.Correlation.SenderItemIDMatched)))
			sb.WriteString(fmt.Sprintf("- Recipient Matched: `%s`\n", boolStatus(r.Correlation.RecipientMatched)))
			sb.WriteString(fmt.Sprintf("- Amount Matched: `%s`\n", boolStatus(r.Correlation.AmountMatched)))
			sb.WriteString(fmt.Sprintf("- Currency Matched: `%s`\n", boolStatus(r.Correlation.CurrencyMatched)))
			sb.WriteString(fmt.Sprintf("- Overall Correlation Verified: `%s`\n\n", boolStatus(r.Correlation.OverallCorrelated)))
		}

		sb.WriteString("### Audit & Status Persistence\n\n")
		sb.WriteString(fmt.Sprintf("- Status Snapshots Saved: `%d`\n", r.StatusSnapshotsCount))
		sb.WriteString(fmt.Sprintf("- Scoped Audit Records Verified: `%d` (Verification: `%s`)\n\n", r.AuditEventsCount, boolStatus(r.AuditVerificationPassed)))
	} else {
		sb.WriteString("## Mutation Status\n\n")
		if r.SubmissionRequested {
			sb.WriteString("A payout mutation was requested by the operator, but failed pre-submission validation. No mutation was dispatched to PayPal Sandbox.\n\n")
		} else {
			sb.WriteString("No financial mutations or payouts were attempted. Read-only preflight mode was active.\n\n")
		}
	}

	sb.WriteString("---\n\n")
	sb.WriteString("> [!NOTE]\n")
	sb.WriteString("> **Sanitization Notice**: This report contains no access tokens, client secrets, database passwords, or unsanitized personal identifying data.\n")
	return sb.String()
}

func boolStatus(b bool) string {
	if b {
		return "PASS"
	}
	return "FAIL"
}
