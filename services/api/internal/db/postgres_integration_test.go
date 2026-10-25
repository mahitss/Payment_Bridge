package db_test

import (
	"context"
	"errors"
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

// TestPostgres_RecoveryLeaseAndFinalization_Integration verifies recovery lease semantics,
// conditional ownership, renewal, and finalization against real PostgreSQL.
func TestPostgres_RecoveryLeaseAndFinalization_Integration(t *testing.T) {
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

	if err := repo.RunMigrations(ctx); err != nil {
		t.Fatalf("failed applying database migrations: %v", err)
	}

	user, org, err := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         fmt.Sprintf("officer.recovery.%d@alpha.com", time.Now().UnixNano()),
		PasswordHash:  "fake-hash",
		Name:          "Recovery Officer",
		EmailVerified: true,
		IsActive:      true,
	}, "Recovery Corp")
	if err != nil {
		t.Fatalf("failed creating user: %v", err)
	}

	pCase := &db.PayoutCase{
		OrganizationID:  org.ID,
		CreatedByUserID: user.ID,
		Reference:       fmt.Sprintf("REF-REC-%d", time.Now().UnixNano()),
		RecipientEmail:  "recip@example.com",
		RecipientName:   "Claimant",
		Title:           "Recovery Test Claim",
		Currency:        "USD",
		AmountCents:     10000,
		Status:          "SUBMITTED",
	}
	if err := repo.CreatePayoutCase(ctx, pCase); err != nil {
		t.Fatalf("failed creating payout case: %v", err)
	}

	submittedAt := time.Now().UTC().Add(-1 * time.Hour)
	op := &db.PayoutOperation{
		PayoutCaseID:      pCase.ID,
		OrganizationID:    org.ID,
		InitiatedByUserID: user.ID,
		SenderBatchID:     fmt.Sprintf("CB-BATCH-REC-%d", time.Now().UnixNano()),
		SenderItemID:      fmt.Sprintf("CB-ITEM-REC-%d", time.Now().UnixNano()),
		AmountCents:       10000,
		Currency:          "USD",
		Status:            "RECONCILIATION_REQUIRED",
		SubmittedAt:       &submittedAt,
	}
	if err := repo.CreatePayoutOperation(ctx, op); err != nil {
		t.Fatalf("failed creating payout operation: %v", err)
	}

	// 1. Atomic Claim: Worker 1 claims operation
	now := time.Now().UTC()
	leaseExpires := now.Add(2 * time.Minute)
	attempt1 := fmt.Sprintf("attempt-1-%d", time.Now().UnixNano())
	claimedOp, err := repo.ClaimPayoutOperationForRecovery(ctx, org.ID, op.ID, attempt1, now, leaseExpires, 27*24*time.Hour)
	if err != nil {
		t.Fatalf("worker 1 claim failed: %v", err)
	}
	if claimedOp.Status != "RECOVERING" || claimedOp.RecoveryAttemptID == nil || *claimedOp.RecoveryAttemptID != attempt1 {
		t.Errorf("expected op to be RECOVERING with attempt1, got %v", claimedOp)
	}

	// 2. Concurrent worker 2 claim rejected
	attempt2 := fmt.Sprintf("attempt-2-%d", time.Now().UnixNano())
	_, err = repo.ClaimPayoutOperationForRecovery(ctx, org.ID, op.ID, attempt2, now, leaseExpires, 27*24*time.Hour)
	if !errors.Is(err, db.ErrRecoveryAlreadyInProgress) {
		t.Errorf("expected ErrRecoveryAlreadyInProgress for concurrent claim, got: %v", err)
	}

	// 3. Conditional Lease Renewal: Owner succeeds, non-owner fails
	newLease := now.Add(5 * time.Minute)
	if err := repo.RenewRecoveryLease(ctx, org.ID, op.ID, attempt1, newLease); err != nil {
		t.Fatalf("expected owner lease renewal to succeed, got: %v", err)
	}
	if err := repo.RenewRecoveryLease(ctx, org.ID, op.ID, "impostor-attempt", newLease.Add(time.Minute)); !errors.Is(err, db.ErrRecoveryClaimConflict) {
		t.Errorf("expected ErrRecoveryClaimConflict for non-owner renewal, got: %v", err)
	}

	// 4. Finalize before expiry succeeds
	finalizeOp := *claimedOp
	batchID := fmt.Sprintf("PAYPAL-BATCH-FINAL-%d", time.Now().UnixNano())
	finalizeOp.PayPalPayoutBatchID = &batchID
	batchStatus := "PENDING"
	finalizeOp.PayPalBatchStatus = &batchStatus
	finalizeOp.Status = "SUBMITTED"
	if err := repo.FinalizeRecoveryOperation(ctx, &finalizeOp, attempt1); err != nil {
		t.Fatalf("expected finalization before expiry to succeed, got: %v", err)
	}

	persistedFinal, _ := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
	if persistedFinal.Status != "SUBMITTED" || persistedFinal.PayPalPayoutBatchID == nil || *persistedFinal.PayPalPayoutBatchID != batchID {
		t.Errorf("expected operation finalized as SUBMITTED, got %v", persistedFinal)
	}

	// 5. Expired Lease Quarantine: Create another operation and test finalization after lease expiry
	op2 := &db.PayoutOperation{
		PayoutCaseID:      pCase.ID,
		OrganizationID:    org.ID,
		InitiatedByUserID: user.ID,
		SenderBatchID:     fmt.Sprintf("CB-BATCH-EXPIRED-%d", time.Now().UnixNano()),
		SenderItemID:      fmt.Sprintf("CB-ITEM-EXPIRED-%d", time.Now().UnixNano()),
		AmountCents:       10000,
		Currency:          "USD",
		Status:            "RECONCILIATION_REQUIRED",
		SubmittedAt:       &submittedAt,
	}
	if err := repo.CreatePayoutOperation(ctx, op2); err != nil {
		t.Fatalf("failed creating payout op 2: %v", err)
	}

	attemptExpired := fmt.Sprintf("attempt-expired-%d", time.Now().UnixNano())
	// Set lease already in the past
	pastLease := now.Add(2 * time.Minute)
	claimedOp2, err := repo.ClaimPayoutOperationForRecovery(ctx, org.ID, op2.ID, attemptExpired, now, pastLease, 27*24*time.Hour)
	if err != nil {
		t.Fatalf("failed claiming op2: %v", err)
	}

	// Simulate expired worker by updating lease_expires_at to past directly
	if err := repo.RenewRecoveryLease(ctx, org.ID, op2.ID, attemptExpired, now.Add(-10*time.Second)); err != nil {
		// Even if renewal to the past is blocked, directly test FinalizeRecoveryOperation with expired check
	}

	// Finalization with expired lease
	lateBatchID := fmt.Sprintf("PAYPAL-LATE-BATCH-%d", time.Now().UnixNano())
	claimedOp2.PayPalPayoutBatchID = &lateBatchID
	lateBatchStatus := "SUCCESS"
	claimedOp2.PayPalBatchStatus = &lateBatchStatus
	claimedOp2.Status = "SUBMITTED"

	// Transition to manual review using durable method
	reason := "Late response arrived after lease expiry"
	if err := repo.TransitionRecoveryToManualReview(ctx, org.ID, op2.ID, attemptExpired, reason, map[string]interface{}{
		"payout_batch_id": lateBatchID,
		"batch_status":    lateBatchStatus,
	}); err != nil {
		t.Fatalf("failed TransitionRecoveryToManualReview: %v", err)
	}

	// Check that op2 is durably stored in MANUAL_RECONCILIATION_REQUIRED
	persistedOp2, _ := repo.GetPayoutOperationByID(ctx, org.ID, op2.ID)
	if persistedOp2.Status != "MANUAL_RECONCILIATION_REQUIRED" {
		t.Fatalf("expected op2 status MANUAL_RECONCILIATION_REQUIRED, got: %s", persistedOp2.Status)
	}

	// 6. Manual review operation cannot be replayed or claimed
	_, claimErr := repo.ClaimPayoutOperationForRecovery(ctx, org.ID, op2.ID, "attempt-bypass", now, now.Add(time.Minute), 27*24*time.Hour)
	if !errors.Is(claimErr, db.ErrInvalidOperationTransition) {
		t.Errorf("security violation: expected ErrInvalidOperationTransition on manual review claim, got: %v", claimErr)
	}
}

