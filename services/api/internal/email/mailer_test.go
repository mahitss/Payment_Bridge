package email

import (
	"context"
	"errors"
	"testing"
)

func TestMockMailer_VerificationAndReset(t *testing.T) {
	mailer := NewMockMailer()
	ctx := context.Background()

	// 1. Send verification email
	err := mailer.SendVerificationEmail(ctx, "user@example.com", "Alice User", "http://localhost:3000/verify-email?token=xyz")
	if err != nil {
		t.Fatalf("unexpected error sending verification email: %v", err)
	}

	last := mailer.GetLastMail()
	if last == nil {
		t.Fatal("expected mail to be recorded")
	}
	if last.To != "user@example.com" || last.Type != "verification" {
		t.Errorf("unexpected mail payload: %+v", last)
	}

	// 2. Send password reset email
	err = mailer.SendPasswordResetEmail(ctx, "user@example.com", "Alice User", "http://localhost:3000/reset-password?token=abc")
	if err != nil {
		t.Fatalf("unexpected error sending reset email: %v", err)
	}

	last = mailer.GetLastMail()
	if last.Type != "password_reset" || last.URL != "http://localhost:3000/reset-password?token=abc" {
		t.Errorf("unexpected reset mail: %+v", last)
	}
}

func TestMockMailer_DeliveryFailure(t *testing.T) {
	mailer := NewMockMailer()
	mailer.FailNextCall = true

	err := mailer.SendVerificationEmail(context.Background(), "user@example.com", "Alice", "http://localhost:3000")
	if err == nil {
		t.Fatal("expected error when FailNextCall is true, got nil")
	}
	if !errors.Is(err, ErrSMTPUnavailable) {
		t.Errorf("expected ErrSMTPUnavailable, got: %v", err)
	}
}

func TestSMTPMailer_InvalidEmailValidation(t *testing.T) {
	mailer := NewSMTPMailer("localhost", 1025, "noreply@claimbridge.local", "", "")

	err := mailer.SendVerificationEmail(context.Background(), "not-an-email", "Bob", "http://localhost:3000")
	if !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("expected ErrInvalidEmail, got: %v", err)
	}
}
