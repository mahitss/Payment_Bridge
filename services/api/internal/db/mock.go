package db

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"claimbridge/services/api/internal/crypto"
)

// MockRepository provides an in-memory implementation of Repository for automated tests.
type MockRepository struct {
	mu                  sync.RWMutex
	Users               map[string]*User                   // id -> User
	UsersByEmail        map[string]*User                   // lowercase email -> User
	Orgs                map[string]*Organization           // id -> Org
	Memberships         map[string]*OrganizationMembership // id -> Membership
	Sessions            map[string]*Session                // token_hash -> Session
	VerifyTokens        map[string]*EmailVerificationToken // token_hash -> Token
	ResetTokens         map[string]*PasswordResetToken     // token_hash -> Token
	Cases               map[string]*PayoutCase             // id -> Case
	Operations          map[string]*PayoutOperation        // id -> Operation
	OperationsByBatchID map[string]*PayoutOperation        // sender_batch_id -> Operation
	Investigations      map[string]*PayoutInvestigation    // id -> Investigation
	Snapshots           []*PayoutStatusSnapshot
	AuditEvents         []*AuditEvent
	FailNextOp          bool
}

func NewMockRepository() *MockRepository {
	return &MockRepository{
		Users:               make(map[string]*User),
		UsersByEmail:        make(map[string]*User),
		Orgs:                make(map[string]*Organization),
		Memberships:         make(map[string]*OrganizationMembership),
		Sessions:            make(map[string]*Session),
		VerifyTokens:        make(map[string]*EmailVerificationToken),
		ResetTokens:         make(map[string]*PasswordResetToken),
		Cases:               make(map[string]*PayoutCase),
		Operations:          make(map[string]*PayoutOperation),
		OperationsByBatchID: make(map[string]*PayoutOperation),
		Investigations:      make(map[string]*PayoutInvestigation),
		Snapshots:           make([]*PayoutStatusSnapshot, 0),
		AuditEvents:         make([]*AuditEvent, 0),
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

func (m *MockRepository) CreateUser(ctx context.Context, user *User) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	normEmail := strings.ToLower(strings.TrimSpace(user.Email))
	if _, exists := m.UsersByEmail[normEmail]; exists {
		return nil, ErrUserAlreadyExists
	}

	now := time.Now().UTC()
	userID := user.ID
	if userID == "" {
		userID = crypto.GenerateUUID()
	}
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

	copyUser := *createdUser
	return &copyUser, nil
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

func (m *MockRepository) AddOrganizationMember(ctx context.Context, orgID, userID, role string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, mship := range m.Memberships {
		if mship.UserID == userID && mship.OrganizationID == orgID {
			mship.Role = role
			return nil
		}
	}

	newID := crypto.GenerateUUID()
	m.Memberships[newID] = &OrganizationMembership{
		ID:             newID,
		OrganizationID: orgID,
		UserID:         userID,
		Role:           role,
		CreatedAt:      time.Now().UTC(),
	}
	return nil
}

func (m *MockRepository) RecordAuditEvent(ctx context.Context, event *AuditEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	event.ID = crypto.GenerateUUID()
	event.CreatedAt = time.Now().UTC()
	m.AuditEvents = append(m.AuditEvents, event)
	return nil
}

// Payout Cases

func (m *MockRepository) CreatePayoutCase(ctx context.Context, c *PayoutCase) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check reference uniqueness within the organization
	for _, existing := range m.Cases {
		if existing.OrganizationID == c.OrganizationID && strings.EqualFold(existing.Reference, c.Reference) {
			return errors.New("a payout case with this reference already exists in your organization")
		}
	}

	if c.ID == "" {
		c.ID = crypto.GenerateUUID()
	}
	now := time.Now().UTC()
	c.CreatedAt = now
	c.UpdatedAt = now

	copied := *c
	m.Cases[c.ID] = &copied
	return nil
}

func (m *MockRepository) GetPayoutCaseByID(ctx context.Context, orgID, caseID string) (*PayoutCase, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, ok := m.Cases[caseID]
	if !ok || c.OrganizationID != orgID {
		return nil, ErrPayoutCaseNotFound
	}
	copied := *c
	return &copied, nil
}

func (m *MockRepository) GetPayoutCaseByReference(ctx context.Context, orgID, reference string) (*PayoutCase, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, c := range m.Cases {
		if c.OrganizationID == orgID && strings.EqualFold(c.Reference, reference) {
			copied := *c
			return &copied, nil
		}
	}
	return nil, ErrPayoutCaseNotFound
}

func (m *MockRepository) ListPayoutCasesByOrg(ctx context.Context, orgID string, limit, offset int) ([]PayoutCase, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var matched []PayoutCase
	for _, c := range m.Cases {
		if c.OrganizationID == orgID {
			matched = append(matched, *c)
		}
	}

	// Sort descending by CreatedAt
	for i := 0; i < len(matched)-1; i++ {
		for j := i + 1; j < len(matched); j++ {
			if matched[j].CreatedAt.After(matched[i].CreatedAt) {
				matched[i], matched[j] = matched[j], matched[i]
			}
		}
	}

	total := len(matched)
	if offset >= total {
		return []PayoutCase{}, total, nil
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}

	return matched[offset:end], total, nil
}

func (m *MockRepository) UpdatePayoutCaseStatus(ctx context.Context, orgID, caseID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.Cases[caseID]
	if !ok || c.OrganizationID != orgID {
		return ErrPayoutCaseNotFound
	}
	c.Status = status
	c.UpdatedAt = time.Now().UTC()
	return nil
}

// Payout Operations

func (m *MockRepository) CreatePayoutOperation(ctx context.Context, op *PayoutOperation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.OperationsByBatchID[op.SenderBatchID]; exists {
		return errors.New("duplicate sender_batch_id")
	}

	if op.ID == "" {
		op.ID = crypto.GenerateUUID()
	}
	now := time.Now().UTC()
	op.CreatedAt = now
	op.UpdatedAt = now

	copied := *op
	m.Operations[op.ID] = &copied
	m.OperationsByBatchID[op.SenderBatchID] = &copied
	return nil
}

func (m *MockRepository) GetPayoutOperationByID(ctx context.Context, orgID, opID string) (*PayoutOperation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	op, ok := m.Operations[opID]
	if !ok || op.OrganizationID != orgID {
		return nil, ErrPayoutOperationNotFound
	}
	copied := *op
	return &copied, nil
}

func (m *MockRepository) GetPayoutOperationBySenderBatchID(ctx context.Context, senderBatchID string) (*PayoutOperation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	op, ok := m.OperationsByBatchID[senderBatchID]
	if !ok {
		return nil, ErrPayoutOperationNotFound
	}
	copied := *op
	return &copied, nil
}

func (m *MockRepository) ListPayoutOperationsByCaseID(ctx context.Context, orgID, caseID string) ([]PayoutOperation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var res []PayoutOperation
	for _, op := range m.Operations {
		if op.PayoutCaseID == caseID && op.OrganizationID == orgID {
			res = append(res, *op)
		}
	}

	for i := 0; i < len(res)-1; i++ {
		for j := i + 1; j < len(res); j++ {
			if res[j].CreatedAt.After(res[i].CreatedAt) {
				res[i], res[j] = res[j], res[i]
			}
		}
	}

	return res, nil
}

func (m *MockRepository) UpdatePayoutOperation(ctx context.Context, op *PayoutOperation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.Operations[op.ID]
	if !ok || existing.OrganizationID != op.OrganizationID {
		return ErrPayoutOperationNotFound
	}

	op.UpdatedAt = time.Now().UTC()
	copied := *op
	m.Operations[op.ID] = &copied
	m.OperationsByBatchID[op.SenderBatchID] = &copied
	return nil
}

func (m *MockRepository) ApprovePayoutOperation(ctx context.Context, orgID, opID, approvedByUserID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	op, ok := m.Operations[opID]
	if !ok || op.OrganizationID != orgID {
		return ErrPayoutOperationNotFound
	}

	if op.RequiresDualApproval && op.InitiatedByUserID == approvedByUserID {
		return ErrSelfApprovalForbidden
	}

	now := time.Now().UTC()
	op.Status = "APPROVED"
	op.ApprovedByUserID = &approvedByUserID
	op.ApprovedAt = &now
	op.UpdatedAt = now
	return nil
}

// Status Snapshots

func (m *MockRepository) CreatePayoutStatusSnapshot(ctx context.Context, snapshot *PayoutStatusSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if snapshot.ID == "" {
		snapshot.ID = crypto.GenerateUUID()
	}
	snapshot.RecordedAt = time.Now().UTC()
	copied := *snapshot
	m.Snapshots = append(m.Snapshots, &copied)
	return nil
}

func (m *MockRepository) ListPayoutStatusSnapshots(ctx context.Context, orgID, opID string) ([]PayoutStatusSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var res []PayoutStatusSnapshot
	for _, s := range m.Snapshots {
		if s.PayoutOperationID == opID && s.OrganizationID == orgID {
			res = append(res, *s)
		}
	}
	return res, nil
}

// Investigations

func (m *MockRepository) CreateInvestigation(ctx context.Context, inv *PayoutInvestigation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if inv.ID == "" {
		inv.ID = crypto.GenerateUUID()
	}
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now().UTC()
	}
	copied := *inv
	m.Investigations[inv.ID] = &copied
	return nil
}