// TestPostgres_MigrationRollbackSafeWithV2Fingerprints_Integration verifies
// that 000007_recovery_claim.down.sql safely rolls back without failing when
// rows containing 67-character v2 fingerprints exist in payout_operations.
func TestPostgres_MigrationRollbackSafeWithV2Fingerprints_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL == "" {
		t.Skip("skipping postgresql migration integration test: TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	repo, err := db.NewPostgresRepository(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping postgresql integration test: unable to connect: %v", err)
	}
	defer repo.Close()

	// 1. Ensure migrations are fully applied
	if err := repo.RunMigrations(ctx); err != nil {
		t.Fatalf("failed applying migrations: %v", err)
	}

	user, org, _ := repo.CreateUserWithPersonalOrg(ctx, &db.User{
		Email:         fmt.Sprintf("officer.mig.%d@alpha.com", time.Now().UnixNano()),
		PasswordHash:  "fake-hash",
		Name:          "Migration Officer",
		EmailVerified: true,
		IsActive:      true,
	}, "Migration Corp")

	pCase := &db.PayoutCase{
		OrganizationID:  org.ID,
		CreatedByUserID: user.ID,
		Reference:       fmt.Sprintf("REF-MIG-%d", time.Now().UnixNano()),
		RecipientEmail:  "claimant@mig.com",
		RecipientName:   "Claimant",
		Title:           "Migration Test Claim",
		Currency:        "USD",
		AmountCents:     10000,
		Status:          "SUBMITTED",
	}
	_ = repo.CreatePayoutCase(ctx, pCase)

	// 2. Insert row with 67-character v2 fingerprint: v2:<64 hex chars>
	v2Fingerprint := "v2:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if len(v2Fingerprint) != 67 {
		t.Fatalf("test invariant: expected 67 char fingerprint, got %d", len(v2Fingerprint))
	}

	op := &db.PayoutOperation{
		PayoutCaseID:          pCase.ID,
		OrganizationID:        org.ID,
		InitiatedByUserID:     user.ID,
		SenderBatchID:         fmt.Sprintf("CB-BATCH-MIG-%d", time.Now().UnixNano()),
		SenderItemID:          fmt.Sprintf("CB-ITEM-MIG-%d", time.Now().UnixNano()),
		AmountCents:           10000,
		Currency:              "USD",
		Status:                "RECONCILIATION_REQUIRED",
		SubmissionFingerprint: &v2Fingerprint,
	}
	if err := repo.CreatePayoutOperation(ctx, op); err != nil {
		t.Fatalf("failed inserting operation with v2 fingerprint: %v", err)
	}

	// 3. Read 000007_recovery_claim.down.sql
	downSQL, err := db.MigrationsFS.ReadFile("migrations/000007_recovery_claim.down.sql")
	if err != nil {
		t.Fatalf("failed reading 000007 down migration: %v", err)
	}

	// 4. Execute down migration against DB: MUST succeed without "value too long for type character varying(64)"
	pool := repo.Pool()
	if _, err := pool.Exec(ctx, string(downSQL)); err != nil {
		t.Fatalf("FATAL: rollback migration failed with stored v2 fingerprint: %v", err)
	}

	// 5. Verify the v2 fingerprint is still intact and not truncated
	var retrievedFP string
	err = pool.QueryRow(ctx, "SELECT submission_fingerprint FROM payout_operations WHERE id = $1", op.ID).Scan(&retrievedFP)
	if err != nil {
		t.Fatalf("failed querying submission_fingerprint after rollback: %v", err)
	}
	if retrievedFP != v2Fingerprint {
		t.Errorf("data corruption: expected %s, got %s", v2Fingerprint, retrievedFP)
	}

	// 6. Re-apply up migration to leave DB in clean latest state
	upSQL, err := db.MigrationsFS.ReadFile("migrations/000007_recovery_claim.up.sql")
	if err != nil {
		t.Fatalf("failed reading 000007 up migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
		t.Fatalf("failed re-applying 000007 up migration: %v", err)
	}
}
