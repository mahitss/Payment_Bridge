package seeder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
)

const (
	OperatorEmail = "operator@claimbridge.internal"
	OperatorPass  = "OperatorPass123!"
	OperatorName  = "Alex Morgan (Lead Adjudicator)"
	DemoOrgName   = "PayPal AI Hackathon Adjudications"

	OfficerEmail = "officer@claimbridge.internal"
	OfficerPass  = "OfficerPass123!"
	OfficerName  = "Dana Vance (Compliance Officer)"
	OfficerOrg   = "Compliance Workspace"

	DemoCase1Ref = "DEMO-W9821"
	DemoCase2Ref = "DEMO-M4421"
	DemoCase3Ref = "DEMO-V1082"
)

type Options struct {
	Reset bool
}

// Seeder encapsulates idempotent seeding and clean fixture resetting.
type Seeder struct {
	repo db.Repository
}

// New constructs a new Seeder with explicit repository dependencies.
func New(repo db.Repository) *Seeder {
	return &Seeder{repo: repo}
}

// Seed executes the fixture seeding workflow.
func (s *Seeder) Seed(ctx context.Context, opts Options) error {
	if opts.Reset {
		deleted, err := s.repo.DeleteSyntheticFixtures(ctx, "")
		if err != nil {
			return fmt.Errorf("failed to reset synthetic demo fixtures: %w", err)
		}
		fmt.Printf("[i] Cleanly removed %d existing synthetic demo fixture cases and child records.\n", deleted)
	}

	// 1. Seed or retrieve primary demo operator
	var demoOrg *db.Organization
	operatorUser, err := s.repo.GetUserByEmail(ctx, OperatorEmail)
	if err != nil && errors.Is(err, db.ErrUserNotFound) {
		passwordHash, err := crypto.HashPassword(OperatorPass)
		if err != nil {
			return fmt.Errorf("failed to hash operator password: %w", err)
		}
		now := time.Now().UTC()
		newUser := &db.User{
			Email:           OperatorEmail,
			PasswordHash:    passwordHash,
			Name:            OperatorName,
			EmailVerified:   true,
			EmailVerifiedAt: &now,
			IsActive:        true,
		}
		operatorUser, demoOrg, err = s.repo.CreateUserWithPersonalOrg(ctx, newUser, DemoOrgName)
		if err != nil {
			return fmt.Errorf("failed to create demo operator: %w", err)
		}
		fmt.Printf("[+] Created Primary Operator: %s (Org: %s)\n", operatorUser.Email, demoOrg.Name)
	} else if err != nil {
		return fmt.Errorf("failed querying operator: %w", err)
	} else {
		if !operatorUser.IsActive {
			return errors.New("operator account exists but is disabled")
		}
		if !operatorUser.EmailVerified {
			if err := s.repo.MarkEmailVerified(ctx, operatorUser.ID); err != nil {
				return fmt.Errorf("failed verifying operator email: %w", err)
			}
		}
		memberships, err := s.repo.GetOrganizationMemberships(ctx, operatorUser.ID)
		if err != nil || len(memberships) == 0 {
			return fmt.Errorf("operator has no organization memberships: %v", err)
		}
		demoOrg = &memberships[0].Organization
		fmt.Printf("[i] Reusing Existing Operator: %s (Org: %s)\n", operatorUser.Email, demoOrg.Name)
	}

	// 2. Seed or retrieve secondary dual-approval officer
	officerUser, err := s.repo.GetUserByEmail(ctx, OfficerEmail)
	if err != nil && errors.Is(err, db.ErrUserNotFound) {
		passwordHash, err := crypto.HashPassword(OfficerPass)
		if err != nil {
			return fmt.Errorf("failed to hash officer password: %w", err)
		}
		now := time.Now().UTC()
		newUser := &db.User{
			Email:           OfficerEmail,
			PasswordHash:    passwordHash,
			Name:            OfficerName,
			EmailVerified:   true,
			EmailVerifiedAt: &now,
			IsActive:        true,
		}
		officerUser, _, err = s.repo.CreateUserWithPersonalOrg(ctx, newUser, OfficerOrg)
		if err != nil {
			return fmt.Errorf("failed to create demo compliance officer: %w", err)
		}
		fmt.Printf("[+] Created Secondary Dual-Approval Officer: %s\n", officerUser.Email)
	} else if err != nil {
		return fmt.Errorf("failed querying officer: %w", err)
	} else {
		if !officerUser.IsActive {
			return errors.New("compliance officer account exists but is disabled")
		}
		if !officerUser.EmailVerified {
			if err := s.repo.MarkEmailVerified(ctx, officerUser.ID); err != nil {
				return fmt.Errorf("failed verifying officer email: %w", err)
			}
		}
		fmt.Printf("[i] Reusing Existing Officer: %s\n", officerUser.Email)
	}

	// Ensure officer has membership in the primary demo organization for dual approval
	if err := s.repo.AddOrganizationMember(ctx, demoOrg.ID, officerUser.ID, "ADMIN"); err != nil {
		return fmt.Errorf("failed granting compliance officer membership in demo org: %w", err)
	}
	hasAccess, _, err := s.repo.VerifyUserOrgAccess(ctx, officerUser.ID, demoOrg.ID)
	if err != nil || !hasAccess {
		return fmt.Errorf("failed verifying officer access in demo org: %v", err)
	}

	now := time.Now().UTC()

	// 3. Seed Demo Case 1: Unclaimed Payout Investigation (Single Officer, simulated UNCLAIMED)
	case1, err := s.repo.GetPayoutCaseByReference(ctx, demoOrg.ID, DemoCase1Ref)
	if err != nil && errors.Is(err, db.ErrPayoutCaseNotFound) {
		case1 = &db.PayoutCase{
			OrganizationID:       demoOrg.ID,
			CreatedByUserID:      operatorUser.ID,
			Reference:            DemoCase1Ref,
			RecipientEmail:       "claimant.unclaimed@example.com",
			RecipientName:        "Jordan Miller",
			Title:                "[DEMO TEST FIXTURE] Unclaimed Warranty Payout #W-9821",
			Description:          "Disbursement issued for verified motherboard repair claim. Simulated funds remain unclaimed in sandbox; claimant reports no notification received.",
			Currency:             "USD",
			AmountCents:          18500, // $185.00
			Status:               "SUBMITTED",
			RiskScore:            10.0,
			DualApprovalRequired: false,
			IsSyntheticFixture:   true,
		}
		if err := s.repo.CreatePayoutCase(ctx, case1); err != nil {
			return fmt.Errorf("failed to create demo case 1: %w", err)
		}

		batchID1 := "SIMULATED-DEMO-BATCH-W9821"
		itemID1 := "SIMULATED-DEMO-ITEM-W9821"
		batchStatus1 := "SUCCESS"
		itemStatus1 := "UNCLAIMED"

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
			IsSyntheticFixture:   true,
		}
		if err := s.repo.CreatePayoutOperation(ctx, op1); err != nil {
			return fmt.Errorf("failed to create demo operation 1: %w", err)
		}

		snap1 := &db.PayoutStatusSnapshot{
			PayoutOperationID: op1.ID,
			OrganizationID:    demoOrg.ID,
			Provider:          "SIMULATED_DEMO",
			BatchStatus:       "SUCCESS",
			ItemStatus:        &itemStatus1,
			RawResponse: map[string]interface{}{
				"payout_item_id":     itemID1,
				"transaction_status": "UNCLAIMED",
				"currency":           "USD",
				"amount":             "185.00",
				"time_processed":     now.Format(time.RFC3339),
				"sender_batch_id":    op1.SenderBatchID,
				"provenance":         "SIMULATED_DEMO_DATA",
				"note":               "Synthetic status snapshot; not a live PayPal API response.",
			},
			IsSyntheticFixture: true,
			RecordedAt:         now,
		}
		if err := s.repo.CreatePayoutStatusSnapshot(ctx, snap1); err != nil {
			return fmt.Errorf("failed to create demo status snapshot 1: %w", err)
		}

		inv1 := &db.PayoutInvestigation{
			PayoutCaseID:        case1.ID,
			OrganizationID:      demoOrg.ID,
			RequestedByUserID:   operatorUser.ID,
			Provider:            "ollama-openai-compatible",
			Model:               "llama3.2",
			PromptVersion:       "claimbridge-investigation-v1.0",
			Status:              "COMPLETED",
			Summary:             "[SIMULATED DEMO DATA] Payout of $185.00 USD was simulated as processed by PayPal Sandbox test double (Batch SIMULATED-DEMO-BATCH-W9821). In this synthetic scenario, the recipient has not claimed the transfer within the observation window. The transaction is recorded in simulated UNCLAIMED status.",
			Confidence:          "HIGH",
			RequiresHumanReview: true,
			ObservedFacts: []string{
				"[SIMULATED] Case reference DEMO-W9821 has recorded amount $185.00 USD.",
				"[SIMULATED] Synthetic payout batch SIMULATED-DEMO-BATCH-W9821 is recorded in SUCCESS state.",
				"[SIMULATED] Synthetic payout item SIMULATED-DEMO-ITEM-W9821 is recorded in UNCLAIMED status.",
			},
			Findings: []db.InvestigationFinding{
				{
					Explanation:        "Simulated scenario: recipient has not claimed the transfer within the observation window.",
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
					Reason:                       "Item is in simulated UNCLAIMED status and eligible for cancellation if recipient cannot claim.",
					RequiresHumanReview:          true,
					DeterministicPolicyPermitted: true,
					PolicyRuleID:                 "RULE-PAYPAL-CANCEL-UNCLAIMED-ONLY",
				},
			},
			MissingInformation: []string{
				"Whether recipient attempted to accept the transfer via PayPal portal in live operations.",
			},
			RiskFlags: []string{},
			Limitations: []string{
				"Investigation is based strictly on simulated demo fixture data, NOT a live PayPal API response.",
			},
		}
		if err := s.repo.CreateInvestigation(ctx, inv1); err != nil {
			return fmt.Errorf("failed to create demo investigation 1: %w", err)
		}
		fmt.Printf("[+] Seeded Demo Case 1 (Unclaimed Investigation): %s\n", case1.Title)
	} else if err != nil {
		return fmt.Errorf("failed querying demo case 1: %w", err)
	} else {
		fmt.Printf("[i] Reusing Existing Demo Case 1: %s\n", case1.Title)
	}

	// 4. Seed Demo Case 2: Dual Approval Medical Reimbursement (> $250.00 USD)
	case2, err := s.repo.GetPayoutCaseByReference(ctx, demoOrg.ID, DemoCase2Ref)
	if err != nil && errors.Is(err, db.ErrPayoutCaseNotFound) {
		case2 = &db.PayoutCase{
			OrganizationID:       demoOrg.ID,
			CreatedByUserID:      operatorUser.ID,
			Reference:            DemoCase2Ref,
			RecipientEmail:       "dr.patel@hospital.example.com",
			RecipientName:        "Dr. Aarav Patel",
			Title:                "[DEMO TEST FIXTURE] Medical Reimbursement - Dual Custody Required #M-4421",
			Description:          "Critical physician emergency room coverage subsidy. Exceeds $250.00 USD threshold, enforcing mandatory dual-custody authorization.",
			Currency:             "USD",
			AmountCents:          35000, // $350.00
			Status:               "DRAFT",
			RiskScore:            25.0,
			DualApprovalRequired: true,
			IsSyntheticFixture:   true,
		}
		if err := s.repo.CreatePayoutCase(ctx, case2); err != nil {
			return fmt.Errorf("failed to create demo case 2: %w", err)
		}

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
			IsSyntheticFixture:   true,
		}
		if err := s.repo.CreatePayoutOperation(ctx, op2); err != nil {
			return fmt.Errorf("failed to create demo operation 2: %w", err)
		}
		fmt.Printf("[+] Seeded Demo Case 2 (Dual Approval Required): %s\n", case2.Title)
	} else if err != nil {
		return fmt.Errorf("failed querying demo case 2: %w", err)
	} else {
		fmt.Printf("[i] Reusing Existing Demo Case 2: %s\n", case2.Title)
	}

	// 5. Seed Demo Case 3: Reconciled Equipment Subsidy
	case3, err := s.repo.GetPayoutCaseByReference(ctx, demoOrg.ID, DemoCase3Ref)
	if err != nil && errors.Is(err, db.ErrPayoutCaseNotFound) {
		case3 = &db.PayoutCase{
			OrganizationID:       demoOrg.ID,
			CreatedByUserID:      operatorUser.ID,
			Reference:            DemoCase3Ref,
			RecipientEmail:       "supplies@acme.example.com",
			RecipientName:        "Acme Logistics LLC",
			Title:                "[DEMO TEST FIXTURE] Settled Vendor Disbursement #V-1082",
			Description:          "Disbursement simulated as settled and confirmed.",
			Currency:             "USD",
			AmountCents:          12000, // $120.00
			Status:               "COMPLETED",
			RiskScore:            5.0,
			DualApprovalRequired: false,
			IsSyntheticFixture:   true,
		}
		if err := s.repo.CreatePayoutCase(ctx, case3); err != nil {
			return fmt.Errorf("failed to create demo case 3: %w", err)
		}

		batchID3 := "SIMULATED-DEMO-BATCH-V1082"
		itemID3 := "SIMULATED-DEMO-ITEM-V1082"
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
			IsSyntheticFixture:   true,
		}
		if err := s.repo.CreatePayoutOperation(ctx, op3); err != nil {
			return fmt.Errorf("failed to create demo operation 3: %w", err)
		}
		fmt.Printf("[+] Seeded Demo Case 3 (Settled): %s\n", case3.Title)
	} else if err != nil {
		return fmt.Errorf("failed querying demo case 3: %w", err)
	} else {
		fmt.Printf("[i] Reusing Existing Demo Case 3: %s\n", case3.Title)
	}

	// 6. Verification: Ensure all expected fixture records and memberships are intact
	return s.VerifyFixtures(ctx, demoOrg.ID)
}

