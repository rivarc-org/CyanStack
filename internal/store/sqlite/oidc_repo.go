package sqlite

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// OIDCRepository handles OIDC-related database operations
type OIDCRepository struct {
	db *gorm.DB
}

// NewOIDCRepository creates a new OIDC repository
func NewOIDCRepository(db *gorm.DB) *OIDCRepository {
	return &OIDCRepository{db: db}
}

// DB returns the underlying database connection
func (r *OIDCRepository) DB() *gorm.DB {
	return r.db
}

// =============================================================================
// User operations
// =============================================================================

// CreateUser creates a new OIDC user
func (r *OIDCRepository) CreateUser(ctx context.Context, user *OIDCUser) error {
	return r.db.WithContext(ctx).Create(user).Error
}

// GetUserByEmail retrieves a user by email
func (r *OIDCRepository) GetUserByEmail(ctx context.Context, email string) (*OIDCUser, error) {
	var user OIDCUser
	if err := r.db.WithContext(ctx).First(&user, "email = ?", email).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByID retrieves a user by ID
func (r *OIDCRepository) GetUserByID(ctx context.Context, id string) (*OIDCUser, error) {
	var user OIDCUser
	if err := r.db.WithContext(ctx).First(&user, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// CountUsers returns the number of OIDC users
func (r *OIDCRepository) CountUsers(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&OIDCUser{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// =============================================================================
// Client operations
// =============================================================================

// CreateClient creates a new OIDC client
func (r *OIDCRepository) CreateClient(ctx context.Context, client *OIDCClient) error {
	return r.db.WithContext(ctx).Create(client).Error
}

// GetClientByID retrieves a client by ID
func (r *OIDCRepository) GetClientByID(ctx context.Context, id string) (*OIDCClient, error) {
	var client OIDCClient
	if err := r.db.WithContext(ctx).First(&client, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &client, nil
}

// GetClientByNodeID retrieves a client by project and node ID
func (r *OIDCRepository) GetClientByNodeID(ctx context.Context, projectID, nodeID string) (*OIDCClient, error) {
	var client OIDCClient
	if err := r.db.WithContext(ctx).First(&client, "project_id = ? AND node_id = ?", projectID, nodeID).Error; err != nil {
		return nil, err
	}
	return &client, nil
}

// ListClientsByProject returns all clients for a project
func (r *OIDCRepository) ListClientsByProject(ctx context.Context, projectID string) ([]OIDCClient, error) {
	var clients []OIDCClient
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

// DeleteClientsByProject removes all clients for a project
func (r *OIDCRepository) DeleteClientsByProject(ctx context.Context, projectID string) error {
	return r.db.WithContext(ctx).Where("project_id = ?", projectID).Delete(&OIDCClient{}).Error
}

// DeleteClientByNodeID removes a client by project and node ID
func (r *OIDCRepository) DeleteClientByNodeID(ctx context.Context, projectID, nodeID string) error {
	return r.db.WithContext(ctx).Where("project_id = ? AND node_id = ?", projectID, nodeID).Delete(&OIDCClient{}).Error
}

// =============================================================================
// Session operations
// =============================================================================

// CreateSession creates a new session
func (r *OIDCRepository) CreateSession(ctx context.Context, session *OIDCSession) error {
	return r.db.WithContext(ctx).Create(session).Error
}

// GetSession retrieves a session by ID
func (r *OIDCRepository) GetSession(ctx context.Context, id string) (*OIDCSession, error) {
	var session OIDCSession
	if err := r.db.WithContext(ctx).First(&session, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// DeleteSession removes a session by ID
func (r *OIDCRepository) DeleteSession(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&OIDCSession{}, "id = ?", id).Error
}

// DeleteExpiredSessions removes all expired sessions
func (r *OIDCRepository) DeleteExpiredSessions(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&OIDCSession{}).Error
}

// =============================================================================
// Signing Key operations
// =============================================================================

// CreateSigningKey stores a new signing key
func (r *OIDCRepository) CreateSigningKey(ctx context.Context, key *OIDCSigningKey) error {
	return r.db.WithContext(ctx).Create(key).Error
}

// GetActiveSigningKey retrieves the active signing key
func (r *OIDCRepository) GetActiveSigningKey(ctx context.Context) (*OIDCSigningKey, error) {
	var key OIDCSigningKey
	if err := r.db.WithContext(ctx).First(&key, "is_active = ?", true).Error; err != nil {
		return nil, err
	}
	return &key, nil
}

// =============================================================================
// Auth Request operations
// =============================================================================

// CreateAuthRequest stores a new auth request
func (r *OIDCRepository) CreateAuthRequest(ctx context.Context, req *OIDCAuthRequest) error {
	return r.db.WithContext(ctx).Create(req).Error
}

// GetAuthRequest retrieves an auth request by ID
func (r *OIDCRepository) GetAuthRequest(ctx context.Context, id string) (*OIDCAuthRequest, error) {
	var req OIDCAuthRequest
	if err := r.db.WithContext(ctx).First(&req, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &req, nil
}

// UpdateAuthRequest updates an auth request
func (r *OIDCRepository) UpdateAuthRequest(ctx context.Context, id string, updates map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&OIDCAuthRequest{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteAuthRequest removes an auth request
func (r *OIDCRepository) DeleteAuthRequest(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&OIDCAuthRequest{}, "id = ?", id).Error
}

// =============================================================================
// Cleanup operations
// =============================================================================

// CleanupExpiredTokens removes expired tokens and auth codes
func (r *OIDCRepository) CleanupExpiredTokens(ctx context.Context) error {
	now := time.Now()

	// Delete expired auth codes
	if err := r.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&OIDCAuthCode{}).Error; err != nil {
		return err
	}

	// Delete expired tokens
	if err := r.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&OIDCToken{}).Error; err != nil {
		return err
	}

	// Delete expired refresh tokens
	if err := r.db.WithContext(ctx).Where("expires_at < ?", now).Delete(&OIDCRefreshToken{}).Error; err != nil {
		return err
	}

	return nil
}
