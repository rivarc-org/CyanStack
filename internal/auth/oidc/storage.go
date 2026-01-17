package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/text/language"
	"gorm.io/gorm"

	"github.com/cyanstack/cyanstack/internal/store/sqlite"
)

// Storage implements op.Storage for SQLite
type Storage struct {
	db         *gorm.DB
	signingKey *SigningKey

	// Configuration
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
	authCodeTTL     time.Duration
}

// NewStorage creates a new OIDC storage
func NewStorage(db *gorm.DB, signingKey *SigningKey) *Storage {
	return &Storage{
		db:              db,
		signingKey:      signingKey,
		accessTokenTTL:  time.Hour,
		refreshTokenTTL: 24 * time.Hour,
		authCodeTTL:     10 * time.Minute,
	}
}

// DB returns the underlying database connection
func (s *Storage) DB() *gorm.DB {
	return s.db
}

// =============================================================================
// op.Storage interface implementation
// =============================================================================

// Health checks storage availability
func (s *Storage) Health(ctx context.Context) error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// CreateAuthRequest stores a new authorization request
func (s *Storage) CreateAuthRequest(ctx context.Context, authReq *oidc.AuthRequest, userID string) (op.AuthRequest, error) {
	scopes, _ := json.Marshal(authReq.Scopes)

	req := &sqlite.OIDCAuthRequest{
		ID:                  uuid.New().String(),
		ClientID:            authReq.ClientID,
		RedirectURI:         authReq.RedirectURI,
		Scopes:              string(scopes),
		State:               authReq.State,
		Nonce:               authReq.Nonce,
		ResponseType:        string(authReq.ResponseType),
		CodeChallenge:       authReq.CodeChallenge,
		CodeChallengeMethod: string(authReq.CodeChallengeMethod),
		Done:                false,
	}

	if err := s.db.WithContext(ctx).Create(req).Error; err != nil {
		return nil, err
	}

	return &AuthRequest{model: req}, nil
}

