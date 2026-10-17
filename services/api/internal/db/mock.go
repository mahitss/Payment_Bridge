package db

import (
	"context"
	"strings"
	"sync"
	"time"

	"claimbridge/services/api/internal/crypto"
)

// MockRepository provides an in-memory implementation of Repository for automated tests.
type MockRepository struct {
	mu            sync.RWMutex
	Users         map[string]*User                   // id -> User
	UsersByEmail  map[string]*User                   // lowercase email -> User
	Orgs          map[string]*Organization           // id -> Org
	Memberships   map[string]*OrganizationMembership // id -> Membership
	Sessions      map[string]*Session                // token_hash -> Session
	VerifyTokens  map[string]*EmailVerificationToken // token_hash -> Token
	ResetTokens   map[string]*PasswordResetToken     // token_hash -> Token
	AuditEvents   []*AuditEvent
	FailNextOp    bool
}

func NewMockRepository() *MockRepository {
	return &MockRepository{
		Users:        make(map[string]*User),
		UsersByEmail: make(map[string]*User),
		Orgs:         make(map[string]*Organization),
		Memberships:  make(map[string]*OrganizationMembership),
		Sessions:     make(map[string]*Session),
		VerifyTokens: make(map[string]*EmailVerificationToken),
		ResetTokens:  make(map[string]*PasswordResetToken),
		AuditEvents:  make([]*AuditEvent, 0),
	}
}

func (m *MockRepository) Ping(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.FailNextOp {
		return ErrUserNotFound
	}
	return nil
}

func (m *MockRepository) CreateUserWithPersonalOrg(ctx context.Context, user *User, orgName string) (*User, *Organization, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	normEmail := strings.ToLower(strings.TrimSpace(user.Email))
	if _, exists := m.UsersByEmail[normEmail]; exists {
		return nil, nil, ErrUserAlreadyExists
	}

	now := time.Now().UTC()
	userID := crypto.GenerateUUID()
	createdUser := &User{
		ID:            userID,
		Email:         normEmail,
		PasswordHash:  user.PasswordHash,
		Name:          user.Name,
		EmailVerified: user.EmailVerified,
		IsActive:      true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	m.Users[userID] = createdUser
	m.UsersByEmail[normEmail] = createdUser

	orgID := crypto.GenerateUUID()
	createdOrg := &Organization{
		ID:         orgID,
		Name:       orgName,
		Slug:       generateSlug(orgName, userID),
		IsPersonal: true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	m.Orgs[orgID] = createdOrg

	membershipID := crypto.GenerateUUID()
	createdMembership := &OrganizationMembership{
		ID:             membershipID,
		OrganizationID: orgID,
		UserID:         userID,
		Role:           "OWNER",
		CreatedAt:      now,
	}
	m.Memberships[membershipID] = createdMembership

	return createdUser, createdOrg, nil
}

func (m *MockRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	norm := strings.ToLower(strings.TrimSpace(email))
	u, ok := m.UsersByEmail[norm]
	if !ok {
		return nil, ErrUserNotFound
	}
	copyUser := *u
	return &copyUser, nil
}

func (m *MockRepository) GetUserByID(ctx context.Context, id string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	u, ok := m.Users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	copyUser := *u
	return &copyUser, nil
}

func (m *MockRepository) UpdateUserPassword(ctx context.Context, userID, newPasswordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.Users[userID]
	if !ok {
		return ErrUserNotFound
	}
	u.PasswordHash = newPasswordHash
	u.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *MockRepository) MarkEmailVerified(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.Users[userID]
	if !ok {
		return ErrUserNotFound
	}
	now := time.Now().UTC()
	u.EmailVerified = true
	u.EmailVerifiedAt = &now
	u.UpdatedAt = now
	return nil
}

func (m *MockRepository) CreateSession(ctx context.Context, session *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session.ID = crypto.GenerateUUID()
	session.CreatedAt = time.Now().UTC()
	session.LastActiveAt = session.CreatedAt
	m.Sessions[session.TokenHash] = session
	return nil
}

func (m *MockRepository) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*Session, *User, *Organization, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sess, ok := m.Sessions[tokenHash]
	if !ok || time.Now().UTC().After(sess.ExpiresAt) {
		return nil, nil, nil, ErrSessionNotFound
	}

	user, ok := m.Users[sess.UserID]
	if !ok || !user.IsActive {
		return nil, nil, nil, ErrSessionNotFound
	}

	var org *Organization
	if sess.ActiveOrganizationID != nil {
		if o, ok := m.Orgs[*sess.ActiveOrganizationID]; ok {
			orgCopy := *o
			org = &orgCopy
		}
	}

	sessCopy := *sess
	userCopy := *user
	return &sessCopy, &userCopy, org, nil
}

func (m *MockRepository) TouchSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.Sessions {
		if s.ID == sessionID {
			s.LastActiveAt = time.Now().UTC()
			return nil
		}
	}
	return ErrSessionNotFound
}

