package oidc

import (
	"encoding/json"
	"time"

	"github.com/zitadel/oidc/v3/pkg/oidc"
	"github.com/zitadel/oidc/v3/pkg/op"
	"golang.org/x/crypto/bcrypt"

	"github.com/cyanstack/cyanstack/internal/store/sqlite"
)

// Client implements op.Client interface for OIDC
type Client struct {
	model        *sqlite.OIDCClient
	redirectURIs []string
	grantTypes   []oidc.GrantType
	responseTypes []oidc.ResponseType
	scopes       []string
}

// NewClient creates a Client from a database model
func NewClient(model *sqlite.OIDCClient) (*Client, error) {
	c := &Client{model: model}

	// Parse JSON arrays
	if err := json.Unmarshal([]byte(model.RedirectURIs), &c.redirectURIs); err != nil {
		return nil, err
	}

	var grantTypes []string
	if err := json.Unmarshal([]byte(model.GrantTypes), &grantTypes); err != nil {
		return nil, err
	}
	for _, gt := range grantTypes {
		c.grantTypes = append(c.grantTypes, oidc.GrantType(gt))
	}

	var responseTypes []string
	if err := json.Unmarshal([]byte(model.ResponseTypes), &responseTypes); err != nil {
		return nil, err
	}
	for _, rt := range responseTypes {
		c.responseTypes = append(c.responseTypes, oidc.ResponseType(rt))
	}

	if err := json.Unmarshal([]byte(model.Scopes), &c.scopes); err != nil {
		return nil, err
	}

	return c, nil
}

// GetID returns the client's ID
func (c *Client) GetID() string {
	return c.model.ID
}

// RedirectURIs returns the registered redirect URIs
func (c *Client) RedirectURIs() []string {
	return c.redirectURIs
}

// PostLogoutRedirectURIs returns URIs for post-logout redirect
func (c *Client) PostLogoutRedirectURIs() []string {
	return nil
}

// ApplicationType returns the application type (web)
func (c *Client) ApplicationType() op.ApplicationType {
	return op.ApplicationTypeWeb
}

// AuthMethod returns the authentication method for token endpoint
func (c *Client) AuthMethod() oidc.AuthMethod {
	return oidc.AuthMethodBasic
}

// ResponseTypes returns supported response types
func (c *Client) ResponseTypes() []oidc.ResponseType {
	return c.responseTypes
}

// GrantTypes returns supported grant types
func (c *Client) GrantTypes() []oidc.GrantType {
	return c.grantTypes
}

// LoginURL returns the login URL (used by zitadel/oidc)
func (c *Client) LoginURL(id string) string {
	return "/login?auth_request_id=" + id
}

// AccessTokenType returns the access token type
func (c *Client) AccessTokenType() op.AccessTokenType {
	return op.AccessTokenTypeJWT
}

// IDTokenLifetime returns the ID token lifetime
func (c *Client) IDTokenLifetime() time.Duration {
	return time.Hour
}

// DevMode returns whether the client is in dev mode
func (c *Client) DevMode() bool {
	return true // Allow http:// redirect URIs for localhost
}

// RestrictAdditionalIdTokenScopes restricts scopes in ID token
func (c *Client) RestrictAdditionalIdTokenScopes() func(scopes []string) []string {
	return func(scopes []string) []string {
		return scopes
	}
}

// RestrictAdditionalAccessTokenScopes restricts scopes in access token
func (c *Client) RestrictAdditionalAccessTokenScopes() func(scopes []string) []string {
	return func(scopes []string) []string {
		return scopes
	}
}

// IsScopeAllowed checks if a scope is allowed for this client
func (c *Client) IsScopeAllowed(scope string) bool {
	for _, s := range c.scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// IDTokenUserinfoClaimsAssertion returns whether userinfo claims should be in ID token
func (c *Client) IDTokenUserinfoClaimsAssertion() bool {
	return false
}

// ClockSkew returns the allowed clock skew
func (c *Client) ClockSkew() time.Duration {
	return 0
}

// ValidateSecret validates the client secret
func (c *Client) ValidateSecret(secret string) error {
	return bcrypt.CompareHashAndPassword([]byte(c.model.SecretHash), []byte(secret))
}

// ClientCredentials holds the plain text credentials for a newly registered client
type ClientCredentials struct {
	ClientID     string
	ClientSecret string
}

// ClientRegistration contains the data needed to register a new OIDC client
type ClientRegistration struct {
	Name         string
	PluginID     string
	NodeID       string
	ProjectID    string
	RedirectURIs []string
}
