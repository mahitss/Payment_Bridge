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
