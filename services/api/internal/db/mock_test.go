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
