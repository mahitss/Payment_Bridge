package db_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
)

// TestPostgres_Integration verifies migrations, persistence, and multi-tenant isolation against real PostgreSQL.
func TestPostgres_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL == "" {
		t.Skip("skipping postgresql integration test: TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := db.NewPostgresRepository(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping postgresql integration test: unable to connect to %s: %v", dbURL, err)
	}
	defer repo.Close()

	// 1. Apply real database migrations
	if err := repo.RunMigrations(ctx); err != nil {
		t.Fatalf("failed applying database migrations: %v", err)
	}

	// 2. Multi-Tenant Setup: Create Org A and Org B with distinct users
	passHashA, _ := crypto.HashPassword("Password123!Safe")
	userA := &db.User{
		Email:         fmt.Sprintf("officer.a.%d@alpha.com", time.Now().UnixNano()),
		PasswordHash:  passHashA,
		Name:          "Officer Alice",
		EmailVerified: true,
		IsActive:      true,
	}
	createdUserA, orgA, err := repo.CreateUserWithPersonalOrg(ctx, userA, "Alpha Mutual")
	if err != nil {
		t.Fatalf("failed creating user A and org A: %v", err)
	}

	passHashB, _ := crypto.HashPassword("Password123!Safe")
	userB := &db.User{
		Email:         fmt.Sprintf("officer.b.%d@beta.com", time.Now().UnixNano()),
		PasswordHash:  passHashB,
		Name:          "Officer Bob",
		EmailVerified: true,
		IsActive:      true,
	}
	createdUserB, orgB, err := repo.CreateUserWithPersonalOrg(ctx, userB, "Beta Assurance")
	if err != nil {
		t.Fatalf("failed creating user B and org B: %v", err)
	}

	// 3. Verify Session Lifecycle & Hashing
	rawToken := fmt.Sprintf("integration-test-session-%d", time.Now().UnixNano())
	tokenHash := crypto.HashToken(rawToken)
	session := &db.Session{
		UserID:               createdUserA.ID,
		TokenHash:            tokenHash,
		ActiveOrganizationID: &orgA.ID,
		ExpiresAt:            time.Now().Add(24 * time.Hour),
	}
	if err := repo.CreateSession(ctx, session); err != nil {
		t.Fatalf("failed creating session: %v", err)
	}

	retrievedSession, retrievedUser, retrievedOrg, err := repo.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil || retrievedSession.UserID != createdUserA.ID || retrievedUser.ID != createdUserA.ID || retrievedOrg.ID != orgA.ID {
		t.Fatalf("failed retrieving session by token hash: %v", err)
	}

	if err := repo.DeleteSession(ctx, tokenHash); err != nil {
		t.Fatalf("failed deleting session: %v", err)
	}

	deletedCheck, _, _, _ := repo.GetSessionByTokenHash(ctx, tokenHash)
	if deletedCheck != nil {
		t.Fatalf("expected nil after session deletion, got: %v", deletedCheck)
	}

	// 4. Create Payout Cases in Tenant A and Tenant B
	caseA := &db.PayoutCase{
		OrganizationID:       orgA.ID,
		CreatedByUserID:      createdUserA.ID,
		Reference:            fmt.Sprintf("REF-A-%d", time.Now().UnixNano()),
		RecipientEmail:       "claimant.a@example.com",
		RecipientName:        "Claimant A",
		Title:                "Water Damage Claim Alpha",
		Currency:             "USD",
		AmountCents:          35000, // $350.00
		Status:               "DRAFT",
		RiskScore:            12.5,
		DualApprovalRequired: true,
	}
	if err := repo.CreatePayoutCase(ctx, caseA); err != nil {
		t.Fatalf("failed creating payout case A: %v", err)
	}

	caseB := &db.PayoutCase{
		OrganizationID:       orgB.ID,
		CreatedByUserID:      createdUserB.ID,
		Reference:            fmt.Sprintf("REF-B-%d", time.Now().UnixNano()),
		RecipientEmail:       "claimant.b@example.com",
		RecipientName:        "Claimant B",
		Title:                "Auto Collision Claim Beta",
		Currency:             "USD",
		AmountCents:          15000, // $150.00
		Status:               "DRAFT",
		RiskScore:            5.0,
		DualApprovalRequired: false,
	}
	if err := repo.CreatePayoutCase(ctx, caseB); err != nil {
		t.Fatalf("failed creating payout case B: %v", err)
	}

	// 5. Strict Multi-Tenant Scoping Tests (Anti-IDOR)
	// Org B queries Org A's case -> MUST return error / not found
	foreignCase, err := repo.GetPayoutCaseByID(ctx, orgB.ID, caseA.ID)
	if err == nil && foreignCase != nil {
		t.Fatalf("SECURITY VIOLATION: Org B successfully read Org A's case %s", caseA.ID)
	}

	// Org A lists cases -> MUST contain Case A, MUST NOT contain Case B
	casesA, _, err := repo.ListPayoutCasesByOrg(ctx, orgA.ID, 50, 0)
	if err != nil {
		t.Fatalf("failed listing cases for Org A: %v", err)
	}
	foundA := false
	for _, c := range casesA {
		if c.ID == caseB.ID {
			t.Fatalf("SECURITY VIOLATION: Org A case list leaked Org B case %s", caseB.ID)
		}
		if c.ID == caseA.ID {
			foundA = true
		}
	}
	if !foundA {
		t.Fatalf("expected case A in Org A list, not found")
	}

	// 6. Payout Operations & Status Snapshots
	senderBatchID := fmt.Sprintf("CB-BATCH-%d", time.Now().UnixNano())
	senderItemID := fmt.Sprintf("CB-ITEM-%d", time.Now().UnixNano())
	op := &db.PayoutOperation{
		PayoutCaseID:         caseA.ID,
		OrganizationID:       orgA.ID,
		InitiatedByUserID:    createdUserA.ID,
		SenderBatchID:        senderBatchID,
		SenderItemID:         senderItemID,
		AmountCents:          caseA.AmountCents,
		Currency:             "USD",
		Status:               "AWAITING_APPROVAL",
		RequiresDualApproval: true,
	}
	if err := repo.CreatePayoutOperation(ctx, op); err != nil {
		t.Fatalf("failed creating payout operation: %v", err)
	}

	// Foreign tenant B cannot read operation
	foreignOp, _ := repo.GetPayoutOperationByID(ctx, orgB.ID, op.ID)
	if foreignOp != nil {
		t.Fatalf("SECURITY VIOLATION: Org B read Org A payout operation %s", op.ID)
	}

	// Snapshot persistence with JSONB
	itemStatus := "UNCLAIMED"
	snapshot := &db.PayoutStatusSnapshot{
		PayoutOperationID: op.ID,
		OrganizationID:    orgA.ID,
		Provider:          "PAYPAL",
		BatchStatus:       "PENDING",
		ItemStatus:        &itemStatus,
		RawResponse: map[string]interface{}{
			"batch_header": map[string]interface{}{
				"payout_batch_id": "PAYPAL-LIVE-TEST-001",
			},
		},
	}
	if err := repo.CreatePayoutStatusSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("failed persisting status snapshot: %v", err)
	}

	snapshots, err := repo.ListPayoutStatusSnapshots(ctx, orgA.ID, op.ID)
	if err != nil || len(snapshots) == 0 {
		t.Fatalf("failed retrieving snapshots: %v", err)
	}

	// 7. Cold Restart Verification (New Connection Pool reads data)
	repo2, err := db.NewPostgresRepository(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed creating secondary connection pool: %v", err)
	}
	defer repo2.Close()

	persistedCase, err := repo2.GetPayoutCaseByID(ctx, orgA.ID, caseA.ID)
	if err != nil || persistedCase.AmountCents != 35000 {
		t.Fatalf("cold restart persistence check failed on case A: %v", err)
	}
}
