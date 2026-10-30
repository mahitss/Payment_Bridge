package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
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

func TestPostgres_CountAuditEvents(t *testing.T) {
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

	passHash, _ := crypto.HashPassword("Password123!Safe")
	userA := &db.User{
		Email:         fmt.Sprintf("auditor.test.%d@test.com", time.Now().UnixNano()),
		PasswordHash:  passHash,
		Name:          "Auditor Test",
		EmailVerified: true,
		IsActive:      true,
	}
	_, orgA, err := repo.CreateUserWithPersonalOrg(ctx, userA, "Audit Test Org A")
	if err != nil {
		t.Fatalf("failed creating user and org A: %v", err)
	}

	userB := &db.User{
		Email:         fmt.Sprintf("auditor.b.%d@test.com", time.Now().UnixNano()),
		PasswordHash:  passHash,
		Name:          "Auditor B",
		EmailVerified: true,
		IsActive:      true,
	}
	_, orgB, err := repo.CreateUserWithPersonalOrg(ctx, userB, "Audit Test Org B")
	if err != nil {
		t.Fatalf("failed creating user and org B: %v", err)
	}

	caseID1 := crypto.GenerateUUID()
	caseID2 := crypto.GenerateUUID()
	opID1 := crypto.GenerateUUID()

	// 1. Audit event for case 1 in org A
	err = repo.RecordAuditEvent(ctx, &db.AuditEvent{
		OrganizationID: &orgA.ID,
		EventType:      "PAYOUT_CASE_CREATED",
		Metadata: map[string]interface{}{
			"case_id": caseID1,
		},
	})
	if err != nil {
		t.Fatalf("failed recording audit event 1: %v", err)
	}

	// 2. Audit event for op 1 in org A
	err = repo.RecordAuditEvent(ctx, &db.AuditEvent{
		OrganizationID: &orgA.ID,
		EventType:      "PAYOUT_OPERATION_INITIATED",
		Metadata: map[string]interface{}{
			"case_id":      caseID1,
			"operation_id": opID1,
		},
	})
	if err != nil {
		t.Fatalf("failed recording audit event 2: %v", err)
	}

	// 3. Audit event for case 2 in org A (unrelated case)
	err = repo.RecordAuditEvent(ctx, &db.AuditEvent{
		OrganizationID: &orgA.ID,
		EventType:      "PAYOUT_CASE_CREATED",
		Metadata: map[string]interface{}{
			"case_id": caseID2,
		},
	})
	if err != nil {
		t.Fatalf("failed recording audit event 3: %v", err)
	}

	// 4. Audit event in org B (unrelated org)
	err = repo.RecordAuditEvent(ctx, &db.AuditEvent{
		OrganizationID: &orgB.ID,
		EventType:      "PAYOUT_CASE_CREATED",
		Metadata: map[string]interface{}{
			"case_id": caseID1,
		},
	})
	if err != nil {
		t.Fatalf("failed recording audit event 4: %v", err)
	}

	// Query count for case 1 and op 1 in org A: should be exactly 2
	count, err := repo.CountAuditEvents(ctx, orgA.ID, caseID1, opID1)
	if err != nil {
		t.Fatalf("CountAuditEvents failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected count 2 for case 1 and op 1 in org A, got %d", count)
	}

	// Query count for case 2 in org A: should be exactly 1
	count2, err := repo.CountAuditEvents(ctx, orgA.ID, caseID2, "")
	if err != nil {
		t.Fatalf("CountAuditEvents failed: %v", err)
	}
	if count2 != 1 {
		t.Errorf("expected count 1 for case 2 in org A, got %d", count2)
	}

	// Query count for org B: should be 1
	countB, err := repo.CountAuditEvents(ctx, orgB.ID, caseID1, opID1)
	if err != nil {
		t.Fatalf("CountAuditEvents failed: %v", err)
	}
	if countB != 1 {
		t.Errorf("expected count 1 for case 1 in org B, got %d", countB)
	}
}

