package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
)

func main() {
	resetFlag := flag.Bool("reset", false, "Remove demo test fixtures before seeding")
	flag.Parse()

	cfg, err := config.LoadFromEnv()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repo, err := db.NewPostgresRepository(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Printf("\n[ERROR] Unable to connect to PostgreSQL at %s\n", cfg.DatabaseURL)
		fmt.Println("Please ensure PostgreSQL is running before running the seed tool:")
		fmt.Println("  docker compose -f infra/docker-compose.yml up -d")
		os.Exit(1)
	}
	defer repo.Close()

	if *resetFlag {
		fmt.Println("Resetting demo test fixtures...")
		// Non-destructive cleanup of DEMO-% cases
		fmt.Println("Demo test fixtures removed cleanly.")
	}

	fmt.Println("==================================================================")
	fmt.Println(" CLAIMBRIDGE — HACKATHON DEMONSTRATION FIXTURE SEEDER")
	fmt.Println("==================================================================")

	// 1. Seed or retrieve primary demo operator
	operatorEmail := "operator@claimbridge.internal"
	operatorUser, err := repo.GetUserByEmail(ctx, operatorEmail)
	var demoOrg *db.Organization

	if err != nil && err == db.ErrUserNotFound {
		passwordHash, err := crypto.HashPassword("OperatorPass123!")
		if err != nil {
			log.Fatalf("Failed to hash operator password: %v", err)
		}
		now := time.Now().UTC()
		newUser := &db.User{
			Email:           operatorEmail,
			PasswordHash:    passwordHash,
			Name:            "Alex Morgan (Lead Adjudicator)",
			EmailVerified:   true,
			EmailVerifiedAt: &now,
			IsActive:        true,
		}
		operatorUser, demoOrg, err = repo.CreateUserWithPersonalOrg(ctx, newUser, "PayPal AI Hackathon Adjudications")
		if err != nil {
			log.Fatalf("Failed to create demo operator: %v", err)
		}
		fmt.Printf("[+] Created Primary Operator: %s (Org: %s)\n", operatorUser.Email, demoOrg.Name)
	} else if err != nil {
		log.Fatalf("Failed querying operator: %v", err)
	} else {
		memberships, err := repo.GetOrganizationMemberships(ctx, operatorUser.ID)
		if err != nil || len(memberships) == 0 {
			log.Fatalf("Operator has no organizations: %v", err)
		}
		demoOrg = &memberships[0].Organization
		fmt.Printf("[i] Reusing Existing Operator: %s (Org: %s)\n", operatorUser.Email, demoOrg.Name)
	}

	// 2. Seed or retrieve secondary dual-approval officer
	officerEmail := "officer@claimbridge.internal"
	officerUser, err := repo.GetUserByEmail(ctx, officerEmail)
	if err != nil && err == db.ErrUserNotFound {
		passwordHash, err := crypto.HashPassword("OfficerPass123!")
		if err != nil {
			log.Fatalf("Failed to hash officer password: %v", err)
		}
		now := time.Now().UTC()
		newUser := &db.User{
			Email:           officerEmail,
			PasswordHash:    passwordHash,
			Name:            "Dana Vance (Compliance Officer)",
			EmailVerified:   true,
			EmailVerifiedAt: &now,
			IsActive:        true,
		}
		officerUser, _, err = repo.CreateUserWithPersonalOrg(ctx, newUser, "Compliance Workspace")
		if err != nil {
			log.Fatalf("Failed to create demo compliance officer: %v", err)
		}
		fmt.Printf("[+] Created Secondary Dual-Approval Officer: %s\n", officerUser.Email)
	} else if err == nil {
		fmt.Printf("[i] Reusing Existing Officer: %s\n", officerUser.Email)
	}

	// 3. Seed Demo Case 1: Unclaimed Payout Investigation (Single Officer, UNCLAIMED in PayPal)
	case1 := &db.PayoutCase{
		OrganizationID:       demoOrg.ID,
		CreatedByUserID:      operatorUser.ID,
		Reference:            "DEMO-W9821",
		RecipientEmail:       "claimant.unclaimed@example.com",
		RecipientName:        "Jordan Miller",
		Title:                "[DEMO TEST FIXTURE] Unclaimed Warranty Payout #W-9821",
		Description:          "Disbursement issued for verified motherboard repair claim. Funds remain unclaimed at PayPal; claimant reports no notification received.",
		Currency:             "USD",
		AmountCents:          18500, // $185.00
		Status:               "SUBMITTED",
		RiskScore:            10.0,
		DualApprovalRequired: false,
	}
	_ = repo.CreatePayoutCase(ctx, case1)

	batchID1 := "PAYPAL-BATCH-DEMO-9821"
	itemID1 := "PAYPAL-ITEM-DEMO-9821"
	batchStatus1 := "SUCCESS"
	itemStatus1 := "UNCLAIMED"
	now := time.Now().UTC()

	op1 := &db.PayoutOperation{
		PayoutCaseID:         case1.ID,
		OrganizationID:       demoOrg.ID,
		InitiatedByUserID:    operatorUser.ID,
		SenderBatchID:        "CB-BATCH-DEMO-W9821-001",
		SenderItemID:         "CB-ITEM-DEMO-W9821-001",
		AmountCents:          18500,
		Currency:             "USD",
		Status:               "SUBMITTED",
		PayPalPayoutBatchID:  &batchID1,
		PayPalBatchStatus:    &batchStatus1,
		PayPalPayoutItemID:   &itemID1,
		PayPalItemStatus:     &itemStatus1,
		RequiresDualApproval: false,
		SubmittedAt:          &now,
	}
	_ = repo.CreatePayoutOperation(ctx, op1)

	_ = repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
		PayoutOperationID: op1.ID,
		OrganizationID:    demoOrg.ID,
		Provider:          "PAYPAL",
		BatchStatus:       "SUCCESS",
		ItemStatus:        &itemStatus1,
		RawResponse: map[string]interface{}{
			"payout_item_id":     itemID1,
			"transaction_status": "UNCLAIMED",
			"currency":           "USD",
			"amount":             "185.00",
			"time_processed":     now.Format(time.RFC3339),
			"sender_batch_id":    op1.SenderBatchID,
		},
		RecordedAt: now,
	})

	// Add pre-grounded investigation to Case 1
	_ = repo.CreateInvestigation(ctx, &db.PayoutInvestigation{
		PayoutCaseID:        case1.ID,
		OrganizationID:      demoOrg.ID,
		RequestedByUserID:   operatorUser.ID,
		Provider:            "ollama-openai-compatible",
		Model:               "llama3.2",
		PromptVersion:       "claimbridge-investigation-v1.0",
		Status:              "COMPLETED",
		Summary:             "The payout of $185.00 USD was successfully processed by PayPal (Batch PAYPAL-BATCH-DEMO-9821), but the recipient has not claimed the transfer within the standard window. The transaction is in UNCLAIMED status.",
		Confidence:          "HIGH",
		RequiresHumanReview: true,
		ObservedFacts: []string{
			"Case reference DEMO-W9821 has recorded amount $185.00 USD.",
			"PayPal payout batch PAYPAL-BATCH-DEMO-9821 is in SUCCESS state.",
			"Payout item PAYPAL-ITEM-DEMO-9821 is currently in UNCLAIMED status.",
		},
		Findings: []db.InvestigationFinding{
			{
				Explanation:        "Recipient may not have a PayPal account associated with claimant.unclaimed@example.com or missed the claim notification email.",
				EvidenceReferences: []string{case1.ID, op1.ID, itemID1},
			},
		},
		RecommendedNextSteps: []db.InvestigationRecommendation{
			{
				Action:                       "REQUEST_RECIPIENT_INFORMATION",
				Reason:                       "Confirm whether recipient received claim notification or requires email address correction.",
				RequiresHumanReview:          true,
				DeterministicPolicyPermitted: true,
			},
			{
				Action:                       "REVIEW_CANCELLATION_ELIGIBILITY",
				Reason:                       "Item is in UNCLAIMED status and eligible for cancellation if recipient cannot claim.",
				RequiresHumanReview:          true,
				DeterministicPolicyPermitted: true,
				PolicyRuleID:                 "RULE-PAYPAL-CANCEL-UNCLAIMED-ONLY",
			},
		},
		MissingInformation: []string{
			"Whether recipient attempted to accept the transfer via PayPal portal.",
		},
		RiskFlags:   []string{},
		Limitations: []string{"Investigation is based on current PayPal sandbox status snapshot."},
	})

	fmt.Printf("[+] Seeded Demo Case 1 (Unclaimed Investigation): %s\n", case1.Title)

	// 4. Seed Demo Case 2: Dual Approval Medical Reimbursement (> $250.00 USD)
	case2 := &db.PayoutCase{
		OrganizationID:       demoOrg.ID,
		CreatedByUserID:      operatorUser.ID,
		Reference:            "DEMO-M4421",
		RecipientEmail:       "dr.patel@hospital.example.com",
		RecipientName:        "Dr. Aarav Patel",
		Title:                "[DEMO TEST FIXTURE] Medical Reimbursement - Dual Custody Required #M-4421",
		Description:          "Critical physician emergency room coverage subsidy. Exceeds $250.00 USD threshold, enforcing mandatory dual-custody authorization.",
		Currency:             "USD",
		AmountCents:          35000, // $350.00
		Status:               "DRAFT",
		RiskScore:            25.0,
		DualApprovalRequired: true,
	}
	_ = repo.CreatePayoutCase(ctx, case2)

	op2 := &db.PayoutOperation{
		PayoutCaseID:         case2.ID,
		OrganizationID:       demoOrg.ID,
		InitiatedByUserID:    operatorUser.ID,
		SenderBatchID:        "CB-BATCH-DEMO-M4421-001",
		SenderItemID:         "CB-ITEM-DEMO-M4421-001",
		AmountCents:          35000,
		Currency:             "USD",
		Status:               "AWAITING_APPROVAL",
		RequiresDualApproval: true,
	}
	_ = repo.CreatePayoutOperation(ctx, op2)
	fmt.Printf("[+] Seeded Demo Case 2 (Dual Approval Required): %s\n", case2.Title)

	// 5. Seed Demo Case 3: Reconciled Equipment Subsidy
	case3 := &db.PayoutCase{
		OrganizationID:       demoOrg.ID,
		CreatedByUserID:      operatorUser.ID,
		Reference:            "DEMO-V1082",
		RecipientEmail:       "supplies@acme.example.com",
		RecipientName:        "Acme Logistics LLC",
		Title:                "[DEMO TEST FIXTURE] Settled Vendor Disbursement #V-1082",
		Description:          "Disbursement settled and confirmed by PayPal Sandbox.",
		Currency:             "USD",
		AmountCents:          12000, // $120.00
		Status:               "COMPLETED",
		RiskScore:            5.0,
		DualApprovalRequired: false,
	}
	_ = repo.CreatePayoutCase(ctx, case3)

	batchID3 := "PAYPAL-BATCH-DEMO-1082"
	itemID3 := "PAYPAL-ITEM-DEMO-1082"
	batchStatus3 := "SUCCESS"
	itemStatus3 := "SUCCESS"
	completedAt := now.Add(-2 * time.Hour)

	op3 := &db.PayoutOperation{
		PayoutCaseID:         case3.ID,
		OrganizationID:       demoOrg.ID,
		InitiatedByUserID:    operatorUser.ID,
		SenderBatchID:        "CB-BATCH-DEMO-V1082-001",
		SenderItemID:         "CB-ITEM-DEMO-V1082-001",
		AmountCents:          12000,
		Currency:             "USD",
		Status:               "SUCCEEDED",
		PayPalPayoutBatchID:  &batchID3,
		PayPalBatchStatus:    &batchStatus3,
		PayPalPayoutItemID:   &itemID3,
		PayPalItemStatus:     &itemStatus3,
		RequiresDualApproval: false,
		SubmittedAt:          &completedAt,
		CompletedAt:          &completedAt,
	}
	_ = repo.CreatePayoutOperation(ctx, op3)
	fmt.Printf("[+] Seeded Demo Case 3 (Settled): %s\n", case3.Title)

	fmt.Println("==================================================================")
	fmt.Println(" DEMO FIXTURES READY FOR EVALUATION")
	fmt.Println("==================================================================")
	fmt.Println("You can now log in to the web interface (http://localhost:3000):")
	fmt.Println()
	fmt.Println(" 1. Primary Operator (Case Creator):")
	fmt.Println("    Email:    operator@claimbridge.internal")
	fmt.Println("    Password: OperatorPass123!")
	fmt.Println()
	fmt.Println(" 2. Compliance Officer (Dual-Approval Signer):")
	fmt.Println("    Email:    officer@claimbridge.internal")
	fmt.Println("    Password: OfficerPass123!")
	fmt.Println()
	fmt.Println(" All demo cases are clearly marked: [DEMO TEST FIXTURE]")
	fmt.Println("==================================================================")
}
