package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMockRepository_CreateUserAndOrg(t *testing.T) {
	repo := NewMockRepository()
	ctx := context.Background()

	user := &User{
		Email:        "Alice@Example.com",
		PasswordHash: "dummyhash",
		Name:         "Alice Smith",
	}

	createdUser, createdOrg, err := repo.CreateUserWithPersonalOrg(ctx, user, "Alice's Workspace")
	if err != nil {
		t.Fatalf("unexpected error creating user: %v", err)
	}

	if createdUser.Email != "alice@example.com" {
		t.Errorf("expected normalized email alice@example.com, got: %s", createdUser.Email)
	}
	if createdOrg.Name != "Alice's Workspace" {
		t.Errorf("unexpected org name: %s", createdOrg.Name)
	}

	// Test Duplicate Email Error
	_, _, err = repo.CreateUserWithPersonalOrg(ctx, user, "Another Org")
	if !errors.Is(err, ErrUserAlreadyExists) {
		t.Errorf("expected ErrUserAlreadyExists on duplicate email, got: %v", err)
	}
}

func TestMockRepository_SessionLifecycle(t *testing.T) {
	repo := NewMockRepository()
	ctx := context.Background()

	user, org, _ := repo.CreateUserWithPersonalOrg(ctx, &User{
		Email:        "bob@example.com",
		PasswordHash: "hash",
		Name:         "Bob",
	}, "Bob Org")

	session := &Session{
		UserID:               user.ID,
		TokenHash:            "dummy-token-hash-12345",
		ActiveOrganizationID: &org.ID,
		ExpiresAt:            time.Now().UTC().Add(24 * time.Hour),
	}

	err := repo.CreateSession(ctx, session)
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Retrieve session
	retrievedSess, retrievedUser, retrievedOrg, err := repo.GetSessionByTokenHash(ctx, "dummy-token-hash-12345")
	if err != nil {
		t.Fatalf("failed to retrieve session: %v", err)
	}
	if retrievedSess.UserID != user.ID {
		t.Errorf("session user id mismatch: %s vs %s", retrievedSess.UserID, user.ID)
	}
	if retrievedUser.Email != "bob@example.com" {
		t.Errorf("user email mismatch: %s", retrievedUser.Email)
	}
	if retrievedOrg.ID != org.ID {
		t.Errorf("org id mismatch: %s", retrievedOrg.ID)
	}

	// Delete single session (logout)
	err = repo.DeleteSession(ctx, "dummy-token-hash-12345")
	if err != nil {
		t.Fatalf("failed to delete session: %v", err)
	}

	_, _, _, err = repo.GetSessionByTokenHash(ctx, "dummy-token-hash-12345")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expected ErrSessionNotFound after deletion, got: %v", err)
	}
}

func TestMockRepository_SingleUseTokens(t *testing.T) {
	repo := NewMockRepository()
	ctx := context.Background()

	// 1. Email Verification Token
	vToken := &EmailVerificationToken{
		UserID:    "user-1",
		TokenHash: "verify-hash-123",
		ExpiresAt: time.Now().UTC().Add(1 * time.Hour),
	}
	err := repo.CreateEmailVerificationToken(ctx, vToken)
	if err != nil {
		t.Fatalf("failed to create verify token: %v", err)
	}

	consumed, err := repo.ConsumeEmailVerificationToken(ctx, "verify-hash-123")
	if err != nil {
		t.Fatalf("failed to consume token: %v", err)
	}
	if consumed.ConsumedAt == nil {
		t.Errorf("expected ConsumedAt to be non-nil")
	}

	// Attempt second consumption (must fail)
	_, err = repo.ConsumeEmailVerificationToken(ctx, "verify-hash-123")
	if !errors.Is(err, ErrTokenNotFoundOrUsed) {
		t.Errorf("expected ErrTokenNotFoundOrUsed on second consumption, got: %v", err)
	}

	// 2. Password Reset Token
	rToken := &PasswordResetToken{
		UserID:    "user-1",
		TokenHash: "reset-hash-456",
		ExpiresAt: time.Now().UTC().Add(30 * time.Minute),
	}
	err = repo.CreatePasswordResetToken(ctx, rToken)
	if err != nil {
		t.Fatalf("failed to create reset token: %v", err)
	}

	consumedReset, err := repo.ConsumePasswordResetToken(ctx, "reset-hash-456")
	if err != nil {
		t.Fatalf("failed to consume reset token: %v", err)
	}
	if consumedReset.ConsumedAt == nil {
		t.Errorf("expected ConsumedAt to be non-nil on reset token")
	}

	// Attempt second consumption (must fail)
	_, err = repo.ConsumePasswordResetToken(ctx, "reset-hash-456")
	if !errors.Is(err, ErrTokenNotFoundOrUsed) {
		t.Errorf("expected ErrTokenNotFoundOrUsed on second reset token consumption, got: %v", err)
	}
}

