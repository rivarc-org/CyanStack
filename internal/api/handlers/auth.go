package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authoidc "github.com/cyanstack/cyanstack/internal/auth/oidc"
)

// AuthAPIHandler handles authentication-related API endpoints
type AuthAPIHandler struct {
	provider *authoidc.Provider
}

// NewAuthAPIHandler creates a new auth API handler
func NewAuthAPIHandler(provider *authoidc.Provider) *AuthAPIHandler {
	return &AuthAPIHandler{
		provider: provider,
	}
}

// CurrentUserResponse is the response for the current user endpoint
type CurrentUserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// GetCurrentUser returns the currently authenticated user
func (h *AuthAPIHandler) GetCurrentUser(c *gin.Context) {
	// Check for session cookie
	sessionID, err := c.Cookie("cyan_session")
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	// Get session from storage
	session, err := h.provider.GetSession(c.Request.Context(), sessionID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "session expired"})
		return
	}

	// Get user by ID
	user, err := authoidc.GetUserByID(c.Request.Context(), h.provider.Storage().DB(), session.UserID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
		return
	}

	c.JSON(http.StatusOK, CurrentUserResponse{
		ID:    user.ID,
		Email: user.Email,
	})
}

// LoginRequest is the request body for the login endpoint
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

// Login authenticates a user and creates a session
func (h *AuthAPIHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Validate credentials
	user, err := authoidc.ValidateCredentials(c.Request.Context(), h.provider.Storage().DB(), req.Email, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid email or password"})
		return
	}

	// Create session
	sessionID := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	if err := h.provider.CreateSession(c.Request.Context(), sessionID, user.ID, expiresAt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create session"})
		return
	}

	// Set session cookie
	c.SetCookie("cyan_session", sessionID, int(24*time.Hour.Seconds()), "/", "", false, true)

	// Return user info
	c.JSON(http.StatusOK, CurrentUserResponse{
		ID:    user.ID,
		Email: user.Email,
	})
}

// Logout clears the session and cookie
func (h *AuthAPIHandler) Logout(c *gin.Context) {
	// Delete session from database if cookie exists
	if sessionID, err := c.Cookie("cyan_session"); err == nil {
		h.provider.DeleteSession(c.Request.Context(), sessionID)
	}

	// Clear the session cookie
	c.SetCookie("cyan_session", "", -1, "/", "", false, true)

	c.Status(http.StatusNoContent)
}
