package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository implements Repository using PostgreSQL with pgxpool.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository establishes a connection pool to PostgreSQL.
func NewPostgresRepository(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid database connection string: %w", err)
	}

	cfg.MaxConns = 25
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 1 * time.Hour
	cfg.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &PostgresRepository{pool: pool}, nil
}

func (r *PostgresRepository) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}

func (r *PostgresRepository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

func (r *PostgresRepository) CreateUser(ctx context.Context, user *User) (*User, error) {
	queryUser := `
		INSERT INTO users (email, password_hash, name, email_verified, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		RETURNING id, email, name, email_verified, is_active, created_at, updated_at
	`
	createdUser := &User{}
	err := r.pool.QueryRow(ctx, queryUser,
		user.Email,
		user.PasswordHash,
		user.Name,
		user.EmailVerified,
		user.IsActive,
	).Scan(
		&createdUser.ID,
		&createdUser.Email,
		&createdUser.Name,
		&createdUser.EmailVerified,
		&createdUser.IsActive,
		&createdUser.CreatedAt,
		&createdUser.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return nil, ErrUserAlreadyExists
		}
		return nil, fmt.Errorf("failed to insert user: %w", err)
	}
	return createdUser, nil
}

func (r *PostgresRepository) CreateUserWithPersonalOrg(ctx context.Context, user *User, orgName string) (*User, *Organization, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Insert User
	queryUser := `
		INSERT INTO users (email, password_hash, name, email_verified, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		RETURNING id, email, name, email_verified, is_active, created_at, updated_at
	`
	createdUser := &User{}
	err = tx.QueryRow(ctx, queryUser,
		user.Email,
		user.PasswordHash,
		user.Name,
		user.EmailVerified,
		user.IsActive,
	).Scan(
		&createdUser.ID,
		&createdUser.Email,
		&createdUser.Name,
		&createdUser.EmailVerified,
		&createdUser.IsActive,
		&createdUser.CreatedAt,
		&createdUser.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return nil, nil, ErrUserAlreadyExists
		}
		return nil, nil, fmt.Errorf("failed to insert user: %w", err)
	}

	// 2. Insert Personal Organization
	slug := generateSlug(orgName, createdUser.ID)
	queryOrg := `
		INSERT INTO organizations (name, slug, is_personal, created_at, updated_at)
		VALUES ($1, $2, TRUE, NOW(), NOW())
		RETURNING id, name, slug, is_personal, created_at, updated_at
	`
	createdOrg := &Organization{}
	err = tx.QueryRow(ctx, queryOrg, orgName, slug).Scan(
		&createdOrg.ID,
		&createdOrg.Name,
		&createdOrg.Slug,
		&createdOrg.IsPersonal,
		&createdOrg.CreatedAt,
		&createdOrg.UpdatedAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to insert organization: %w", err)
	}

	// 3. Insert Organization Membership (OWNER)
	queryMembership := `
		INSERT INTO organization_memberships (organization_id, user_id, role, created_at)
		VALUES ($1, $2, 'OWNER', NOW())
	`
	_, err = tx.Exec(ctx, queryMembership, createdOrg.ID, createdUser.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to insert organization membership: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to commit user creation: %w", err)
	}

	return createdUser, createdOrg, nil
}

func (r *PostgresRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	query := `
		SELECT id, email, password_hash, name, email_verified, email_verified_at, is_active, created_at, updated_at
		FROM users
		WHERE email = $1
	`
	u := &User{}
	err := r.pool.QueryRow(ctx, query, strings.ToLower(strings.TrimSpace(email))).Scan(
		&u.ID,
		&u.Email,
		&u.PasswordHash,
		&u.Name,
		&u.EmailVerified,
		&u.EmailVerifiedAt,
		&u.IsActive,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query user by email: %w", err)
	}
	return u, nil
}