func (m *MockRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.Sessions, tokenHash)
	return nil
}

func (m *MockRepository) DeleteAllUserSessions(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for k, s := range m.Sessions {
		if s.UserID == userID {
			delete(m.Sessions, k)
		}
	}
	return nil
}

func (m *MockRepository) CreateEmailVerificationToken(ctx context.Context, token *EmailVerificationToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	token.ID = crypto.GenerateUUID()
	token.CreatedAt = time.Now().UTC()
	m.VerifyTokens[token.TokenHash] = token
	return nil
}

func (m *MockRepository) ConsumeEmailVerificationToken(ctx context.Context, tokenHash string) (*EmailVerificationToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tok, ok := m.VerifyTokens[tokenHash]
	if !ok || tok.ConsumedAt != nil || time.Now().UTC().After(tok.ExpiresAt) {
		return nil, ErrTokenNotFoundOrUsed
	}

	now := time.Now().UTC()
	tok.ConsumedAt = &now
	tokCopy := *tok
	return &tokCopy, nil
}

func (m *MockRepository) CreatePasswordResetToken(ctx context.Context, token *PasswordResetToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	token.ID = crypto.GenerateUUID()
	token.CreatedAt = time.Now().UTC()
	m.ResetTokens[token.TokenHash] = token
	return nil
}

func (m *MockRepository) ConsumePasswordResetToken(ctx context.Context, tokenHash string) (*PasswordResetToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	tok, ok := m.ResetTokens[tokenHash]
	if !ok || tok.ConsumedAt != nil || time.Now().UTC().After(tok.ExpiresAt) {
		return nil, ErrTokenNotFoundOrUsed
	}

	now := time.Now().UTC()
	tok.ConsumedAt = &now
	tokCopy := *tok
	return &tokCopy, nil
}

func (m *MockRepository) GetOrganizationMemberships(ctx context.Context, userID string) ([]OrganizationMembershipWithOrg, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var res []OrganizationMembershipWithOrg
	for _, mship := range m.Memberships {
		if mship.UserID == userID {
			if org, ok := m.Orgs[mship.OrganizationID]; ok {
				res = append(res, OrganizationMembershipWithOrg{
					Membership:   *mship,
					Organization: *org,
				})
			}
		}
	}
	return res, nil
}

func (m *MockRepository) GetOrganizationByID(ctx context.Context, orgID string) (*Organization, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	org, ok := m.Orgs[orgID]
	if !ok {
		return nil, ErrOrganizationNotFound
	}
	copyOrg := *org
	return &copyOrg, nil
}

func (m *MockRepository) VerifyUserOrgAccess(ctx context.Context, userID, orgID string) (bool, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, mship := range m.Memberships {
		if mship.UserID == userID && mship.OrganizationID == orgID {
			return true, mship.Role, nil
		}
	}
	return false, "", ErrAccessDenied
}

func (m *MockRepository) RecordAuditEvent(ctx context.Context, event *AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	event.ID = crypto.GenerateUUID()
	event.CreatedAt = time.Now().UTC()
	m.AuditEvents = append(m.AuditEvents, event)
	return nil
}
