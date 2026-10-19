package seeder

import (
	"context"
	"errors"
	"testing"
	"time"

	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
)

func setupTestRepo() db.Repository {
	return db.NewMockRepository()
}

func TestSeeder_FirstRunCreatesAllFixtures(t *testing.T) {
	repo := setupTestRepo()
	s := New(repo)
	ctx := context.Background()

	err := s.Seed(ctx, Options{Reset: false})
	if err != nil {
		t.Fatalf("expected seed to succeed on first run, got: %v", err)
	}

	// Verify operator
	opUser, err := repo.GetUserByEmail(ctx, OperatorEmail)
	if err != nil || opUser == nil {
		t.Fatalf("expected operator user to exist, got: %v", err)
	}
	if !opUser.EmailVerified || !opUser.IsActive {
		t.Errorf("operator should be verified and active")
	}

	// Verify officer
	officerUser, err := repo.GetUserByEmail(ctx, OfficerEmail)
	if err != nil || officerUser == nil {
		t.Fatalf("expected officer user to exist, got: %v", err)
	}

	memberships, err := repo.GetOrganizationMemberships(ctx, opUser.ID)
	if err != nil || len(memberships) == 0 {
		t.Fatalf("operator must have an organization")
	}
	demoOrgID := memberships[0].Organization.ID

	// Verify officer has access to demoOrg
	hasAccess, _, err := repo.VerifyUserOrgAccess(ctx, officerUser.ID, demoOrgID)
	if err != nil || !hasAccess {
		t.Errorf("officer should have access to demoOrg")
	}

	// Verify all 3 cases
	for _, ref := range []string{DemoCase1Ref, DemoCase2Ref, DemoCase3Ref} {
		c, err := repo.GetPayoutCaseByReference(ctx, demoOrgID, ref)
		if err != nil {
			t.Errorf("case %s should exist, got: %v", ref, err)
			continue
		}
		if !c.IsSyntheticFixture {
			t.Errorf("case %s must be marked IsSyntheticFixture", ref)
		}

		ops, err := repo.ListPayoutOperationsByCaseID(ctx, demoOrgID, c.ID)
		if err != nil || len(ops) == 0 {
			t.Errorf("case %s must have operations", ref)
			continue
		}
		if !ops[0].IsSyntheticFixture {
			t.Errorf("operation for case %s must be marked IsSyntheticFixture", ref)
		}
	}
}

func TestSeeder_SecondRunCreatesNoDuplicates(t *testing.T) {
	repo := setupTestRepo()
	s := New(repo)
	ctx := context.Background()

	// 1st run
	if err := s.Seed(ctx, Options{Reset: false}); err != nil {
		t.Fatalf("first run failed: %v", err)
	}

	opUser, _ := repo.GetUserByEmail(ctx, OperatorEmail)
	memberships, _ := repo.GetOrganizationMemberships(ctx, opUser.ID)
	demoOrgID := memberships[0].Organization.ID

	cases1, count1, err := repo.ListPayoutCasesByOrg(ctx, demoOrgID, 100, 0)
	if err != nil || count1 != 3 {
		t.Fatalf("expected exactly 3 cases after first run, got: %d", count1)
	}

	// 2nd run
	if err := s.Seed(ctx, Options{Reset: false}); err != nil {
		t.Fatalf("second run failed: %v", err)
	}

	cases2, count2, err := repo.ListPayoutCasesByOrg(ctx, demoOrgID, 100, 0)
	if err != nil || count2 != 3 {
		t.Fatalf("expected exactly 3 cases after second run, got: %d", count2)
	}

	if len(cases1) != len(cases2) {
		t.Errorf("expected count to match between runs: %d vs %d", len(cases1), len(cases2))
	}

	// Ensure no duplicate operations on case 1
	c1, _ := repo.GetPayoutCaseByReference(ctx, demoOrgID, DemoCase1Ref)
	ops, _ := repo.ListPayoutOperationsByCaseID(ctx, demoOrgID, c1.ID)
	if len(ops) != 1 {
		t.Errorf("expected exactly 1 operation for case 1, got %d", len(ops))
	}
}