func (r *PostgresRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
	query := `
		SELECT id, email, password_hash, name, email_verified, email_verified_at, is_active, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	u := &User{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&u.ID,
		&u.Email,
		&u.PasswordHash,
		&u.Name,
		&u.EmailVerified,
		&u.EmailVerifiedAt,
		&u.IsActive,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query user by id: %w", err)
	}
	return u, nil
}

func (r *PostgresRepository) UpdateUserPassword(ctx context.Context, userID, newPasswordHash string) error {
	query := `
		UPDATE users
		SET password_hash = $1, updated_at = NOW()
		WHERE id = $2
	`
	res, err := r.pool.Exec(ctx, query, newPasswordHash, userID)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (r *PostgresRepository) MarkEmailVerified(ctx context.Context, userID string) error {
	query := `
		UPDATE users
		SET email_verified = TRUE, email_verified_at = NOW(), updated_at = NOW()
		WHERE id = $1
	`
	res, err := r.pool.Exec(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("failed to mark email verified: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (r *PostgresRepository) CreateSession(ctx context.Context, session *Session) error {
	query := `
		INSERT INTO sessions (user_id, token_hash, active_organization_id, ip_address, user_agent, expires_at, created_at, last_active_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
		RETURNING id, created_at, last_active_at
	`
	return r.pool.QueryRow(ctx, query,
		session.UserID,
		session.TokenHash,
		session.ActiveOrganizationID,
		session.IPAddress,
		session.UserAgent,
		session.ExpiresAt,
	).Scan(&session.ID, &session.CreatedAt, &session.LastActiveAt)
}

func (r *PostgresRepository) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, *User, *Organization, error) {
	query := `
		SELECT 
			s.id, s.user_id, s.token_hash, s.active_organization_id, s.ip_address, s.user_agent, s.expires_at, s.created_at, s.last_active_at,
			u.id, u.email, u.name, u.email_verified, u.is_active,
			o.id, o.name, o.slug, o.is_personal
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		LEFT JOIN organizations o ON s.active_organization_id = o.id
		WHERE s.token_hash = $1 AND s.expires_at > NOW() AND u.is_active = TRUE
	`
	sess := &Session{}
	user := &User{}
	var orgID, orgName, orgSlug *string
	var orgIsPersonal *bool

	err := r.pool.QueryRow(ctx, query, tokenHash).Scan(
		&sess.ID,
		&sess.UserID,
		&sess.TokenHash,
		&sess.ActiveOrganizationID,
		&sess.IPAddress,
		&sess.UserAgent,
		&sess.ExpiresAt,
		&sess.CreatedAt,
		&sess.LastActiveAt,
		&user.ID,
		&user.Email,
		&user.Name,
		&user.EmailVerified,
		&user.IsActive,
		&orgID,
		&orgName,
		&orgSlug,
		&orgIsPersonal,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to query session: %w", err)
	}

	var org *Organization
	if orgID != nil && orgName != nil && orgSlug != nil && orgIsPersonal != nil {
		org = &Organization{
			ID:         *orgID,
			Name:       *orgName,
			Slug:       *orgSlug,
			IsPersonal: *orgIsPersonal,
		}
	}

	return sess, user, org, nil
}

func (r *PostgresRepository) TouchSession(ctx context.Context, sessionID string) error {
	query := `UPDATE sessions SET last_active_at = NOW() WHERE id = $1`
	_, err := r.pool.Exec(ctx, query, sessionID)
	return err
}

func (r *PostgresRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	query := `DELETE FROM sessions WHERE token_hash = $1`
	_, err := r.pool.Exec(ctx, query, tokenHash)
	return err
}

func (r *PostgresRepository) DeleteAllUserSessions(ctx context.Context, userID string) error {
	query := `DELETE FROM sessions WHERE user_id = $1`
	_, err := r.pool.Exec(ctx, query, userID)
	return err
}

func (r *PostgresRepository) CreateEmailVerificationToken(ctx context.Context, token *EmailVerificationToken) error {
	query := `
		INSERT INTO email_verification_tokens (user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, NOW())
		RETURNING id, created_at
	`
	return r.pool.QueryRow(ctx, query, token.UserID, token.TokenHash, token.ExpiresAt).Scan(&token.ID, &token.CreatedAt)
}

func (r *PostgresRepository) ConsumeEmailVerificationToken(ctx context.Context, tokenHash string) (*EmailVerificationToken, error) {
	query := `
		UPDATE email_verification_tokens
		SET consumed_at = NOW()
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > NOW()
		RETURNING id, user_id, token_hash, expires_at, consumed_at, created_at
	`
	tok := &EmailVerificationToken{}
	err := r.pool.QueryRow(ctx, query, tokenHash).Scan(
		&tok.ID,
		&tok.UserID,
		&tok.TokenHash,
		&tok.ExpiresAt,
		&tok.ConsumedAt,
		&tok.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenNotFoundOrUsed
	}
	if err != nil {
		return nil, fmt.Errorf("failed to consume verification token: %w", err)
	}
	return tok, nil
}

func (r *PostgresRepository) CreatePasswordResetToken(ctx context.Context, token *PasswordResetToken) error {
	query := `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, NOW())
		RETURNING id, created_at
	`
	return r.pool.QueryRow(ctx, query, token.UserID, token.TokenHash, token.ExpiresAt).Scan(&token.ID, &token.CreatedAt)
}

func (r *PostgresRepository) ConsumePasswordResetToken(ctx context.Context, tokenHash string) (*PasswordResetToken, error) {
	query := `
		UPDATE password_reset_tokens
		SET consumed_at = NOW()
		WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > NOW()
		RETURNING id, user_id, token_hash, expires_at, consumed_at, created_at
	`
	tok := &PasswordResetToken{}
	err := r.pool.QueryRow(ctx, query, tokenHash).Scan(
		&tok.ID,
		&tok.UserID,
		&tok.TokenHash,
		&tok.ExpiresAt,
		&tok.ConsumedAt,
		&tok.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrTokenNotFoundOrUsed
	}
	if err != nil {
		return nil, fmt.Errorf("failed to consume password reset token: %w", err)
	}
	return tok, nil
}

func (r *PostgresRepository) GetOrganizationMemberships(ctx context.Context, userID string) ([]OrganizationMembershipWithOrg, error) {
	query := `
		SELECT 
			m.id, m.organization_id, m.user_id, m.role, m.created_at,
			o.id, o.name, o.slug, o.is_personal, o.created_at, o.updated_at
		FROM organization_memberships m
		JOIN organizations o ON m.organization_id = o.id
		WHERE m.user_id = $1
		ORDER BY m.created_at ASC
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query memberships: %w", err)
	}
	defer rows.Close()

	var result []OrganizationMembershipWithOrg
	for rows.Next() {
		var item OrganizationMembershipWithOrg
		if err := rows.Scan(
			&item.Membership.ID,
			&item.Membership.OrganizationID,
			&item.Membership.UserID,
			&item.Membership.Role,
			&item.Membership.CreatedAt,
			&item.Organization.ID,
			&item.Organization.Name,
			&item.Organization.Slug,
			&item.Organization.IsPersonal,
			&item.Organization.CreatedAt,
			&item.Organization.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan membership row: %w", err)
		}
		result = append(result, item)
	}
	return result, nil
}

