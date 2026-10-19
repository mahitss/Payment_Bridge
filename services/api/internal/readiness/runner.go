package readiness

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"claimbridge/services/api/internal/ai"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/paypal"
)

// RunnerOptions configures the execution of the readiness diagnostic.
type RunnerOptions struct {
	Profile string // "core" or "submission" / "demo"
	EnvFile string
	BatchID string
	ItemID  string

	// Overrides for unit and integration testing
	Config       *config.Config
	BaseURL      string
	WebURL       string
	DBPingFn     func(context.Context) error
	PayPalClient PayPalReadOnlyClient
	AIProvider   ai.AIProvider
	HTTPClient   *http.Client
}

// Run executes the diagnostic suite according to the selected profile and options.
func Run(ctx context.Context, opts RunnerOptions, stdout, stderr io.Writer) (*Summary, error) {
	// 0. Validate mutually exclusive lookup flags
	if opts.BatchID != "" && opts.ItemID != "" {
		return nil, fmt.Errorf("ambiguous lookup invocation: both -batch-id (%s) and -item-id (%s) provided; specify only one", opts.BatchID, opts.ItemID)
	}

	// 1. Resolve Profile
	profileName := strings.ToLower(strings.TrimSpace(opts.Profile))
	if profileName == "" {
		profileName = string(ProfileCore)
	}

	var activeProfile Profile
	switch profileName {
	case string(ProfileSubmission), string(ProfileDemo):
		activeProfile = ProfileSubmission
	case string(ProfileCore):
		activeProfile = ProfileCore
	default:
		return nil, fmt.Errorf("invalid readiness profile %q (supported: 'core', 'submission')", opts.Profile)
	}

	// 2. Load Configuration
	cfg := opts.Config
	if cfg == nil {
		_, _ = config.LoadEnvFiles(opts.EnvFile)
		var err error
		cfg, err = config.LoadFromEnv()
		if err != nil {
			fmt.Fprintf(stderr, "[CRITICAL ERROR] Failed to load configuration: %v\n", err)
			return nil, err
		}
	}

	// 3. HTTP Client with bounded timeouts
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("http://localhost:%s", cfg.Port)
	}

	webURL := opts.WebURL
	if webURL == "" {
		webURL = "http://localhost:3000"
	}

	// 4. Render Diagnostic Header
	fmt.Fprintln(stdout, "================================================================================")
	fmt.Fprintln(stdout, " CLAIMBRIDGE — SYSTEM READINESS & EVALUATION DIAGNOSTIC")
	fmt.Fprintf(stdout, " Active Profile: %s\n", strings.ToUpper(string(activeProfile)))
	if activeProfile == ProfileCore {
		fmt.Fprintln(stdout, " Note: Core profile verifies local services. Use -profile=submission for live gates.")
	} else {
		fmt.Fprintln(stdout, " Gate Mandate: Core services + Genuine PayPal Sandbox + Genuine Model Inference.")
	}
	fmt.Fprintln(stdout, "================================================================================")

	// 5. Initialize PayPal Client for the diagnostic run if permitted and not injected
	var ppClient PayPalReadOnlyClient = opts.PayPalClient
	var ppInitErr error
	if ppClient == nil {
		if cfg.PayPalClientID != "" && cfg.PayPalClientSecret != "" {
			if cfg.PayPalEnv != "sandbox" {
				ppInitErr = paypal.ErrSandboxAssertionFailed
			} else if err := paypal.ValidateSandboxBaseURL(cfg.PayPalBaseURL); err != nil {
				ppInitErr = err
			} else {
				c, err := paypal.NewClient(cfg, nil)
				if err != nil {
					ppInitErr = err
				} else {
					ppClient = c
				}
			}
		}
	}

	// 6. Execute Probes
	var rawResults []CheckResult

	// Check 1: PostgreSQL
	rawResults = append(rawResults, ProbePostgres(ctx, cfg, opts.DBPingFn))

	// Check 2: Go API Gateway Health
	rawResults = append(rawResults, ProbeAPIHealth(ctx, httpClient, baseURL))

	// Check 3: Go API Readiness & Storage
	rawResults = append(rawResults, ProbeAPIReadiness(ctx, httpClient, baseURL))

	// Check 4: Policy Engine
	rawResults = append(rawResults, ProbePolicyEngine(ctx, httpClient, baseURL))

	// Check 5: Next.js Web Frontend
	rawResults = append(rawResults, ProbeWebFrontend(ctx, httpClient, webURL))

	// Check 6: PayPal Sandbox OAuth
	oauthRes := ProbePayPalOAuth(ctx, cfg, ppClient, ppInitErr)
	rawResults = append(rawResults, oauthRes)

	// Check 7: PayPal Read-Only Lookup
	rawResults = append(rawResults, ProbePayPalLookup(ctx, ppClient, oauthRes.Status, opts.BatchID, opts.ItemID))

	// Check 8: AI Reachability & Model Availability
	rawResults = append(rawResults, ProbeAIHealth(ctx, cfg, opts.AIProvider))

	// Check 9: Genuine AI Model Inference Probe
	isMockAI := (opts.AIProvider != nil && opts.AIProvider.Name() == "mock") ||
		(opts.AIProvider == nil && (strings.EqualFold(cfg.AIProvider, "mock") || cfg.AIProvider == ""))

	if activeProfile == ProfileCore && !cfg.AIEnabled {
		rawResults = append(rawResults, CheckResult{
			ID:       9,
			Name:     "Genuine AI Model Inference & Grounding Probe",
			Category: "AI Model Verification",
			Status:   StatusSkipped,
			Summary:  "Inference probe skipped in core profile (AI disabled)",
			Detail:   "Core profile does not require live model inference.",
		})
	} else if activeProfile == ProfileCore && isMockAI {
		rawResults = append(rawResults, CheckResult{
			ID:             9,
			Name:           "Genuine AI Model Inference & Grounding Probe",
			Category:       "AI Model Verification",
			Status:         StatusWarn,
			Summary:        "Mock AI provider configured (informational only in core profile)",
			Detail:         "Core profile permits mock AI for unit development. Live model inference probe is skipped.",
			OperatorAction: "Configure AI_PROVIDER=ollama and AI_MODEL=llama3.2 for genuine model inference",
		})
	} else {
		rawResults = append(rawResults, ProbeAIInference(ctx, cfg, opts.AIProvider))
	}

	// 6. Apply Profile Requirements
	summary := &Summary{
		Profile:     activeProfile,
		TotalChecks: len(rawResults),
		Results:     make([]CheckResult, 0, len(rawResults)),
	}

	for _, res := range rawResults {
		switch activeProfile {
		case ProfileCore:
			// Core profile requires checks 1 to 5 (Local Infrastructure)
			res.Required = (res.ID >= 1 && res.ID <= 5)
		case ProfileSubmission:
			// Submission profile requires checks 1..6, 8, 9.
			// Check 7 (lookup) is optional unless requested.
			if res.ID == 7 {
				res.Required = (opts.BatchID != "" || opts.ItemID != "")
			} else {
				res.Required = true
			}
		}

		// Count statuses
		switch res.Status {
		case StatusPass:
			summary.PassedCount++
			if res.Required {
				summary.RequiredPass++
			}
		case StatusFail:
			summary.FailedCount++
		case StatusBlocked:
			summary.BlockedCount++
		case StatusWarn:
			summary.WarnCount++
		case StatusSkipped:
			summary.SkippedCount++
		}

		if res.Required {
			summary.RequiredTotal++
		}

		summary.Results = append(summary.Results, res)

		// Print check line
		reqTag := "[REQUIRED]"
		if !res.Required {
			reqTag = "[OPTIONAL]"
		}

		fmt.Fprintf(stdout, "\n[%d/%d] %s %s %s\n", res.ID, len(rawResults), reqTag, res.Name, statusBadge(res.Status))
		fmt.Fprintf(stdout, "      Summary : %s\n", res.Summary)
		if res.Detail != "" {
			fmt.Fprintf(stdout, "      Detail  : %s\n", res.Detail)
		}
		if res.OperatorAction != "" && res.Status != StatusPass {
			fmt.Fprintf(stdout, "      Action  : %s\n", res.OperatorAction)
		}
	}

	// 7. Calculate Readiness & Exit Code
	allRequiredPassed := (summary.RequiredPass == summary.RequiredTotal)
	summary.IsReady = allRequiredPassed

	if summary.IsReady {
		summary.ExitCode = 0
	} else {
		summary.ExitCode = 1
	}

	// 8. Render Summary Table
	fmt.Fprintln(stdout, "\n--------------------------------------------------------------------------------")
	fmt.Fprintf(stdout, " DIAGNOSTIC SUMMARY (%s PROFILE):\n", strings.ToUpper(string(activeProfile)))
	fmt.Fprintf(stdout, "   Total Checks Evaluated : %d\n", summary.TotalChecks)
	fmt.Fprintf(stdout, "   Required Checks Passed : %d / %d\n", summary.RequiredPass, summary.RequiredTotal)
	fmt.Fprintf(stdout, "   Passed   : %d\n", summary.PassedCount)
	fmt.Fprintf(stdout, "   Failed   : %d\n", summary.FailedCount)
	fmt.Fprintf(stdout, "   Blocked  : %d\n", summary.BlockedCount)
	fmt.Fprintf(stdout, "   Warnings : %d\n", summary.WarnCount)
	fmt.Fprintf(stdout, "   Skipped  : %d\n", summary.SkippedCount)
	fmt.Fprintln(stdout, "--------------------------------------------------------------------------------")

	// 9. Render Final Status Banner
	if activeProfile == ProfileCore {
		if summary.IsReady {
			fmt.Fprintln(stdout, "\n================================================================================")
			fmt.Fprintln(stdout, " [STATUS: READY] CORE DEVELOPMENT READINESS PASSED")
			fmt.Fprintln(stdout, " Note: Core profile readiness is NOT proof of complete integration readiness.")
			fmt.Fprintln(stdout, " External PayPal sandbox and local AI model integrations remain unverified")
			fmt.Fprintln(stdout, " or blocked. To test submission gates, run:")
			fmt.Fprintln(stdout, "   go run ./cmd/readiness -profile=submission")
			fmt.Fprintln(stdout, "================================================================================")
		} else {
			fmt.Fprintln(stdout, "\n================================================================================")
			fmt.Fprintln(stdout, " [STATUS: NOT READY] CORE LOCAL SERVICES FAILED OR OFFLINE")
			fmt.Fprintln(stdout, " One or more required core services are offline. See actions above.")
			fmt.Fprintln(stdout, "================================================================================")
		}
	} else {
		if summary.IsReady {
			fmt.Fprintln(stdout, "\n================================================================================")
			fmt.Fprintln(stdout, " [STATUS: READY] FULL SUBMISSION & DEMO READINESS VERIFIED")
			fmt.Fprintln(stdout, " All core services, genuine PayPal Sandbox OAuth, and genuine local model")
			fmt.Fprintln(stdout, " inference are 100% verified. Ready for video recording & evaluation.")
			fmt.Fprintln(stdout, "================================================================================")
		} else {
			fmt.Fprintln(stdout, "\n================================================================================")
			fmt.Fprintln(stdout, " [STATUS: BLOCKED] SUBMISSION READINESS GATES UNFULFILLED")
			fmt.Fprintln(stdout, " One or more required submission gates failed or remain unverified.")
			fmt.Fprintln(stdout, " Mock providers and synthetic fixtures cannot satisfy submission profile.")
			fmt.Fprintln(stdout, " Review required actions above to unblock external dependencies.")
			fmt.Fprintln(stdout, "================================================================================")
		}
	}

	return summary, nil
}

func statusBadge(s CheckStatus) string {
	switch s {
	case StatusPass:
		return "[PASS]"
	case StatusFail:
		return "[FAIL]"
	case StatusBlocked:
		return "[BLOCKED]"
	case StatusWarn:
		return "[WARN]"
	case StatusSkipped:
		return "[SKIPPED]"
	default:
		return string(s)
	}
}