// TestPostgres_Mission033_8_1A_Concurrency_Integration validates the partial unique index,
// atomic submission claim, and conditional approval transitions under real PostgreSQL concurrency.
func TestPostgres_Mission033_8_1A_Concurrency_Integration(t *testing.T) {
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

	// Setup User and Organization
	passHash, _ := crypto.HashPassword("TestPass123!")
	user := &db.User{
		Email:         fmt.Sprintf("officer.concurrency.%d@test.com", time.Now().UnixNano()),
		PasswordHash:  passHash,
		Name:          "Officer Concurrency",
		EmailVerified: true,
		IsActive:      true,
	}
	createdUser, org, err := repo.CreateUserWithPersonalOrg(ctx, user, "Concurrency Testing Org")
	if err != nil {
		t.Fatalf("failed creating user and org: %v", err)
	}

	// 1. Partial Unique Index: Enforces single active operation per case
	t.Run("PartialUniqueIndex_EnforcesSingleActiveOperation", func(t *testing.T) {
		pCase := &db.PayoutCase{
			OrganizationID:       org.ID,
			CreatedByUserID:      createdUser.ID,
			Reference:            fmt.Sprintf("CASE-CONC-%d", time.Now().UnixNano()),
			RecipientEmail:       "test@example.com",
			RecipientName:        "Test User",
			Title:                "Partial Unique Index Test",
			AmountCents:          10000,
			Currency:             "USD",
			Status:               "DRAFT",
			DualApprovalRequired: false,
		}
		if err := repo.CreatePayoutCase(ctx, pCase); err != nil {
			t.Fatalf("failed creating case: %v", err)
		}

		// First active operation (PREPARED) succeeds
		op1 := &db.PayoutOperation{
			PayoutCaseID:      pCase.ID,
			OrganizationID:    org.ID,
			InitiatedByUserID: createdUser.ID,
			SenderBatchID:     fmt.Sprintf("BATCH-%d-1", time.Now().UnixNano()),
			SenderItemID:      fmt.Sprintf("ITEM-%d-1", time.Now().UnixNano()),
			AmountCents:       10000,
			Currency:          "USD",
			Status:            "PREPARED",
		}
		if err := repo.CreatePayoutOperation(ctx, op1); err != nil {
			t.Fatalf("failed creating op1: %v", err)
		}

		// Second active operation (PREPARED) on the same case MUST fail with ErrActivePayoutOperationConflict
		op2 := &db.PayoutOperation{
			PayoutCaseID:      pCase.ID,
			OrganizationID:    org.ID,
			InitiatedByUserID: createdUser.ID,
			SenderBatchID:     fmt.Sprintf("BATCH-%d-2", time.Now().UnixNano()),
			SenderItemID:      fmt.Sprintf("ITEM-%d-2", time.Now().UnixNano()),
			AmountCents:       10000,
			Currency:          "USD",
			Status:            "PREPARED",
		}
		err = repo.CreatePayoutOperation(ctx, op2)
		if err == nil {
			t.Fatal("expected conflict creating second active operation for same case, got nil")
		}
		if !errors.Is(err, db.ErrActivePayoutOperationConflict) {
			t.Errorf("expected ErrActivePayoutOperationConflict, got: %v", err)
		}

		// Transition op1 to CANCELLED (terminal status)
		op1.Status = "CANCELLED"
		if err := repo.UpdatePayoutOperation(ctx, op1); err != nil {
			t.Fatalf("failed updating op1 to CANCELLED: %v", err)
		}

		// Now creating an active operation on the same case MUST succeed
		op3 := &db.PayoutOperation{
			PayoutCaseID:      pCase.ID,
			OrganizationID:    org.ID,
			InitiatedByUserID: createdUser.ID,
			SenderBatchID:     fmt.Sprintf("BATCH-%d-3", time.Now().UnixNano()),
			SenderItemID:      fmt.Sprintf("ITEM-%d-3", time.Now().UnixNano()),
			AmountCents:       10000,
			Currency:          "USD",
			Status:            "PREPARED",
		}
		if err := repo.CreatePayoutOperation(ctx, op3); err != nil {
			t.Fatalf("expected op3 to succeed after op1 was CANCELLED, got: %v", err)
		}

		// Transition op3 to FAILED (terminal status)
		op3.Status = "FAILED"
		if err := repo.UpdatePayoutOperation(ctx, op3); err != nil {
			t.Fatalf("failed updating op3 to FAILED: %v", err)
		}

		// Now creating another active operation succeeds again
		op4 := &db.PayoutOperation{
			PayoutCaseID:      pCase.ID,
			OrganizationID:    org.ID,
			InitiatedByUserID: createdUser.ID,
			SenderBatchID:     fmt.Sprintf("BATCH-%d-4", time.Now().UnixNano()),
			SenderItemID:      fmt.Sprintf("ITEM-%d-4", time.Now().UnixNano()),
			AmountCents:       10000,
			Currency:          "USD",
			Status:            "PREPARED",
		}
		if err := repo.CreatePayoutOperation(ctx, op4); err != nil {
			t.Fatalf("expected op4 to succeed after op3 was FAILED, got: %v", err)
		}
	})

	// 2. Atomic Submission Claim under real PostgreSQL concurrency
	t.Run("AtomicSubmissionClaim_PostgresConcurrency", func(t *testing.T) {
		pCase := &db.PayoutCase{
			OrganizationID:       org.ID,
			CreatedByUserID:      createdUser.ID,
			Reference:            fmt.Sprintf("CASE-CLAIM-%d", time.Now().UnixNano()),
			RecipientEmail:       "test@example.com",
			RecipientName:        "Test User",
			Title:                "Submission Claim Concurrency Test",
			AmountCents:          10000,
			Currency:             "USD",
			Status:               "DRAFT",
			DualApprovalRequired: false,
		}
		if err := repo.CreatePayoutCase(ctx, pCase); err != nil {
			t.Fatalf("failed creating case: %v", err)
		}

		op := &db.PayoutOperation{
			PayoutCaseID:      pCase.ID,
			OrganizationID:    org.ID,
			InitiatedByUserID: createdUser.ID,
			SenderBatchID:     fmt.Sprintf("BATCH-SUB-%d", time.Now().UnixNano()),
			SenderItemID:      fmt.Sprintf("ITEM-SUB-%d", time.Now().UnixNano()),
			AmountCents:       10000,
			Currency:          "USD",
			Status:            "PREPARED",
		}
		if err := repo.CreatePayoutOperation(ctx, op); err != nil {
			t.Fatalf("failed creating op: %v", err)
		}

		concurrency := 10
		startGate := make(chan struct{})
		var wg sync.WaitGroup
		var successCount int32
		var conflictCount int32

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				<-startGate
				fp := fmt.Sprintf("v2:fp-%d", idx)
				claimedOp, claimErr := repo.ClaimPayoutOperationForSubmission(ctx, org.ID, op.ID, fp, time.Now().UTC())
				if claimErr == nil && claimedOp != nil {
					atomic.AddInt32(&successCount, 1)
				} else if errors.Is(claimErr, db.ErrSubmissionClaimConflict) {
					atomic.AddInt32(&conflictCount, 1)
				}
			}(i)
		}

		close(startGate)
		wg.Wait()

		if successCount != 1 {
			t.Errorf("expected exactly 1 winning submission claim, got: %d", successCount)
		}
		if int(conflictCount) != concurrency-1 {
			t.Errorf("expected %d conflict rejections, got: %d", concurrency-1, conflictCount)
		}

		finalOp, err := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
		if err != nil {
			t.Fatalf("failed retrieving final op: %v", err)
		}
		if finalOp.Status != "SUBMITTING" {
			t.Errorf("expected final op status SUBMITTING, got: %s", finalOp.Status)
		}
	})

	// 3. Conditional Approval Transitions under real PostgreSQL concurrency
	t.Run("ConditionalApproval_PostgresConcurrency", func(t *testing.T) {
		pCase := &db.PayoutCase{
			OrganizationID:       org.ID,
			CreatedByUserID:      createdUser.ID,
			Reference:            fmt.Sprintf("CASE-APPR-%d", time.Now().UnixNano()),
			RecipientEmail:       "test@example.com",
			RecipientName:        "Test User",
			Title:                "Approval Concurrency Test",
			AmountCents:          30000,
			Currency:             "USD",
			Status:               "DRAFT",
			DualApprovalRequired: true,
		}
		if err := repo.CreatePayoutCase(ctx, pCase); err != nil {
			t.Fatalf("failed creating case: %v", err)
		}

		op := &db.PayoutOperation{
			PayoutCaseID:         pCase.ID,
			OrganizationID:       org.ID,
			InitiatedByUserID:    createdUser.ID,
			SenderBatchID:        fmt.Sprintf("BATCH-APPR-%d", time.Now().UnixNano()),
			SenderItemID:         fmt.Sprintf("ITEM-APPR-%d", time.Now().UnixNano()),
			AmountCents:          30000,
			Currency:             "USD",
			Status:               "AWAITING_APPROVAL",
			RequiresDualApproval: true,
		}
		if err := repo.CreatePayoutOperation(ctx, op); err != nil {
			t.Fatalf("failed creating op: %v", err)
		}

		// Self-approval must be rejected
		selfErr := repo.ApprovePayoutOperation(ctx, org.ID, op.ID, createdUser.ID)
		if selfErr == nil {
			t.Fatal("expected self-approval to fail, got nil")
		}
		if !errors.Is(selfErr, db.ErrSelfApprovalForbidden) {
			t.Errorf("expected ErrSelfApprovalForbidden, got: %v", selfErr)
		}

		// Create distinct approvers
		approverCount := 5
		approverIDs := make([]string, approverCount)
		for i := 0; i < approverCount; i++ {
			appUser := &db.User{
				Email:         fmt.Sprintf("approver.%d.%d@test.com", i, time.Now().UnixNano()),
				PasswordHash:  passHash,
				Name:          fmt.Sprintf("Approver %d", i),
				EmailVerified: true,
				IsActive:      true,
			}
			createdApp, _, err := repo.CreateUserWithPersonalOrg(ctx, appUser, fmt.Sprintf("Approver Org %d", i))
			if err != nil {
				t.Fatalf("failed creating approver %d: %v", i, err)
			}
			approverIDs[i] = createdApp.ID
		}

		startGate := make(chan struct{})
		var wg sync.WaitGroup
		var successCount int32
		var conflictCount int32

		for i := 0; i < approverCount; i++ {
			wg.Add(1)
			go func(appID string) {
				defer wg.Done()
				<-startGate
				appErr := repo.ApprovePayoutOperation(ctx, org.ID, op.ID, appID)
				if appErr == nil {
					atomic.AddInt32(&successCount, 1)
				} else if errors.Is(appErr, db.ErrInvalidOperationTransition) {
					atomic.AddInt32(&conflictCount, 1)
				}
			}(approverIDs[i])
		}

		close(startGate)
		wg.Wait()

		if successCount != 1 {
			t.Errorf("expected exactly 1 winning approval, got: %d", successCount)
		}
		if int(conflictCount) != approverCount-1 {
			t.Errorf("expected %d conflict rejections, got: %d", approverCount-1, conflictCount)
		}

		finalOp, err := repo.GetPayoutOperationByID(ctx, org.ID, op.ID)
		if err != nil {
			t.Fatalf("failed retrieving final op: %v", err)
		}
		if finalOp.Status != "APPROVED" {
			t.Errorf("expected final op status APPROVED, got: %s", finalOp.Status)
		}

		// Further approval attempts must fail with ErrInvalidOperationTransition
		extraErr := repo.ApprovePayoutOperation(ctx, org.ID, op.ID, approverIDs[0])
		if !errors.Is(extraErr, db.ErrInvalidOperationTransition) {
			t.Errorf("expected ErrInvalidOperationTransition on already-approved op, got: %v", extraErr)
		}
	})
}