func (r *PostgresRepository) GetOrganizationByID(ctx context.Context, orgID string) (*Organization, error) {
	query := `SELECT id, name, slug, is_personal, created_at, updated_at FROM organizations WHERE id = $1`
	org := &Organization{}
	err := r.pool.QueryRow(ctx, query, orgID).Scan(
		&org.ID,
		&org.Name,
		&org.Slug,
		&org.IsPersonal,
		&org.CreatedAt,
		&org.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOrganizationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query organization: %w", err)
	}
	return org, nil
}

func (r *PostgresRepository) VerifyUserOrgAccess(ctx context.Context, userID, orgID string) (bool, string, error) {
	query := `SELECT role FROM organization_memberships WHERE user_id = $1 AND organization_id = $2`
	var role string
	err := r.pool.QueryRow(ctx, query, userID, orgID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", ErrAccessDenied
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to query membership role: %w", err)
	}
	return true, role, nil
}

func (r *PostgresRepository) AddOrganizationMember(ctx context.Context, orgID, userID, role string) error {
	query := `
		INSERT INTO organization_memberships (organization_id, user_id, role, created_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (organization_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`
	_, err := r.pool.Exec(ctx, query, orgID, userID, role)
	if err != nil {
		return fmt.Errorf("failed to add organization member: %w", err)
	}
	return nil
}