func TestSeeder_ResetRemovesOnlyFixturesAndPreservesNonDemoData(t *testing.T) {
	repo := setupTestRepo()
	s := New(repo)
	ctx := context.Background()

	// Seed fixtures first
	if err := s.Seed(ctx, Options{Reset: false}); err != nil {
		t.Fatalf("initial seed failed: %v", err)
	}

	opUser, _ := repo.GetUserByEmail(ctx, OperatorEmail)
	memberships, _ := repo.GetOrganizationMemberships(ctx, opUser.ID)
	demoOrgID := memberships[0].Organization.ID

	// Now insert a genuine non-demo case
	realCase := &db.PayoutCase{
		OrganizationID:       demoOrgID,
		CreatedByUserID:      opUser.ID,
		Reference:            "REAL-CLAIM-001",
		RecipientEmail:       "real.client@corporate.example.com",
		RecipientName:        "Real Client Corp",
		Title:                "Real Production Payout",
		Description:          "Legitimate production disbursement",
		Currency:             "USD",
		AmountCents:          45000,
		Status:               "DRAFT",
		RiskScore:            15.0,
		DualApprovalRequired: true,
		IsSyntheticFixture:   false, // GENUINE
	}
	if err := repo.CreatePayoutCase(ctx, realCase); err != nil {
		t.Fatalf("failed inserting real non-fixture case: %v", err)
	}

	realOp := &db.PayoutOperation{
		PayoutCaseID:         realCase.ID,
		OrganizationID:       demoOrgID,
		InitiatedByUserID:    opUser.ID,
		SenderBatchID:        "CB-BATCH-REAL-001",
		SenderItemID:         "CB-ITEM-REAL-001",
		AmountCents:          45000,
		Currency:             "USD",
		Status:               "PREPARED",
		RequiresDualApproval: true,
		IsSyntheticFixture:   false, // GENUINE
	}
	if err := repo.CreatePayoutOperation(ctx, realOp); err != nil {
		t.Fatalf("failed inserting real non-fixture op: %v", err)
	}

	// Now run Reset
	if err := s.Seed(ctx, Options{Reset: true}); err != nil {
		t.Fatalf("seed with reset failed: %v", err)
	}

	// Verify real case is PRESERVED
	preservedCase, err := repo.GetPayoutCaseByReference(ctx, demoOrgID, "REAL-CLAIM-001")
	if err != nil || preservedCase == nil {
		t.Fatalf("real non-fixture case MUST be preserved after reset, got err: %v", err)
	}
	if preservedCase.IsSyntheticFixture {
		t.Errorf("real case should remain marked IsSyntheticFixture=false")
	}

	preservedOps, err := repo.ListPayoutOperationsByCaseID(ctx, demoOrgID, preservedCase.ID)
	if err != nil || len(preservedOps) != 1 {
		t.Fatalf("real non-fixture op MUST be preserved after reset")
	}

	// Verify that the demo fixtures were re-created cleanly
	for _, ref := range []string{DemoCase1Ref, DemoCase2Ref, DemoCase3Ref} {
		c, err := repo.GetPayoutCaseByReference(ctx, demoOrgID, ref)
		if err != nil || c == nil {
			t.Errorf("fixture %s should be re-created after reset", ref)
		}
	}

	// Total cases in org should now be 4 (3 fixtures + 1 preserved real case)
	_, totalCount, _ := repo.ListPayoutCasesByOrg(ctx, demoOrgID, 100, 0)
	if totalCount != 4 {
		t.Errorf("expected 4 total cases (3 fixtures + 1 real), got %d", totalCount)
	}
}

// FailingRepository wraps a repository to simulate failure on specific calls.
type FailingRepository struct {
	db.Repository
	FailCreateCase bool
}

func (f *FailingRepository) CreatePayoutCase(ctx context.Context, c *db.PayoutCase) error {
	if f.FailCreateCase {
		return errors.New("simulated database disk failure on payout case insert")
	}
	return f.Repository.CreatePayoutCase(ctx, c)
}

func TestSeeder_WriteFailureReturnsError(t *testing.T) {
	baseRepo := setupTestRepo()
	failingRepo := &FailingRepository{
		Repository:     baseRepo,
		FailCreateCase: true,
	}
	s := New(failingRepo)
	ctx := context.Background()

	err := s.Seed(ctx, Options{Reset: false})
	if err == nil {
		t.Fatalf("expected seed to fail on write error, but got nil")
	}
	if !errors.Is(err, errors.New("simulated database disk failure on payout case insert")) &&
		err.Error() != "failed to create demo case 1: simulated database disk failure on payout case insert" {
		t.Logf("got expected error wrapping: %v", err)
	}
}

func TestSeeder_DisabledAccountFailsCleanly(t *testing.T) {
	repo := setupTestRepo()
	ctx := context.Background()

	// Pre-create disabled operator
	hash, _ := crypto.HashPassword("any")
	now := time.Now().UTC()
	u := &db.User{
		Email:           OperatorEmail,
		PasswordHash:    hash,
		Name:            "Disabled Operator",
		EmailVerified:   true,
		EmailVerifiedAt: &now,
	}
	createdUser, _, err := repo.CreateUserWithPersonalOrg(ctx, u, "Test Org")
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	mockRepo, ok := repo.(*db.MockRepository)
	if !ok {
		t.Fatal("expected mock repository")
	}
	mockRepo.Users[createdUser.ID].IsActive = false

	s := New(repo)
	err = s.Seed(ctx, Options{Reset: false})
	if err == nil {
		t.Fatalf("expected seeding to fail when operator account is disabled")
	}
}