// AuthRequestByID retrieves an authorization request by ID
func (s *Storage) AuthRequestByID(ctx context.Context, id string) (op.AuthRequest, error) {
	var req sqlite.OIDCAuthRequest
	if err := s.db.WithContext(ctx).First(&req, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &AuthRequest{model: &req}, nil
}

// AuthRequestByCode retrieves auth request by authorization code
func (s *Storage) AuthRequestByCode(ctx context.Context, code string) (op.AuthRequest, error) {
	var authCode sqlite.OIDCAuthCode
	if err := s.db.WithContext(ctx).Preload("AuthRequest").First(&authCode, "id = ?", code).Error; err != nil {
		return nil, err
	}

	if time.Now().After(authCode.ExpiresAt) {
		return nil, fmt.Errorf("auth code expired")
	}

	return &AuthRequest{model: &authCode.AuthRequest}, nil
}

// SaveAuthCode stores the authorization code
func (s *Storage) SaveAuthCode(ctx context.Context, id, code string) error {
	authCode := &sqlite.OIDCAuthCode{
		ID:            code,
		AuthRequestID: id,
		ExpiresAt:     time.Now().Add(s.authCodeTTL),
	}

	return s.db.WithContext(ctx).Create(authCode).Error
}

// DeleteAuthRequest removes an auth request after successful token exchange
func (s *Storage) DeleteAuthRequest(ctx context.Context, id string) error {
	// Delete the auth code first (if exists)
	s.db.WithContext(ctx).Where("auth_request_id = ?", id).Delete(&sqlite.OIDCAuthCode{})
	// Delete the auth request
	return s.db.WithContext(ctx).Delete(&sqlite.OIDCAuthRequest{}, "id = ?", id).Error
}

// CreateAccessToken creates a new access token
func (s *Storage) CreateAccessToken(ctx context.Context, request op.TokenRequest) (accessTokenID string, expiration time.Time, err error) {
	tokenID := uuid.New().String()
	expiresAt := time.Now().Add(s.accessTokenTTL)

	audience, _ := json.Marshal(request.GetAudience())
	scopes, _ := json.Marshal(request.GetScopes())

	token := &sqlite.OIDCToken{
		ID:            tokenID,
		ApplicationID: getClientIDFromRequest(request),
		Subject:       request.GetSubject(),
		Audience:      string(audience),
		Scopes:        string(scopes),
		AuthTime:      getAuthTimeFromRequest(request),
		ExpiresAt:     expiresAt,
	}

	if err := s.db.WithContext(ctx).Create(token).Error; err != nil {
		return "", time.Time{}, err
	}

	return tokenID, expiresAt, nil
}

// CreateAccessAndRefreshTokens creates both access and refresh tokens
func (s *Storage) CreateAccessAndRefreshTokens(
	ctx context.Context,
	request op.TokenRequest,
	currentRefreshToken string,
) (accessTokenID string, newRefreshTokenID string, expiration time.Time, err error) {
	// Create access token
	accessTokenID, expiration, err = s.CreateAccessToken(ctx, request)
	if err != nil {
		return "", "", time.Time{}, err
	}

	// Generate refresh token
	refreshBytes := make([]byte, 32)
	if _, err := rand.Read(refreshBytes); err != nil {
		return "", "", time.Time{}, err
	}
	newRefreshTokenID = base64.RawURLEncoding.EncodeToString(refreshBytes)

	// Get auth request ID from the request
	authReqID := ""
	if ar, ok := request.(*AuthRequest); ok {
		authReqID = ar.GetID()
	}

	// Store refresh token
	rt := &sqlite.OIDCRefreshToken{
		ID:            newRefreshTokenID,
		TokenID:       accessTokenID,
		AuthRequestID: authReqID,
		AuthTime:      getAuthTimeFromRequest(request),
		ExpiresAt:     time.Now().Add(s.refreshTokenTTL),
	}

	if err := s.db.WithContext(ctx).Create(rt).Error; err != nil {
		return "", "", time.Time{}, err
	}

	// Delete old refresh token if provided
	if currentRefreshToken != "" {
		s.db.WithContext(ctx).Delete(&sqlite.OIDCRefreshToken{}, "id = ?", currentRefreshToken)
	}

	return accessTokenID, newRefreshTokenID, expiration, nil
}

// TokenRequestByRefreshToken validates and returns the token request for a refresh token
func (s *Storage) TokenRequestByRefreshToken(ctx context.Context, refreshToken string) (op.RefreshTokenRequest, error) {
	var rt sqlite.OIDCRefreshToken
	if err := s.db.WithContext(ctx).Preload("AuthRequest").First(&rt, "id = ?", refreshToken).Error; err != nil {
		return nil, err
	}

	if time.Now().After(rt.ExpiresAt) {
		return nil, fmt.Errorf("refresh token expired")
	}

	return &RefreshTokenRequest{
		rt:          &rt,
		authRequest: &rt.AuthRequest,
	}, nil
}

// TerminateSession ends a user session
func (s *Storage) TerminateSession(ctx context.Context, userID, clientID string) error {
	return s.db.WithContext(ctx).Delete(&sqlite.OIDCSession{}, "user_id = ?", userID).Error
}

// RevokeToken revokes a token
func (s *Storage) RevokeToken(ctx context.Context, tokenOrTokenID string, userID string, clientID string) *oidc.Error {
	// Try to delete as refresh token
	result := s.db.WithContext(ctx).Delete(&sqlite.OIDCRefreshToken{}, "id = ?", tokenOrTokenID)
	if result.RowsAffected > 0 {
		return nil
	}

	// Try to delete as access token
	result = s.db.WithContext(ctx).Delete(&sqlite.OIDCToken{}, "id = ?", tokenOrTokenID)
	if result.RowsAffected > 0 {
		return nil
	}

	return nil // Token not found is not an error per spec
}

// GetRefreshTokenInfo returns information about a refresh token
func (s *Storage) GetRefreshTokenInfo(ctx context.Context, clientID, token string) (userID string, tokenID string, err error) {
	var rt sqlite.OIDCRefreshToken
	if err := s.db.WithContext(ctx).Preload("AuthRequest").First(&rt, "id = ?", token).Error; err != nil {
		return "", "", err
	}

	if rt.AuthRequest.UserID == nil {
		return "", "", fmt.Errorf("no user associated with refresh token")
	}

	return *rt.AuthRequest.UserID, rt.TokenID, nil
}

// SigningKey returns the key used for signing tokens
func (s *Storage) SigningKey(ctx context.Context) (op.SigningKey, error) {
	return &signingKeyWrapper{key: s.signingKey}, nil
}

// SignatureAlgorithms returns the supported signature algorithms
func (s *Storage) SignatureAlgorithms(ctx context.Context) ([]jose.SignatureAlgorithm, error) {
	return []jose.SignatureAlgorithm{jose.RS256}, nil
}

// KeySet returns the public key set for token verification
func (s *Storage) KeySet(ctx context.Context) ([]op.Key, error) {
	return []op.Key{&keyWrapper{key: s.signingKey}}, nil
}

// GetClientByClientID retrieves a client by ID
func (s *Storage) GetClientByClientID(ctx context.Context, clientID string) (op.Client, error) {
	var client sqlite.OIDCClient
	if err := s.db.WithContext(ctx).First(&client, "id = ?", clientID).Error; err != nil {
		return nil, err
	}
	return NewClient(&client)
}

// AuthorizeClientIDSecret validates client credentials
func (s *Storage) AuthorizeClientIDSecret(ctx context.Context, clientID, secret string) error {
	client, err := s.GetClientByClientID(ctx, clientID)
	if err != nil {
		return err
	}

	if c, ok := client.(*Client); ok {
		return c.ValidateSecret(secret)
	}

	return fmt.Errorf("invalid client type")
}

// SetUserinfoFromScopes sets user info claims based on scopes
func (s *Storage) SetUserinfoFromScopes(ctx context.Context, userinfo *oidc.UserInfo, userID, clientID string, scopes []string) error {
	user, err := GetUserByID(ctx, s.db, userID)
	if err != nil {
		return err
	}

	for _, scope := range scopes {
		switch scope {
		case oidc.ScopeEmail:
			userinfo.Email = user.Email
			userinfo.EmailVerified = oidc.Bool(true)
		case oidc.ScopeProfile:
			userinfo.Name = user.Email // Use email as name for now
			userinfo.PreferredUsername = user.Email
		}
	}

	return nil
}

// SetUserinfoFromToken sets user info from token claims
func (s *Storage) SetUserinfoFromToken(ctx context.Context, userinfo *oidc.UserInfo, tokenID, subject, origin string) error {
	return s.SetUserinfoFromScopes(ctx, userinfo, subject, "", []string{oidc.ScopeEmail, oidc.ScopeProfile})
}

// SetIntrospectionFromToken sets introspection response from token
func (s *Storage) SetIntrospectionFromToken(ctx context.Context, introspection *oidc.IntrospectionResponse, tokenID, subject, clientID string) error {
	var token sqlite.OIDCToken
	if err := s.db.WithContext(ctx).First(&token, "id = ?", tokenID).Error; err != nil {
		return err
	}

	introspection.Active = true
	introspection.Subject = token.Subject
	introspection.ClientID = token.ApplicationID

	return nil
}

// GetPrivateClaimsFromScopes returns additional private claims
func (s *Storage) GetPrivateClaimsFromScopes(ctx context.Context, userID, clientID string, scopes []string) (map[string]interface{}, error) {
	return nil, nil
}

// GetKeyByIDAndClientID returns a key for a specific client
func (s *Storage) GetKeyByIDAndClientID(ctx context.Context, keyID, clientID string) (*jose.JSONWebKey, error) {
	if keyID == s.signingKey.ID {
		jwk := s.signingKey.JWK()
		return &jwk, nil
	}
	return nil, fmt.Errorf("key not found")
}

// ValidateJWTProfileScopes validates scopes for JWT profile
func (s *Storage) ValidateJWTProfileScopes(ctx context.Context, userID string, scopes []string) ([]string, error) {
	return scopes, nil
}

// =============================================================================
// Client Registration
// =============================================================================

// RegisterClient creates a new OIDC client
func (s *Storage) RegisterClient(ctx context.Context, reg ClientRegistration) (*ClientCredentials, error) {
	clientID := uuid.New().String()
	clientSecret := generateSecureSecret(32)

	hash, err := bcrypt.GenerateFromPassword([]byte(clientSecret), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing secret: %w", err)
	}

	redirectURIs, _ := json.Marshal(reg.RedirectURIs)
	grantTypes, _ := json.Marshal([]string{"authorization_code", "refresh_token"})
	responseTypes, _ := json.Marshal([]string{"code"})
	scopes, _ := json.Marshal([]string{"openid", "profile", "email"})

	client := &sqlite.OIDCClient{
		ID:            clientID,
		SecretHash:    string(hash),
		Name:          reg.Name,
		PluginID:      reg.PluginID,
		NodeID:        reg.NodeID,
		ProjectID:     reg.ProjectID,
		RedirectURIs:  string(redirectURIs),
		GrantTypes:    string(grantTypes),
		ResponseTypes: string(responseTypes),
		Scopes:        string(scopes),
	}

	if err := s.db.WithContext(ctx).Create(client).Error; err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}

	return &ClientCredentials{
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

// GetClientByNodeID retrieves a client by project and node ID
func (s *Storage) GetClientByNodeID(ctx context.Context, projectID, nodeID string) (*ClientCredentials, error) {
	var client sqlite.OIDCClient
	if err := s.db.WithContext(ctx).First(&client, "project_id = ? AND node_id = ?", projectID, nodeID).Error; err != nil {
		return nil, err
	}

	// We can't return the original secret since it's hashed
	// This is used to check if a client already exists
	return &ClientCredentials{
		ClientID:     client.ID,
		ClientSecret: "", // Secret is hashed, can't be retrieved
	}, nil
}

// =============================================================================
// Session Management
// =============================================================================

// CreateSession creates a new session for a user
func (s *Storage) CreateSession(ctx context.Context, sessionID, userID string, expiresAt time.Time) error {
	session := &sqlite.OIDCSession{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}
	return s.db.WithContext(ctx).Create(session).Error
}

// GetSession retrieves a session by ID
func (s *Storage) GetSession(ctx context.Context, sessionID string) (*sqlite.OIDCSession, error) {
	var session sqlite.OIDCSession
	if err := s.db.WithContext(ctx).First(&session, "id = ?", sessionID).Error; err != nil {
		return nil, err
	}

	if time.Now().After(session.ExpiresAt) {
		s.db.WithContext(ctx).Delete(&session)
		return nil, fmt.Errorf("session expired")
	}

	return &session, nil
}

// DeleteSession removes a session
func (s *Storage) DeleteSession(ctx context.Context, sessionID string) error {
	return s.db.WithContext(ctx).Delete(&sqlite.OIDCSession{}, "id = ?", sessionID).Error
}

// CompleteAuthRequest marks an auth request as authenticated
func (s *Storage) CompleteAuthRequest(ctx context.Context, authRequestID, userID string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&sqlite.OIDCAuthRequest{}).
		Where("id = ?", authRequestID).
		Updates(map[string]interface{}{
			"user_id":   userID,
			"auth_time": now,
			"done":      true,
		}).Error
}

// =============================================================================
// Helper types
// =============================================================================

// AuthRequest wraps the database model for the op.AuthRequest interface
type AuthRequest struct {
	model *sqlite.OIDCAuthRequest
}

func (a *AuthRequest) GetID() string {
	return a.model.ID
}

func (a *AuthRequest) GetACR() string {
	return ""
}

func (a *AuthRequest) GetAMR() []string {
	return []string{"pwd"}
}

func (a *AuthRequest) GetAudience() []string {
	return []string{a.model.ClientID}
}

func (a *AuthRequest) GetAuthTime() time.Time {
	if a.model.AuthTime != nil {
		return *a.model.AuthTime
	}
	return time.Now()
}

func (a *AuthRequest) GetClientID() string {
	return a.model.ClientID
}

func (a *AuthRequest) GetCodeChallenge() *oidc.CodeChallenge {
	if a.model.CodeChallenge == "" {
		return nil
	}
	return &oidc.CodeChallenge{
		Challenge: a.model.CodeChallenge,
		Method:    oidc.CodeChallengeMethod(a.model.CodeChallengeMethod),
	}
}

func (a *AuthRequest) GetNonce() string {
	return a.model.Nonce
}

func (a *AuthRequest) GetRedirectURI() string {
	return a.model.RedirectURI
}

func (a *AuthRequest) GetResponseType() oidc.ResponseType {
	return oidc.ResponseType(a.model.ResponseType)
}

func (a *AuthRequest) GetResponseMode() oidc.ResponseMode {
	return ""
}

func (a *AuthRequest) GetScopes() []string {
	var scopes []string
	json.Unmarshal([]byte(a.model.Scopes), &scopes)
	return scopes
}

func (a *AuthRequest) GetState() string {
	return a.model.State
}

func (a *AuthRequest) GetSubject() string {
	if a.model.UserID != nil {
		return *a.model.UserID
	}
	return ""
}

func (a *AuthRequest) Done() bool {
	return a.model.Done
}

// RefreshTokenRequest wraps refresh token data
type RefreshTokenRequest struct {
	rt          *sqlite.OIDCRefreshToken
	authRequest *sqlite.OIDCAuthRequest
}

func (r *RefreshTokenRequest) GetAMR() []string {
	return []string{"pwd"}
}

func (r *RefreshTokenRequest) GetAudience() []string {
	return []string{r.authRequest.ClientID}
}

func (r *RefreshTokenRequest) GetAuthTime() time.Time {
	return r.rt.AuthTime
}

func (r *RefreshTokenRequest) GetClientID() string {
	return r.authRequest.ClientID
}

func (r *RefreshTokenRequest) GetScopes() []string {
	var scopes []string
	json.Unmarshal([]byte(r.authRequest.Scopes), &scopes)
	return scopes
}

func (r *RefreshTokenRequest) GetSubject() string {
	if r.authRequest.UserID != nil {
		return *r.authRequest.UserID
	}
	return ""
}

func (r *RefreshTokenRequest) SetCurrentScopes(scopes []string) {
	// Not implemented for now
}

// signingKeyWrapper wraps SigningKey for op.SigningKey interface
type signingKeyWrapper struct {
	key *SigningKey
}

func (s *signingKeyWrapper) SignatureAlgorithm() jose.SignatureAlgorithm {
	return jose.RS256
}

func (s *signingKeyWrapper) Key() interface{} {
	return s.key.PrivateKey
}

func (s *signingKeyWrapper) ID() string {
	return s.key.ID
}

// keyWrapper wraps SigningKey for op.Key interface
type keyWrapper struct {
	key *SigningKey
}

func (k *keyWrapper) ID() string {
	return k.key.ID
}

func (k *keyWrapper) Algorithm() jose.SignatureAlgorithm {
	return jose.RS256
}

func (k *keyWrapper) Use() string {
	return "sig"
}

func (k *keyWrapper) Key() interface{} {
	return k.key.PublicKey
}

// =============================================================================
// Helpers
// =============================================================================

func generateSecureSecret(length int) string {
	bytes := make([]byte, length)
	rand.Read(bytes)
	return base64.RawURLEncoding.EncodeToString(bytes)[:length]
}

// getClientIDFromRequest extracts client ID from a token request
func getClientIDFromRequest(request op.TokenRequest) string {
	if ar, ok := request.(*AuthRequest); ok {
		return ar.GetClientID()
	}
	if rtr, ok := request.(*RefreshTokenRequest); ok {
		return rtr.GetClientID()
	}
	// Try to get audience as fallback
	aud := request.GetAudience()
	if len(aud) > 0 {
		return aud[0]
	}
	return ""
}

// getAuthTimeFromRequest extracts auth time from a token request
func getAuthTimeFromRequest(request op.TokenRequest) time.Time {
	if ar, ok := request.(*AuthRequest); ok {
		return ar.GetAuthTime()
	}
	if rtr, ok := request.(*RefreshTokenRequest); ok {
		return rtr.GetAuthTime()
	}
	return time.Now()
}

// =============================================================================
// Device Authorization (not implemented - stubs)
// =============================================================================

func (s *Storage) StoreDeviceAuthorization(ctx context.Context, clientID, deviceCode, userCode string, expires time.Time, scopes []string) error {
	return fmt.Errorf("device authorization not supported")
}

func (s *Storage) GetDeviceAuthorizatonState(ctx context.Context, clientID, deviceCode string) (*op.DeviceAuthorizationState, error) {
	return nil, fmt.Errorf("device authorization not supported")
}

func (s *Storage) GetDeviceAuthorizationByUserCode(ctx context.Context, userCode string) (*op.DeviceAuthorizationState, error) {
	return nil, fmt.Errorf("device authorization not supported")
}

func (s *Storage) CompleteDeviceAuthorization(ctx context.Context, userCode, subject string) error {
	return fmt.Errorf("device authorization not supported")
}

func (s *Storage) DenyDeviceAuthorization(ctx context.Context, userCode string) error {
	return fmt.Errorf("device authorization not supported")
}

// =============================================================================
// Client Credentials (not implemented - stubs)
// =============================================================================

func (s *Storage) ClientCredentials(ctx context.Context, clientID, clientSecret string) (op.Client, error) {
	return s.GetClientByClientID(ctx, clientID)
}

func (s *Storage) ClientCredentialsTokenRequest(ctx context.Context, clientID string, scopes []string) (op.TokenRequest, error) {
	return nil, fmt.Errorf("client credentials not supported")
}

// =============================================================================
// UserInfo claims customization
// =============================================================================

func (s *Storage) GetUserinfoFromScopes(ctx context.Context, userID, clientID string, scopes []string) (*oidc.UserInfo, error) {
	userinfo := &oidc.UserInfo{
		Subject: userID,
	}

	if err := s.SetUserinfoFromScopes(ctx, userinfo, userID, clientID, scopes); err != nil {
		return nil, err
	}

	return userinfo, nil
}

// SupportedUILocales returns supported UI locales
func (s *Storage) SupportedUILocales() []language.Tag {
	return []language.Tag{language.English}
}