func (r *PostgresRepository) RecordAuditEvent(ctx context.Context, event *AuditEvent) error {
	metaJSON, err := json.Marshal(event.Metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	query := `
		INSERT INTO audit_events (user_id, organization_id, event_type, ip_address, user_agent, metadata, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
		RETURNING id, created_at
	`
	return r.pool.QueryRow(ctx, query,
		event.UserID,
		event.OrganizationID,
		event.EventType,
		event.IPAddress,
		event.UserAgent,
		metaJSON,
	).Scan(&event.ID, &event.CreatedAt)
}

func generateSlug(name, suffixID string) string {
	clean := strings.ToLower(strings.TrimSpace(name))
	clean = strings.ReplaceAll(clean, " ", "-")
	clean = strings.ReplaceAll(clean, "_", "-")
	if len(suffixID) > 8 {
		suffixID = suffixID[:8]
	}
	return fmt.Sprintf("%s-%s", clean, suffixID)
}

// Payout Cases

func (r *PostgresRepository) CreatePayoutCase(ctx context.Context, c *PayoutCase) error {
	query := `
		INSERT INTO payout_cases (
			organization_id, created_by_user_id, reference, recipient_email, recipient_name,
			title, description, currency, amount_cents, status, risk_score, dual_approval_required,
			is_synthetic_fixture, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW())
		RETURNING id, created_at, updated_at
	`
	err := r.pool.QueryRow(ctx, query,
		c.OrganizationID,
		c.CreatedByUserID,
		c.Reference,
		strings.ToLower(strings.TrimSpace(c.RecipientEmail)),
		c.RecipientName,
		c.Title,
		c.Description,
		strings.ToUpper(c.Currency),
		c.AmountCents,
		c.Status,
		c.RiskScore,
		c.DualApprovalRequired,
		c.IsSyntheticFixture,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return errors.New("a payout case with this reference already exists in your organization")
		}
		return fmt.Errorf("failed to create payout case: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetPayoutCaseByID(ctx context.Context, orgID, caseID string) (*PayoutCase, error) {
	query := `
		SELECT id, organization_id, created_by_user_id, reference, recipient_email, recipient_name,
		       title, description, currency, amount_cents, status, risk_score, dual_approval_required,
		       is_synthetic_fixture, created_at, updated_at
		FROM payout_cases
		WHERE id = $1 AND organization_id = $2
	`
	var c PayoutCase
	err := r.pool.QueryRow(ctx, query, caseID, orgID).Scan(
		&c.ID, &c.OrganizationID, &c.CreatedByUserID, &c.Reference, &c.RecipientEmail, &c.RecipientName,
		&c.Title, &c.Description, &c.Currency, &c.AmountCents, &c.Status, &c.RiskScore, &c.DualApprovalRequired,
		&c.IsSyntheticFixture, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPayoutCaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get payout case: %w", err)
	}
	return &c, nil
}

func (r *PostgresRepository) GetPayoutCaseByReference(ctx context.Context, orgID, reference string) (*PayoutCase, error) {
	query := `
		SELECT id, organization_id, created_by_user_id, reference, recipient_email, recipient_name,
		       title, description, currency, amount_cents, status, risk_score, dual_approval_required,
		       is_synthetic_fixture, created_at, updated_at
		FROM payout_cases
		WHERE organization_id = $1 AND reference = $2
	`
	var c PayoutCase
	err := r.pool.QueryRow(ctx, query, orgID, reference).Scan(
		&c.ID, &c.OrganizationID, &c.CreatedByUserID, &c.Reference, &c.RecipientEmail, &c.RecipientName,
		&c.Title, &c.Description, &c.Currency, &c.AmountCents, &c.Status, &c.RiskScore, &c.DualApprovalRequired,
		&c.IsSyntheticFixture, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPayoutCaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get payout case by reference: %w", err)
	}
	return &c, nil
}

func (r *PostgresRepository) ListPayoutCasesByOrg(ctx context.Context, orgID string, limit, offset int) ([]PayoutCase, int, error) {
	countQuery := `SELECT COUNT(*) FROM payout_cases WHERE organization_id = $1`
	var total int
	if err := r.pool.QueryRow(ctx, countQuery, orgID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count payout cases: %w", err)
	}

	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, organization_id, created_by_user_id, reference, recipient_email, recipient_name,
		       title, description, currency, amount_cents, status, risk_score, dual_approval_required,
		       is_synthetic_fixture, created_at, updated_at
		FROM payout_cases
		WHERE organization_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, orgID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query payout cases: %w", err)
	}
	defer rows.Close()

	cases := make([]PayoutCase, 0)
	for rows.Next() {
		var c PayoutCase
		err := rows.Scan(
			&c.ID, &c.OrganizationID, &c.CreatedByUserID, &c.Reference, &c.RecipientEmail, &c.RecipientName,
			&c.Title, &c.Description, &c.Currency, &c.AmountCents, &c.Status, &c.RiskScore, &c.DualApprovalRequired,
			&c.IsSyntheticFixture, &c.CreatedAt, &c.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan payout case: %w", err)
		}
		cases = append(cases, c)
	}
	return cases, total, nil
}

func (r *PostgresRepository) UpdatePayoutCaseStatus(ctx context.Context, orgID, caseID, status string) error {
	query := `
		UPDATE payout_cases
		SET status = $1, updated_at = NOW()
		WHERE id = $2 AND organization_id = $3
	`
	res, err := r.pool.Exec(ctx, query, status, caseID, orgID)
	if err != nil {
		return fmt.Errorf("failed to update payout case status: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrPayoutCaseNotFound
	}
	return nil
}

// Payout Operations

func (r *PostgresRepository) CreatePayoutOperation(ctx context.Context, op *PayoutOperation) error {
	var rawJSON []byte
	if op.RawProviderResponse != nil {
		rawJSON, _ = json.Marshal(op.RawProviderResponse)
	}

	query := `
		INSERT INTO payout_operations (
			payout_case_id, organization_id, initiated_by_user_id, sender_batch_id, sender_item_id,
			amount_cents, currency, status, paypal_payout_batch_id, paypal_batch_status,
			paypal_payout_item_id, paypal_item_status, error_message, raw_provider_response,
			requires_dual_approval, approved_by_user_id, approved_at, submitted_at,
			completed_at, cancelled_at, is_synthetic_fixture, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, NOW(), NOW()
		)
		RETURNING id, created_at, updated_at
	`
	err := r.pool.QueryRow(ctx, query,
		op.PayoutCaseID,
		op.OrganizationID,
		op.InitiatedByUserID,
		op.SenderBatchID,
		op.SenderItemID,
		op.AmountCents,
		op.Currency,
		op.Status,
		op.PayPalPayoutBatchID,
		op.PayPalBatchStatus,
		op.PayPalPayoutItemID,
		op.PayPalItemStatus,
		op.ErrorMessage,
		rawJSON,
		op.RequiresDualApproval,
		op.ApprovedByUserID,
		op.ApprovedAt,
		op.SubmittedAt,
		op.CompletedAt,
		op.CancelledAt,
		op.IsSyntheticFixture,
	).Scan(&op.ID, &op.CreatedAt, &op.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create payout operation: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetPayoutOperationByID(ctx context.Context, orgID, opID string) (*PayoutOperation, error) {
	query := `
		SELECT id, payout_case_id, organization_id, initiated_by_user_id, sender_batch_id, sender_item_id,
		       amount_cents, currency, status, paypal_payout_batch_id, paypal_batch_status,
		       paypal_payout_item_id, paypal_item_status, error_message, raw_provider_response,
		       requires_dual_approval, approved_by_user_id, approved_at, submitted_at, completed_at,
		       cancelled_at, is_synthetic_fixture, created_at, updated_at
		FROM payout_operations
		WHERE id = $1 AND organization_id = $2
	`
	var op PayoutOperation
	var rawJSON []byte
	err := r.pool.QueryRow(ctx, query, opID, orgID).Scan(
		&op.ID, &op.PayoutCaseID, &op.OrganizationID, &op.InitiatedByUserID, &op.SenderBatchID, &op.SenderItemID,
		&op.AmountCents, &op.Currency, &op.Status, &op.PayPalPayoutBatchID, &op.PayPalBatchStatus,
		&op.PayPalPayoutItemID, &op.PayPalItemStatus, &op.ErrorMessage, &rawJSON,
		&op.RequiresDualApproval, &op.ApprovedByUserID, &op.ApprovedAt, &op.SubmittedAt, &op.CompletedAt,
		&op.CancelledAt, &op.IsSyntheticFixture, &op.CreatedAt, &op.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPayoutOperationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get payout operation: %w", err)
	}
	if len(rawJSON) > 0 {
		_ = json.Unmarshal(rawJSON, &op.RawProviderResponse)
	}
	return &op, nil
}

func (r *PostgresRepository) GetPayoutOperationBySenderBatchID(ctx context.Context, senderBatchID string) (*PayoutOperation, error) {
	query := `
		SELECT id, payout_case_id, organization_id, initiated_by_user_id, sender_batch_id, sender_item_id,
		       amount_cents, currency, status, paypal_payout_batch_id, paypal_batch_status,
		       paypal_payout_item_id, paypal_item_status, error_message, raw_provider_response,
		       requires_dual_approval, approved_by_user_id, approved_at, submitted_at, completed_at,
		       cancelled_at, is_synthetic_fixture, created_at, updated_at
		FROM payout_operations
		WHERE sender_batch_id = $1
	`
	var op PayoutOperation
	var rawJSON []byte
	err := r.pool.QueryRow(ctx, query, senderBatchID).Scan(
		&op.ID, &op.PayoutCaseID, &op.OrganizationID, &op.InitiatedByUserID, &op.SenderBatchID, &op.SenderItemID,
		&op.AmountCents, &op.Currency, &op.Status, &op.PayPalPayoutBatchID, &op.PayPalBatchStatus,
		&op.PayPalPayoutItemID, &op.PayPalItemStatus, &op.ErrorMessage, &rawJSON,
		&op.RequiresDualApproval, &op.ApprovedByUserID, &op.ApprovedAt, &op.SubmittedAt, &op.CompletedAt,
		&op.CancelledAt, &op.IsSyntheticFixture, &op.CreatedAt, &op.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPayoutOperationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get payout operation by batch id: %w", err)
	}
	if len(rawJSON) > 0 {
		_ = json.Unmarshal(rawJSON, &op.RawProviderResponse)
	}
	return &op, nil
}

func (r *PostgresRepository) ListPayoutOperationsByCaseID(ctx context.Context, orgID, caseID string) ([]PayoutOperation, error) {
	query := `
		SELECT id, payout_case_id, organization_id, initiated_by_user_id, sender_batch_id, sender_item_id,
		       amount_cents, currency, status, paypal_payout_batch_id, paypal_batch_status,
		       paypal_payout_item_id, paypal_item_status, error_message, raw_provider_response,
		       requires_dual_approval, approved_by_user_id, approved_at, submitted_at, completed_at,
		       cancelled_at, is_synthetic_fixture, created_at, updated_at
		FROM payout_operations
		WHERE payout_case_id = $1 AND organization_id = $2
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, caseID, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to list payout operations: %w", err)
	}
	defer rows.Close()

	var ops []PayoutOperation
	for rows.Next() {
		var op PayoutOperation
		var rawJSON []byte
		err := rows.Scan(
			&op.ID, &op.PayoutCaseID, &op.OrganizationID, &op.InitiatedByUserID, &op.SenderBatchID, &op.SenderItemID,
			&op.AmountCents, &op.Currency, &op.Status, &op.PayPalPayoutBatchID, &op.PayPalBatchStatus,
			&op.PayPalPayoutItemID, &op.PayPalItemStatus, &op.ErrorMessage, &rawJSON,
			&op.RequiresDualApproval, &op.ApprovedByUserID, &op.ApprovedAt, &op.SubmittedAt, &op.CompletedAt,
			&op.CancelledAt, &op.IsSyntheticFixture, &op.CreatedAt, &op.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan payout operation: %w", err)
		}
		if len(rawJSON) > 0 {
			_ = json.Unmarshal(rawJSON, &op.RawProviderResponse)
		}
		ops = append(ops, op)
	}
	return ops, nil
}

func (r *PostgresRepository) UpdatePayoutOperation(ctx context.Context, op *PayoutOperation) error {
	var rawJSON []byte
	if op.RawProviderResponse != nil {
		rawJSON, _ = json.Marshal(op.RawProviderResponse)
	}

	query := `
		UPDATE payout_operations
		SET status = $1, paypal_payout_batch_id = $2, paypal_batch_status = $3,
		    paypal_payout_item_id = $4, paypal_item_status = $5, error_message = $6,
		    raw_provider_response = $7, submitted_at = $8, completed_at = $9,
		    cancelled_at = $10, updated_at = NOW()
		WHERE id = $11 AND organization_id = $12
	`
	res, err := r.pool.Exec(ctx, query,
		op.Status,
		op.PayPalPayoutBatchID,
		op.PayPalBatchStatus,
		op.PayPalPayoutItemID,
		op.PayPalItemStatus,
		op.ErrorMessage,
		rawJSON,
		op.SubmittedAt,
		op.CompletedAt,
		op.CancelledAt,
		op.ID,
		op.OrganizationID,
	)
	if err != nil {
		return fmt.Errorf("failed to update payout operation: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrPayoutOperationNotFound
	}
	return nil
}

func (r *PostgresRepository) ApprovePayoutOperation(ctx context.Context, orgID, opID, approvedByUserID string) error {
	// First fetch operation to verify initiator is different (no self-approval)
	checkQuery := `SELECT initiated_by_user_id, requires_dual_approval FROM payout_operations WHERE id = $1 AND organization_id = $2`
	var initiatorID string
	var requiresDual bool
	err := r.pool.QueryRow(ctx, checkQuery, opID, orgID).Scan(&initiatorID, &requiresDual)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPayoutOperationNotFound
	}
	if err != nil {
		return fmt.Errorf("failed to verify operation for approval: %w", err)
	}

	if requiresDual && initiatorID == approvedByUserID {
		return ErrSelfApprovalForbidden
	}

	updateQuery := `
		UPDATE payout_operations
		SET status = 'APPROVED', approved_by_user_id = $1, approved_at = NOW(), updated_at = NOW()
		WHERE id = $2 AND organization_id = $3
	`
	res, err := r.pool.Exec(ctx, updateQuery, approvedByUserID, opID, orgID)
	if err != nil {
		return fmt.Errorf("failed to approve payout operation: %w", err)
	}
	if res.RowsAffected() == 0 {
		return ErrPayoutOperationNotFound
	}
	return nil
}

// Status Snapshots

func (r *PostgresRepository) CreatePayoutStatusSnapshot(ctx context.Context, snapshot *PayoutStatusSnapshot) error {
	rawJSON, _ := json.Marshal(snapshot.RawResponse)
	query := `
		INSERT INTO payout_status_snapshots (
			payout_operation_id, organization_id, provider, batch_status, item_status, raw_response, is_synthetic_fixture, recorded_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		RETURNING id, recorded_at
	`
	return r.pool.QueryRow(ctx, query,
		snapshot.PayoutOperationID,
		snapshot.OrganizationID,
		snapshot.Provider,
		snapshot.BatchStatus,
		snapshot.ItemStatus,
		rawJSON,
		snapshot.IsSyntheticFixture,
	).Scan(&snapshot.ID, &snapshot.RecordedAt)
}

func (r *PostgresRepository) ListPayoutStatusSnapshots(ctx context.Context, orgID, opID string) ([]PayoutStatusSnapshot, error) {
	query := `
		SELECT id, payout_operation_id, organization_id, provider, batch_status, item_status, raw_response, is_synthetic_fixture, recorded_at
		FROM payout_status_snapshots
		WHERE payout_operation_id = $1 AND organization_id = $2
		ORDER BY recorded_at DESC
	`
	rows, err := r.pool.Query(ctx, query, opID, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to list payout status snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []PayoutStatusSnapshot
	for rows.Next() {
		var s PayoutStatusSnapshot
		var rawJSON []byte
		err := rows.Scan(
			&s.ID, &s.PayoutOperationID, &s.OrganizationID, &s.Provider,
			&s.BatchStatus, &s.ItemStatus, &rawJSON, &s.IsSyntheticFixture, &s.RecordedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan payout snapshot: %w", err)
		}
		if len(rawJSON) > 0 {
			_ = json.Unmarshal(rawJSON, &s.RawResponse)
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, nil
}

func (r *PostgresRepository) DeleteSyntheticFixtures(ctx context.Context, orgID string) (int64, error) {
	var query string
	var args []interface{}
	if orgID != "" {
		query = `DELETE FROM payout_cases WHERE organization_id = $1 AND is_synthetic_fixture = true`
		args = []interface{}{orgID}
	} else {
		query = `DELETE FROM payout_cases WHERE is_synthetic_fixture = true`
	}
	res, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to delete synthetic fixtures: %w", err)
	}
	return res.RowsAffected(), nil
}

// Investigations

func (r *PostgresRepository) CreateInvestigation(ctx context.Context, inv *PayoutInvestigation) error {
	observedFactsJSON, _ := json.Marshal(inv.ObservedFacts)
	findingsJSON, _ := json.Marshal(inv.Findings)
	recommendedJSON, _ := json.Marshal(inv.RecommendedNextSteps)
	missingInfoJSON, _ := json.Marshal(inv.MissingInformation)
	riskFlagsJSON, _ := json.Marshal(inv.RiskFlags)
	limitationsJSON, _ := json.Marshal(inv.Limitations)
	rawRespJSON, _ := json.Marshal(inv.RawResponse)

	query := `
		INSERT INTO payout_investigations (
			payout_case_id, organization_id, requested_by_user_id,
			provider, model, prompt_version, status, summary,
			confidence, requires_human_review, observed_facts, findings,
			recommended_next_steps, missing_information, risk_flags, limitations,
			raw_response, error_message, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, NOW()
		)
		RETURNING id, created_at
	`
	return r.pool.QueryRow(ctx, query,
		inv.PayoutCaseID, inv.OrganizationID, inv.RequestedByUserID,
		inv.Provider, inv.Model, inv.PromptVersion, inv.Status, inv.Summary,
		inv.Confidence, inv.RequiresHumanReview, observedFactsJSON, findingsJSON,
		recommendedJSON, missingInfoJSON, riskFlagsJSON, limitationsJSON,
		rawRespJSON, inv.ErrorMessage,
	).Scan(&inv.ID, &inv.CreatedAt)
}

func (r *PostgresRepository) GetInvestigationByID(ctx context.Context, orgID, investigationID string) (*PayoutInvestigation, error) {
	query := `
		SELECT id, payout_case_id, organization_id, requested_by_user_id,
		       provider, model, prompt_version, status, summary,
		       confidence, requires_human_review, observed_facts, findings,
		       recommended_next_steps, missing_information, risk_flags, limitations,
		       raw_response, error_message, created_at
		FROM payout_investigations
		WHERE id = $1 AND organization_id = $2
	`
	var inv PayoutInvestigation
	var observedFactsJSON, findingsJSON, recommendedJSON, missingInfoJSON, riskFlagsJSON, limitationsJSON, rawRespJSON []byte

	err := r.pool.QueryRow(ctx, query, investigationID, orgID).Scan(
		&inv.ID, &inv.PayoutCaseID, &inv.OrganizationID, &inv.RequestedByUserID,
		&inv.Provider, &inv.Model, &inv.PromptVersion, &inv.Status, &inv.Summary,
		&inv.Confidence, &inv.RequiresHumanReview, &observedFactsJSON, &findingsJSON,
		&recommendedJSON, &missingInfoJSON, &riskFlagsJSON, &limitationsJSON,
		&rawRespJSON, &inv.ErrorMessage, &inv.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvestigationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get investigation: %w", err)
	}

	_ = json.Unmarshal(observedFactsJSON, &inv.ObservedFacts)
	_ = json.Unmarshal(findingsJSON, &inv.Findings)
	_ = json.Unmarshal(recommendedJSON, &inv.RecommendedNextSteps)
	_ = json.Unmarshal(missingInfoJSON, &inv.MissingInformation)
	_ = json.Unmarshal(riskFlagsJSON, &inv.RiskFlags)
	_ = json.Unmarshal(limitationsJSON, &inv.Limitations)
	if len(rawRespJSON) > 0 {
		_ = json.Unmarshal(rawRespJSON, &inv.RawResponse)
	}
	return &inv, nil
}

func (r *PostgresRepository) ListInvestigationsByCaseID(ctx context.Context, orgID, caseID string) ([]PayoutInvestigation, error) {
	query := `
		SELECT id, payout_case_id, organization_id, requested_by_user_id,
		       provider, model, prompt_version, status, summary,
		       confidence, requires_human_review, observed_facts, findings,
		       recommended_next_steps, missing_information, risk_flags, limitations,
		       raw_response, error_message, created_at
		FROM payout_investigations
		WHERE payout_case_id = $1 AND organization_id = $2
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, caseID, orgID)
	if err != nil {
		return nil, fmt.Errorf("failed to list investigations: %w", err)
	}
	defer rows.Close()

	var list []PayoutInvestigation
	for rows.Next() {
		var inv PayoutInvestigation
		var observedFactsJSON, findingsJSON, recommendedJSON, missingInfoJSON, riskFlagsJSON, limitationsJSON, rawRespJSON []byte

		err := rows.Scan(
			&inv.ID, &inv.PayoutCaseID, &inv.OrganizationID, &inv.RequestedByUserID,
			&inv.Provider, &inv.Model, &inv.PromptVersion, &inv.Status, &inv.Summary,
			&inv.Confidence, &inv.RequiresHumanReview, &observedFactsJSON, &findingsJSON,
			&recommendedJSON, &missingInfoJSON, &riskFlagsJSON, &limitationsJSON,
			&rawRespJSON, &inv.ErrorMessage, &inv.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan investigation: %w", err)
		}

		_ = json.Unmarshal(observedFactsJSON, &inv.ObservedFacts)
		_ = json.Unmarshal(findingsJSON, &inv.Findings)
		_ = json.Unmarshal(recommendedJSON, &inv.RecommendedNextSteps)
		_ = json.Unmarshal(missingInfoJSON, &inv.MissingInformation)
		_ = json.Unmarshal(riskFlagsJSON, &inv.RiskFlags)
		_ = json.Unmarshal(limitationsJSON, &inv.Limitations)
		if len(rawRespJSON) > 0 {
			_ = json.Unmarshal(rawRespJSON, &inv.RawResponse)
		}
		list = append(list, inv)
	}
	return list, nil
}