func (m *MockRepository) GetInvestigationByID(ctx context.Context, orgID, investigationID string) (*PayoutInvestigation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	inv, ok := m.Investigations[investigationID]
	if !ok || inv.OrganizationID != orgID {
		return nil, ErrInvestigationNotFound
	}
	copied := *inv
	return &copied, nil
}

func (m *MockRepository) ListInvestigationsByCaseID(ctx context.Context, orgID, caseID string) ([]PayoutInvestigation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var res []PayoutInvestigation
	for _, inv := range m.Investigations {
		if inv.PayoutCaseID == caseID && inv.OrganizationID == orgID {
			res = append(res, *inv)
		}
	}
	return res, nil
}

func (m *MockRepository) DeleteSyntheticFixtures(ctx context.Context, orgID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var deletedCount int64
	var caseIDsToDelete []string
	for id, c := range m.Cases {
		if c.IsSyntheticFixture && (orgID == "" || c.OrganizationID == orgID) {
			caseIDsToDelete = append(caseIDsToDelete, id)
		}
	}

	for _, caseID := range caseIDsToDelete {
		delete(m.Cases, caseID)
		deletedCount++

		// Cascade delete operations
		var opIDsToDelete []string
		for opID, op := range m.Operations {
			if op.PayoutCaseID == caseID {
				opIDsToDelete = append(opIDsToDelete, opID)
				delete(m.OperationsByBatchID, op.SenderBatchID)
			}
		}
		for _, opID := range opIDsToDelete {
			delete(m.Operations, opID)
			// Cascade delete snapshots
			var keptSnapshots []*PayoutStatusSnapshot
			for _, s := range m.Snapshots {
				if s.PayoutOperationID != opID {
					keptSnapshots = append(keptSnapshots, s)
				}
			}
			m.Snapshots = keptSnapshots
		}

		// Cascade delete investigations
		for invID, inv := range m.Investigations {
			if inv.PayoutCaseID == caseID {
				delete(m.Investigations, invID)
			}
		}
	}

	return deletedCount, nil
}