func TestMockRepository_MultiTenantIsolation(t *testing.T) {
	repo := NewMockRepository()
	ctx := context.Background()

	userA, orgA, _ := repo.CreateUserWithPersonalOrg(ctx, &User{Email: "a@example.com", PasswordHash: "h", Name: "A"}, "Org A")
	userB, orgB, _ := repo.CreateUserWithPersonalOrg(ctx, &User{Email: "b@example.com", PasswordHash: "h", Name: "B"}, "Org B")

	// User A can access Org A
	okA, roleA, err := repo.VerifyUserOrgAccess(ctx, userA.ID, orgA.ID)
	if err != nil || !okA || roleA != "OWNER" {
		t.Errorf("User A should have access to Org A as OWNER, got: %v, %s, %v", okA, roleA, err)
	}

	// User A CANNOT access Org B
	okB, _, err := repo.VerifyUserOrgAccess(ctx, userA.ID, orgB.ID)
	if !errors.Is(err, ErrAccessDenied) || okB {
		t.Errorf("User A should be denied access to Org B, got: %v, %v", okB, err)
	}

	// User B CANNOT access Org A
	okCross, _, err := repo.VerifyUserOrgAccess(ctx, userB.ID, orgA.ID)
	if !errors.Is(err, ErrAccessDenied) || okCross {
		t.Errorf("User B should be denied access to Org A, got: %v, %v", okCross, err)
	}
}

func TestMockRepository_PayoutCasesAndOperations(t *testing.T) {
	repo := NewMockRepository()
	ctx := context.Background()

	userA, orgA, _ := repo.CreateUserWithPersonalOrg(ctx, &User{Email: "a@claim.com", PasswordHash: "h", Name: "A"}, "Org A")
	userB, orgB, _ := repo.CreateUserWithPersonalOrg(ctx, &User{Email: "b@claim.com", PasswordHash: "h", Name: "B"}, "Org B")

	// 1. Create Payout Case
	pCase := &PayoutCase{
		OrganizationID:       orgA.ID,
		CreatedByUserID:      userA.ID,
		Reference:            "CASE-2026-001",
		RecipientEmail:       "claimant@example.com",
		RecipientName:        "Jane Claimant",
		Title:                "Emergency Medical Reimbursement",
		Description:          "Validated dental claim",
		Currency:             "USD",
		AmountCents:          35000, // $350.00
		Status:               "DRAFT",
		RiskScore:            25.5,
		DualApprovalRequired: true,
	}

	err := repo.CreatePayoutCase(ctx, pCase)
	if err != nil {
		t.Fatalf("failed to create payout case: %v", err)
	}

	// Reference uniqueness within same org
	errDup := repo.CreatePayoutCase(ctx, &PayoutCase{
		OrganizationID:  orgA.ID,
		CreatedByUserID: userA.ID,
		Reference:       "CASE-2026-001",
		RecipientEmail:  "other@example.com",
		AmountCents:     1000,
	})
	if errDup == nil {
		t.Errorf("expected duplicate reference error in same org")
	}

	// Same reference in different org is allowed
	errOtherOrg := repo.CreatePayoutCase(ctx, &PayoutCase{
		OrganizationID:  orgB.ID,
		CreatedByUserID: userB.ID,
		Reference:       "CASE-2026-001",
		RecipientEmail:  "other@example.com",
		AmountCents:     1000,
	})
	if errOtherOrg != nil {
		t.Errorf("same reference in different org should succeed, got: %v", errOtherOrg)
	}

	// Retrieve case
	retrievedCase, err := repo.GetPayoutCaseByID(ctx, orgA.ID, pCase.ID)
	if err != nil {
		t.Fatalf("failed to get payout case: %v", err)
	}
	if retrievedCase.AmountCents != 35000 {
		t.Errorf("expected 35000 cents, got %d", retrievedCase.AmountCents)
	}

	// Cross-tenant access denial
	_, errCross := repo.GetPayoutCaseByID(ctx, orgB.ID, pCase.ID)
	if !errors.Is(errCross, ErrPayoutCaseNotFound) {
		t.Errorf("expected ErrPayoutCaseNotFound for cross-org lookup, got: %v", errCross)
	}

	// 2. Create Payout Operation
	op := &PayoutOperation{
		PayoutCaseID:         pCase.ID,
		OrganizationID:       orgA.ID,
		InitiatedByUserID:    userA.ID,
		SenderBatchID:        "BATCH-CLAIM-001",
		SenderItemID:         "ITEM-CLAIM-001",
		AmountCents:          35000,
		Currency:             "USD",
		Status:               "AWAITING_APPROVAL",
		RequiresDualApproval: true,
	}

	err = repo.CreatePayoutOperation(ctx, op)
	if err != nil {
		t.Fatalf("failed to create payout operation: %v", err)
	}

	// 3. Test Self-Approval Rejection
	errSelfApprove := repo.ApprovePayoutOperation(ctx, orgA.ID, op.ID, userA.ID)
	if !errors.Is(errSelfApprove, ErrSelfApprovalForbidden) {
		t.Errorf("expected ErrSelfApprovalForbidden on self-approval, got: %v", errSelfApprove)
	}

	// Dual approval by distinct user in same org
	userApprover := &User{ID: "approver-user-id", Email: "auditor2@claim.com", Name: "Auditor 2"}
	repo.Users[userApprover.ID] = userApprover
	errApprove := repo.ApprovePayoutOperation(ctx, orgA.ID, op.ID, userApprover.ID)
	if errApprove != nil {
		t.Fatalf("expected approval by distinct user to succeed, got: %v", errApprove)
	}

	approvedOp, _ := repo.GetPayoutOperationByID(ctx, orgA.ID, op.ID)
	if approvedOp.Status != "APPROVED" || approvedOp.ApprovedByUserID == nil || *approvedOp.ApprovedByUserID != userApprover.ID {
		t.Errorf("expected operation to be APPROVED by %s, got %+v", userApprover.ID, approvedOp)
	}

	// 4. Status Snapshot
	errSnap := repo.CreatePayoutStatusSnapshot(ctx, &PayoutStatusSnapshot{
		PayoutOperationID: op.ID,
		OrganizationID:    orgA.ID,
		Provider:          "PAYPAL",
		BatchStatus:       "SUCCESS",
		RawResponse:       map[string]interface{}{"batch_id": "PAYPAL-123"},
	})
	if errSnap != nil {
		t.Fatalf("failed to create status snapshot: %v", errSnap)
	}

	snaps, err := repo.ListPayoutStatusSnapshots(ctx, orgA.ID, op.ID)
	if err != nil || len(snaps) != 1 {
		t.Errorf("expected 1 snapshot, got %d, err: %v", len(snaps), err)
	}
}

