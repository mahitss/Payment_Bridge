package seeder_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"claimbridge/services/api/internal/crypto"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/seeder"
)

// TestSeeder_PostgresIntegration tests seeder idempotency, membership verification,
// and safe scoped reset against a real disposable PostgreSQL database.
func TestSeeder_PostgresIntegration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = os.Getenv("DATABASE_URL")
	}
	if dbURL == "" {
		t.Skip("skipping postgresql seeder test: TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	repo, err := db.NewPostgresRepository(ctx, dbURL)
	if err != nil {
		t.Skipf("skipping postgresql seeder test: unable to connect to %s: %v", dbURL, err)
	}
	defer repo.Close()

	// 1. Run migrations to ensure schema (including 000005_fixture_provenance) is up to date
	if err := repo.RunMigrations(ctx); err != nil {
		t.Fatalf("failed applying database migrations: %v", err)
	}

	// 2. Pre-create a non-fixture case and organization to verify it survives fixture reset
	passHash, _ := crypto.HashPassword("RealUserPass123!")
	nonFixtureUser := &db.User{
		Email:         fmt.Sprintf("real.user.%d@production.internal", time.Now().UnixNano()),
		PasswordHash:  passHash,
		Name:          "Genuine Operator",
		EmailVerified: true,
		IsActive:      true,
	}
	createdUser, nonFixtureOrg, err := repo.CreateUserWithPersonalOrg(ctx, nonFixtureUser, "Genuine Claims Org")
	if err != nil {
		t.Fatalf("failed to create non-fixture user and org: %v", err)
	}

	nonFixtureCase := &db.PayoutCase{
		OrganizationID:       nonFixtureOrg.ID,
		CreatedByUserID:      createdUser.ID,
		Reference:            fmt.Sprintf("REAL-CASE-%d", time.Now().UnixNano()),
		RecipientEmail:       "claimant.real@example.com",
		RecipientName:        "Real Claimant",
		Title:                "Genuine Customer Warranty Disbursement",
		Description:          "Non-fixture claim that must survive demo fixture resets.",
		Currency:             "USD",
		AmountCents:          18500,
		Status:               "DRAFT",
		RiskScore:            12.5,
		DualApprovalRequired: false,
		IsSyntheticFixture:   false, // GENUINE NON-FIXTURE
	}
	if err := repo.CreatePayoutCase(ctx, nonFixtureCase); err != nil {
		t.Fatalf("failed to create non-fixture case: %v", err)
	}

	// 3. Execution 1: Seed fixtures
	s := seeder.New(repo)
	if err := s.Seed(ctx, seeder.Options{Reset: false}); err != nil {
		t.Fatalf("first seed execution failed against postgres: %v", err)
	}

	// Retrieve demo organization
	demoUser, err := repo.GetUserByEmail(ctx, seeder.OperatorEmail)
	if err != nil {
		t.Fatalf("failed to retrieve demo operator user: %v", err)
	}
	if !demoUser.IsActive || !demoUser.EmailVerified {
		t.Fatalf("demo operator must be active and verified")
	}

	memberships, err := repo.GetOrganizationMemberships(ctx, demoUser.ID)
	if err != nil || len(memberships) == 0 {
		t.Fatalf("failed to retrieve operator organization memberships: %v", err)
	}
	demoOrgID := memberships[0].Organization.ID

	// Verify officer membership in demoOrg
	officerUser, err := repo.GetUserByEmail(ctx, seeder.OfficerEmail)
	if err != nil {
		t.Fatalf("failed to retrieve demo officer user: %v", err)
	}
	hasAccess, role, err := repo.VerifyUserOrgAccess(ctx, officerUser.ID, demoOrgID)
	if err != nil || !hasAccess {
		t.Fatalf("demo officer must have access to demoOrg: access=%v, err=%v", hasAccess, err)
	}
	if role != "ADMIN" {
		t.Fatalf("expected demo officer role ADMIN in demoOrg, got: %s", role)
	}

	// Verify all 3 cases were created
	casesRun1, totalRun1, err := repo.ListPayoutCasesByOrg(ctx, demoOrgID, 100, 0)
	if err != nil {
		t.Fatalf("failed to list payout cases: %v", err)
	}
	if totalRun1 != 3 || len(casesRun1) != 3 {
		t.Fatalf("expected exactly 3 demo cases on run 1, got total=%d, len=%d", totalRun1, len(casesRun1))
	}
	for _, c := range casesRun1 {
		if !c.IsSyntheticFixture {
			t.Errorf("expected demo case %s to have IsSyntheticFixture=true", c.Reference)
		}
	}

	// 4. Execution 2: Repeated seed execution must be 100% idempotent
	if err := s.Seed(ctx, seeder.Options{Reset: false}); err != nil {
		t.Fatalf("second seed execution failed against postgres: %v", err)
	}

	casesRun2, totalRun2, err := repo.ListPayoutCasesByOrg(ctx, demoOrgID, 100, 0)
	if err != nil {
		t.Fatalf("failed to list payout cases after second run: %v", err)
	}
	if totalRun2 != 3 || len(casesRun2) != 3 {
		t.Fatalf("expected STILL exactly 3 demo cases after run 2 (no duplicates), got total=%d, len=%d", totalRun2, len(casesRun2))
	}

	// 5. Execution 3: Reset must purge ONLY synthetic fixtures, preserving non-demo data
	if err := s.Seed(ctx, seeder.Options{Reset: true}); err != nil {
		t.Fatalf("reset seed execution failed against postgres: %v", err)
	}

	// After reset, demoOrg should have 3 re-created fixtures
	casesAfterReset, totalAfterReset, err := repo.ListPayoutCasesByOrg(ctx, demoOrgID, 100, 0)
	if err != nil {
		t.Fatalf("failed to list payout cases after reset: %v", err)
	}
	if totalAfterReset != 3 || len(casesAfterReset) != 3 {
		t.Fatalf("expected exactly 3 demo cases re-seeded after reset, got total=%d", totalAfterReset)
	}

	// CRITICAL TEST: Verify the non-fixture case in the other org SURVIVED completely intact
	survivingCase, err := repo.GetPayoutCaseByID(ctx, nonFixtureOrg.ID, nonFixtureCase.ID)
	if err != nil {
		t.Fatalf("non-fixture case was erroneously deleted during demo reset: %v", err)
	}
	if survivingCase.Reference != nonFixtureCase.Reference {
		t.Errorf("surviving case reference mismatch: %s vs %s", survivingCase.Reference, nonFixtureCase.Reference)
	}
	if survivingCase.IsSyntheticFixture {
		t.Errorf("non-fixture case was erroneously marked as synthetic fixture")
	}

	// 6. Test direct DeleteSyntheticFixtures preserves non-demo cases in the same org
	deletedCount, err := repo.DeleteSyntheticFixtures(ctx, demoOrgID)
	if err != nil {
		t.Fatalf("DeleteSyntheticFixtures failed: %v", err)
	}
	if deletedCount != 3 {
		t.Fatalf("expected 3 synthetic cases deleted, got: %d", deletedCount)
	}

	// Verify demoOrg cases count is 0
	casesPurged, totalPurged, err := repo.ListPayoutCasesByOrg(ctx, demoOrgID, 100, 0)
	if err != nil {
		t.Fatalf("failed to list payout cases after purge: %v", err)
	}
	if totalPurged != 0 || len(casesPurged) != 0 {
		t.Fatalf("expected 0 cases in demoOrg after explicit purge, got: %d", totalPurged)
	}

	// Verify non-fixture case still survives
	_, err = repo.GetPayoutCaseByID(ctx, nonFixtureOrg.ID, nonFixtureCase.ID)
	if err != nil {
		t.Fatalf("non-fixture case failed to survive direct DeleteSyntheticFixtures: %v", err)
	}

	// Re-seed demo cases so database is left in a clean seeded state
	if err := s.Seed(ctx, seeder.Options{Reset: false}); err != nil {
		t.Fatalf("final re-seeding failed: %v", err)
	}
}