// VerifyFixtures asserts that all expected demo fixtures and accounts are fully ready.
func (s *Seeder) VerifyFixtures(ctx context.Context, orgID string) error {
	refs := []string{DemoCase1Ref, DemoCase2Ref, DemoCase3Ref}
	for _, ref := range refs {
		c, err := s.repo.GetPayoutCaseByReference(ctx, orgID, ref)
		if err != nil {
			return fmt.Errorf("verification failed: case %s missing: %w", ref, err)
		}
		if !c.IsSyntheticFixture {
			return fmt.Errorf("verification failed: case %s is not marked as synthetic fixture", ref)
		}
		ops, err := s.repo.ListPayoutOperationsByCaseID(ctx, orgID, c.ID)
		if err != nil || len(ops) == 0 {
			return fmt.Errorf("verification failed: case %s has no operations: %v", ref, err)
		}
		if !ops[0].IsSyntheticFixture {
			return fmt.Errorf("verification failed: operation for case %s is not marked as synthetic fixture", ref)
		}
	}

	// Verify officer membership in demo org
	officer, err := s.repo.GetUserByEmail(ctx, OfficerEmail)
	if err != nil {
		return fmt.Errorf("verification failed: officer user missing: %w", err)
	}
	hasAccess, _, err := s.repo.VerifyUserOrgAccess(ctx, officer.ID, orgID)
	if err != nil || !hasAccess {
		return fmt.Errorf("verification failed: officer does not have membership in demo organization")
	}

	return nil
}
