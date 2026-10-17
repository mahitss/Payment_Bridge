package db

import (
	"time"
)

type User struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	PasswordHash    string     `json:"-"`
	Name            string     `json:"name"`
	EmailVerified   bool       `json:"email_verified"`
	EmailVerifiedAt *time.Time `json:"email_verified_at,omitempty"`
	IsActive        bool       `json:"is_active"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Organization struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	IsPersonal bool      `json:"is_personal"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type OrganizationMembership struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organization_id"`
	UserID         string    `json:"user_id"`
	Role           string    `json:"role"` // OWNER, ADMIN, MEMBER
	CreatedAt      time.Time `json:"created_at"`
}

type OrganizationMembershipWithOrg struct {
	Membership   OrganizationMembership `json:"membership"`
	Organization Organization           `json:"organization"`
}

type Session struct {
	ID                   string    `json:"id"`
	UserID               string    `json:"user_id"`
	TokenHash            string    `json:"-"`
	ActiveOrganizationID *string   `json:"active_organization_id,omitempty"`
	IPAddress            string    `json:"ip_address,omitempty"`
	UserAgent            string    `json:"user_agent,omitempty"`
	ExpiresAt            time.Time `json:"expires_at"`
	CreatedAt            time.Time `json:"created_at"`
	LastActiveAt         time.Time `json:"last_active_at"`
}

type EmailVerificationToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	TokenHash  string     `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type PasswordResetToken struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	TokenHash  string     `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	ConsumedAt *time.Time `json:"consumed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type AuditEvent struct {
	ID             string                 `json:"id"`
	UserID         *string                `json:"user_id,omitempty"`
	OrganizationID *string                `json:"organization_id,omitempty"`
	EventType      string                 `json:"event_type"`
	IPAddress      string                 `json:"ip_address,omitempty"`
	UserAgent      string                 `json:"user_agent,omitempty"`
	Metadata       map[string]interface{} `json:"metadata"`
	CreatedAt      time.Time              `json:"created_at"`
}
