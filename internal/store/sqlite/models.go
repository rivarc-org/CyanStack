package sqlite

import (
	"time"
)

// Project represents a CyanStack project
type Project struct {
	ID                string    `gorm:"primaryKey;size:36"`
	Name              string    `gorm:"size:255;not null"`
	CurrentRevisionID *string   `gorm:"size:36"`
	CreatedAt         time.Time `gorm:"autoCreateTime"`

	// Relations
	Revisions []Revision `gorm:"foreignKey:ProjectID;constraint:OnDelete:CASCADE"`
}

// TableName specifies the table name for Project
func (Project) TableName() string {
	return "projects"
}

// Revision represents a versioned snapshot of project state
type Revision struct {
	ID            string    `gorm:"primaryKey;size:36"`
	ProjectID     string    `gorm:"size:36;not null;index"`
	VersionNumber int       `gorm:"not null"`
	GraphState    string    `gorm:"type:text;not null"` // JSON-serialized node graph
	IsDraft       bool      `gorm:"column:is_draft;not null"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`

	// Relations
	Project Project `gorm:"foreignKey:ProjectID"`
}

// TableName specifies the table name for Revision
func (Revision) TableName() string {
	return "revisions"
}

// =============================================================================
// OIDC Models
// =============================================================================

// OIDCUser represents a user that can authenticate via OIDC
type OIDCUser struct {
	ID           string    `gorm:"primaryKey;size:36"`
	Email        string    `gorm:"size:255;not null;uniqueIndex"`
	PasswordHash string    `gorm:"size:255;not null"`
	CreatedAt    time.Time `gorm:"autoCreateTime"`
}

// TableName specifies the table name for OIDCUser
func (OIDCUser) TableName() string {
	return "oidc_users"
}

// OIDCClient represents a registered OIDC client (plugin)
type OIDCClient struct {
	ID            string    `gorm:"primaryKey;size:36"`
	SecretHash    string    `gorm:"size:255;not null"`
	Name          string    `gorm:"size:255;not null"`
	PluginID      string    `gorm:"size:255;not null"`
	NodeID        string    `gorm:"size:36;not null;index"`
	ProjectID     string    `gorm:"size:36;not null;index"`
	RedirectURIs  string    `gorm:"type:text;not null"` // JSON array
	GrantTypes    string    `gorm:"type:text;not null"`
	ResponseTypes string    `gorm:"type:text;not null"`
	Scopes        string    `gorm:"type:text;not null"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`

	// Relations
	Project Project `gorm:"foreignKey:ProjectID"`
}

// TableName specifies the table name for OIDCClient
func (OIDCClient) TableName() string {
	return "oidc_clients"
}

// OIDCAuthRequest represents a pending authorization request
type OIDCAuthRequest struct {
	ID                  string     `gorm:"primaryKey;size:36"`
	ClientID            string     `gorm:"size:36;not null;index"`
	RedirectURI         string     `gorm:"type:text;not null"`
	Scopes              string     `gorm:"type:text;not null"` // JSON array
	State               string     `gorm:"type:text"`
	Nonce               string     `gorm:"type:text"`
	ResponseType        string     `gorm:"size:50;not null"`
	CodeChallenge       string     `gorm:"type:text"`
	CodeChallengeMethod string     `gorm:"size:20"`
	UserID              *string    `gorm:"size:36"`
	AuthTime            *time.Time `gorm:""`
	Done                bool       `gorm:"not null;default:false"`
	CreatedAt           time.Time  `gorm:"autoCreateTime"`
}

// TableName specifies the table name for OIDCAuthRequest
func (OIDCAuthRequest) TableName() string {
	return "oidc_auth_requests"
}

// OIDCAuthCode represents an authorization code
type OIDCAuthCode struct {
	ID            string    `gorm:"primaryKey;size:36"`
	AuthRequestID string    `gorm:"size:36;not null;uniqueIndex"`
	ExpiresAt     time.Time `gorm:"not null;index"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`

	// Relations
	AuthRequest OIDCAuthRequest `gorm:"foreignKey:AuthRequestID"`
}

// TableName specifies the table name for OIDCAuthCode
func (OIDCAuthCode) TableName() string {
	return "oidc_auth_codes"
}

// OIDCToken represents an access token
type OIDCToken struct {
	ID                   string    `gorm:"primaryKey;size:36"`
	ApplicationID        string    `gorm:"size:36;not null"`
	Subject              string    `gorm:"size:36;not null;index"`
	Audience             string    `gorm:"type:text;not null"` // JSON array
	Scopes               string    `gorm:"type:text;not null"` // JSON array
	AuthMethodReferences string    `gorm:"type:text"`          // JSON array
	AuthTime             time.Time `gorm:"not null"`
	ExpiresAt            time.Time `gorm:"not null;index"`
	CreatedAt            time.Time `gorm:"autoCreateTime"`
}

// TableName specifies the table name for OIDCToken
func (OIDCToken) TableName() string {
	return "oidc_tokens"
}

// OIDCRefreshToken represents a refresh token
type OIDCRefreshToken struct {
	ID            string    `gorm:"primaryKey;size:36"`
	TokenID       string    `gorm:"size:36;not null"`
	AuthRequestID string    `gorm:"size:36;not null"`
	AuthTime      time.Time `gorm:"not null"`
	AMR           string    `gorm:"type:text"` // JSON array
	ExpiresAt     time.Time `gorm:"not null;index"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`

	// Relations
	AuthRequest OIDCAuthRequest `gorm:"foreignKey:AuthRequestID"`
}

// TableName specifies the table name for OIDCRefreshToken
func (OIDCRefreshToken) TableName() string {
	return "oidc_refresh_tokens"
}

// OIDCSigningKey holds RSA keys for JWT signing
type OIDCSigningKey struct {
	ID            string    `gorm:"primaryKey;size:36"`
	PrivateKeyPEM string    `gorm:"type:text;not null"`
	PublicKeyPEM  string    `gorm:"type:text;not null"`
	Algorithm     string    `gorm:"size:16;not null;default:RS256"`
	IsActive      bool      `gorm:"not null;default:true"`
	CreatedAt     time.Time `gorm:"autoCreateTime"`
}

// TableName specifies the table name for OIDCSigningKey
func (OIDCSigningKey) TableName() string {
	return "oidc_signing_keys"
}

// OIDCSession represents an authenticated root session
type OIDCSession struct {
	ID        string    `gorm:"primaryKey;size:36"`
	UserID    string    `gorm:"size:36;not null;index"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time `gorm:"autoCreateTime"`

	// Relations
	User OIDCUser `gorm:"foreignKey:UserID"`
}

// TableName specifies the table name for OIDCSession
func (OIDCSession) TableName() string {
	return "oidc_sessions"
}