func TestMockRepository_Investigations(t *testing.T) {
	ctx := context.Background()
	repo := NewMockRepository()

	orgA := &Organization{ID: "org-ai-1", Name: "AI Org 1", Slug: "ai-org-1"}
	orgB := &Organization{ID: "org-ai-2", Name: "AI Org 2", Slug: "ai-org-2"}
	userA := &User{ID: "user-ai-1", Email: "investigator@claim.com", Name: "Investigator"}
	repo.Orgs[orgA.ID] = orgA
	repo.Orgs[orgB.ID] = orgB
	repo.Users[userA.ID] = userA

	pCase := &PayoutCase{
		ID:              "case-ai-100",
		OrganizationID:  orgA.ID,
		CreatedByUserID: userA.ID,
		Reference:       "CASE-AI-001",
		RecipientEmail:  "dest@example.com",
		AmountCents:     12500,
		Status:          "SUBMITTED",
	}
	repo.Cases[pCase.ID] = pCase

	inv := &PayoutInvestigation{
		PayoutCaseID:        pCase.ID,
		OrganizationID:      orgA.ID,
		RequestedByUserID:   userA.ID,
		Provider:            "ollama",
		Model:               "llama3.2",
		PromptVersion:       "v1.0",
		Status:              "COMPLETED",
		Summary:             "Payment submitted to PayPal and awaiting recipient claiming.",
		Confidence:          "HIGH",
		RequiresHumanReview: false,
		ObservedFacts:       []string{"Case status is SUBMITTED"},
		Findings: []InvestigationFinding{
			{
				Explanation:        "Batch dispatched to PayPal sandbox",
				EvidenceReferences: []string{pCase.ID},
			},
		},
		RecommendedNextSteps: []InvestigationRecommendation{
			{
				Action:                       "CHECK_PROVIDER_STATUS",
				Reason:                       "Poll PayPal API for claim settlement",
				RequiresHumanReview:          false,
				DeterministicPolicyPermitted: true,
			},
		},
	}

	if err := repo.CreateInvestigation(ctx, inv); err != nil {
		t.Fatalf("failed to create investigation: %v", err)
	}
	if inv.ID == "" {
		t.Fatalf("expected generated ID for investigation")
	}

	// Retrieve by ID
	fetched, err := repo.GetInvestigationByID(ctx, orgA.ID, inv.ID)
	if err != nil {
		t.Fatalf("failed to fetch investigation: %v", err)
	}
	if fetched.Summary != inv.Summary {
		t.Errorf("expected summary %q, got %q", inv.Summary, fetched.Summary)
	}

	// Cross-tenant access
	_, errCross := repo.GetInvestigationByID(ctx, orgB.ID, inv.ID)
	if !errors.Is(errCross, ErrInvestigationNotFound) {
		t.Errorf("expected ErrInvestigationNotFound on cross-tenant lookup, got %v", errCross)
	}

	// List by case
	list, err := repo.ListInvestigationsByCaseID(ctx, orgA.ID, pCase.ID)
	if err != nil || len(list) != 1 {
		t.Errorf("expected 1 investigation in list, got %d, err: %v", len(list), err)
	}
}
