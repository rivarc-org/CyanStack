package auth

import (
	"html/template"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authoidc "github.com/cyanstack/cyanstack/internal/auth/oidc"
)

// Handler handles authentication-related HTTP requests
type Handler struct {
	provider      *authoidc.Provider
	loginTemplate *template.Template
}

// NewHandler creates a new auth handler
func NewHandler(provider *authoidc.Provider) *Handler {
	tmpl := template.Must(template.New("login").Parse(loginHTML))
	return &Handler{
		provider:      provider,
		loginTemplate: tmpl,
	}
}

// LoginPage renders the login form
func (h *Handler) LoginPage(c *gin.Context) {
	// Check for existing session
	if sessionID, err := c.Cookie("cyan_session"); err == nil {
		if session, _ := h.provider.GetSession(c.Request.Context(), sessionID); session != nil {
			// Already logged in, complete the auth request if present
			authRequestID := c.Query("auth_request_id")
			if authRequestID != "" {
				// Complete the auth request and redirect back to authorize
				h.provider.CompleteAuthRequest(c.Request.Context(), authRequestID, session.UserID)
				c.Redirect(http.StatusFound, "/authorize/callback?id="+authRequestID)
				return
			}
			c.Redirect(http.StatusFound, "/")
			return
		}
	}

	// Render login form
	data := map[string]interface{}{
		"AuthRequestID": c.Query("auth_request_id"),
		"Error":         "",
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	h.loginTemplate.Execute(c.Writer, data)
}

// LoginSubmit handles login form submission
func (h *Handler) LoginSubmit(c *gin.Context) {
	email := c.PostForm("email")
	password := c.PostForm("password")
	authRequestID := c.PostForm("auth_request_id")

	// Validate credentials
	user, err := authoidc.ValidateCredentials(c.Request.Context(), h.provider.Storage().DB(), email, password)
	if err != nil {
		data := map[string]interface{}{
			"AuthRequestID": authRequestID,
			"Error":         "Invalid email or password",
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusUnauthorized)
		h.loginTemplate.Execute(c.Writer, data)
		return
	}

	// Create session
	sessionID := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	if err := h.provider.CreateSession(c.Request.Context(), sessionID, user.ID, expiresAt); err != nil {
		data := map[string]interface{}{
			"AuthRequestID": authRequestID,
			"Error":         "Failed to create session",
		}
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusInternalServerError)
		h.loginTemplate.Execute(c.Writer, data)
		return
	}

	// Set session cookie
	c.SetCookie("cyan_session", sessionID, int(24*time.Hour.Seconds()), "/", "", false, true)

	// Complete the auth request if present
	if authRequestID != "" {
		h.provider.CompleteAuthRequest(c.Request.Context(), authRequestID, user.ID)
		c.Redirect(http.StatusFound, "/authorize/callback?id="+authRequestID)
		return
	}

	c.Redirect(http.StatusFound, "/")
}

// Logout clears the session
func (h *Handler) Logout(c *gin.Context) {
	if sessionID, err := c.Cookie("cyan_session"); err == nil {
		h.provider.DeleteSession(c.Request.Context(), sessionID)
	}
	c.SetCookie("cyan_session", "", -1, "/", "", false, true)
	c.Redirect(http.StatusFound, "/login")
}

// loginHTML is the login page template
const loginHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>CyanStack Login</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
            background: linear-gradient(135deg, #0f172a 0%, #1e293b 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .login-container {
            background: #1e293b;
            border-radius: 16px;
            padding: 40px;
            width: 100%;
            max-width: 400px;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.25);
            border: 1px solid #334155;
        }
        .logo {
            text-align: center;
            margin-bottom: 32px;
        }
        .logo h1 {
            color: #22d3ee;
            font-size: 28px;
            font-weight: 700;
        }
        .logo p {
            color: #94a3b8;
            font-size: 14px;
            margin-top: 8px;
        }
        .form-group {
            margin-bottom: 20px;
        }
        label {
            display: block;
            color: #e2e8f0;
            font-size: 14px;
            font-weight: 500;
            margin-bottom: 8px;
        }
        input[type="email"],
        input[type="password"] {
            width: 100%;
            padding: 12px 16px;
            background: #0f172a;
            border: 1px solid #334155;
            border-radius: 8px;
            color: #e2e8f0;
            font-size: 16px;
            transition: border-color 0.2s, box-shadow 0.2s;
        }
        input[type="email"]:focus,
        input[type="password"]:focus {
            outline: none;
            border-color: #22d3ee;
            box-shadow: 0 0 0 3px rgba(34, 211, 238, 0.1);
        }
        input[type="email"]::placeholder,
        input[type="password"]::placeholder {
            color: #64748b;
        }
        .error {
            background: #7f1d1d;
            border: 1px solid #dc2626;
            color: #fca5a5;
            padding: 12px 16px;
            border-radius: 8px;
            font-size: 14px;
            margin-bottom: 20px;
        }
        button {
            width: 100%;
            padding: 14px 24px;
            background: linear-gradient(135deg, #06b6d4 0%, #22d3ee 100%);
            border: none;
            border-radius: 8px;
            color: #0f172a;
            font-size: 16px;
            font-weight: 600;
            cursor: pointer;
            transition: transform 0.2s, box-shadow 0.2s;
        }
        button:hover {
            transform: translateY(-1px);
            box-shadow: 0 10px 20px -10px rgba(34, 211, 238, 0.4);
        }
        button:active {
            transform: translateY(0);
        }
        .hint {
            text-align: center;
            margin-top: 24px;
            color: #64748b;
            font-size: 13px;
        }
        .hint code {
            background: #334155;
            padding: 2px 6px;
            border-radius: 4px;
            color: #94a3b8;
        }
    </style>
</head>
<body>
    <div class="login-container">
        <div class="logo">
            <h1>CyanStack</h1>
            <p>Identity Provider</p>
        </div>

        {{if .Error}}
        <div class="error">{{.Error}}</div>
        {{end}}

        <form method="POST" action="/login">
            <input type="hidden" name="auth_request_id" value="{{.AuthRequestID}}">

            <div class="form-group">
                <label for="email">Email</label>
                <input type="email" id="email" name="email" placeholder="Enter your email" required autofocus>
            </div>

            <div class="form-group">
                <label for="password">Password</label>
                <input type="password" id="password" name="password" placeholder="Enter your password" required>
            </div>

            <button type="submit">Sign In</button>
        </form>

        <div class="hint">
            Test credentials: <code>test@test.com</code> / <code>test</code>
        </div>
    </div>
</body>
</html>`
