package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	oauth2Config *oauth2.Config
	oidcVerifier *oidc.IDTokenVerifier
	stateStore   = make(map[string]bool) // Simple in-memory state store
)

func main() {
	// Read OIDC config from environment (as injected by CyanStack)
	clientID := getEnv("OIDC_CLIENT_ID", "")
	clientSecret := getEnv("OIDC_CLIENT_SECRET", "")
	issuerURL := getEnv("OIDC_ISSUER_URL", "http://localhost:8080")
	port := getEnv("PORT", "3000")

	if clientID == "" || clientSecret == "" {
		log.Println("Warning: OIDC_CLIENT_ID or OIDC_CLIENT_SECRET not set")
		log.Println("Running in demo mode - you'll need to register a client manually")
	}

	// Initialize OIDC provider
	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		log.Printf("Warning: Failed to get OIDC provider from %s: %v", issuerURL, err)
		log.Println("The IdP may not be running yet. Start cyan-cli serve first.")
	} else {
		log.Printf("OIDC Provider initialized: %s", issuerURL)

		// Configure OAuth2
		oauth2Config = &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  fmt.Sprintf("http://localhost:%s/auth/callback", port),
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		}

		oidcVerifier = provider.Verifier(&oidc.Config{ClientID: clientID})
	}

	// HTTP handlers
	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/auth/callback", handleCallback)
	http.HandleFunc("/protected", handleProtected)
	http.HandleFunc("/logout", handleLogout)

	log.Printf("Test OIDC Client listening on http://localhost:%s", port)
	log.Printf("Click 'Login with CyanStack' to start the SSO flow")
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	state := base64.RawURLEncoding.EncodeToString(b)
	stateStore[state] = true
	return state
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	tmpl := template.Must(template.New("index").Parse(indexHTML))
	data := map[string]interface{}{
		"LoggedIn":     false,
		"OAuth2Config": oauth2Config != nil,
	}
	tmpl.Execute(w, data)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	if oauth2Config == nil {
		http.Error(w, "OIDC not configured. Set OIDC_CLIENT_ID and OIDC_CLIENT_SECRET environment variables.", http.StatusServiceUnavailable)
		return
	}

	// Generate state for CSRF protection
	state := generateState()

	// Redirect to IdP authorize endpoint
	authURL := oauth2Config.AuthCodeURL(state)
	log.Printf("Redirecting to: %s", authURL)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func handleCallback(w http.ResponseWriter, r *http.Request) {
	if oauth2Config == nil {
		http.Error(w, "OIDC not configured", http.StatusServiceUnavailable)
		return
	}

	ctx := r.Context()

	// Verify state
	state := r.URL.Query().Get("state")
	if !stateStore[state] {
		http.Error(w, "Invalid state parameter", http.StatusBadRequest)
		return
	}
	delete(stateStore, state)

	// Check for errors
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		http.Error(w, fmt.Sprintf("Authorization error: %s - %s", errParam, errDesc), http.StatusBadRequest)
		return
	}

	// Exchange code for token
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "No authorization code received", http.StatusBadRequest)
		return
	}

	token, err := oauth2Config.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "Failed to exchange token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Extract and verify ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "No id_token in response", http.StatusInternalServerError)
		return
	}

	idToken, err := oidcVerifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, "Failed to verify ID token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Extract claims
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Subject       string `json:"sub"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "Failed to parse claims: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Render success page
	tmpl := template.Must(template.New("success").Parse(successHTML))
	data := map[string]interface{}{
		"Email":       claims.Email,
		"Subject":     claims.Subject,
		"Name":        claims.Name,
		"AccessToken": token.AccessToken[:min(20, len(token.AccessToken))] + "...",
		"IDToken":     rawIDToken[:min(50, len(rawIDToken))] + "...",
	}
	tmpl.Execute(w, data)
}

func handleProtected(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"message": "This is a protected resource",
		"status":  "authenticated",
	})
}

func handleLogout(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/", http.StatusFound)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Test OIDC Client</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .container {
            background: #1e293b;
            border-radius: 16px;
            padding: 40px;
            max-width: 500px;
            width: 100%;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
            border: 1px solid #334155;
            text-align: center;
        }
        h1 {
            color: #f472b6;
            font-size: 24px;
            margin-bottom: 8px;
        }
        .subtitle {
            color: #94a3b8;
            font-size: 14px;
            margin-bottom: 32px;
        }
        .login-btn {
            display: inline-block;
            padding: 14px 32px;
            background: linear-gradient(135deg, #22d3ee 0%, #06b6d4 100%);
            color: #0f172a;
            text-decoration: none;
            border-radius: 8px;
            font-weight: 600;
            font-size: 16px;
            transition: transform 0.2s, box-shadow 0.2s;
        }
        .login-btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 10px 20px -10px rgba(34, 211, 238, 0.4);
        }
        .info {
            margin-top: 24px;
            padding: 16px;
            background: #334155;
            border-radius: 8px;
            text-align: left;
        }
        .info p {
            color: #94a3b8;
            font-size: 13px;
            margin-bottom: 8px;
        }
        .info code {
            background: #1e293b;
            padding: 2px 6px;
            border-radius: 4px;
            color: #22d3ee;
        }
        .warning {
            color: #fbbf24;
            margin-top: 16px;
            font-size: 14px;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Test OIDC Client</h1>
        <p class="subtitle">CyanStack SSO Demo</p>

        {{if .OAuth2Config}}
        <a href="/login" class="login-btn">Login with CyanStack</a>
        {{else}}
        <p class="warning">OIDC not configured. Set environment variables:</p>
        <div class="info">
            <p><code>OIDC_CLIENT_ID</code> - Client ID from CyanStack</p>
            <p><code>OIDC_CLIENT_SECRET</code> - Client secret</p>
            <p><code>OIDC_ISSUER_URL</code> - IdP URL (default: http://localhost:8080)</p>
        </div>
        {{end}}

        <div class="info">
            <p>This is a test OIDC client that demonstrates the SSO flow:</p>
            <p>1. Click "Login with CyanStack"</p>
            <p>2. You'll be redirected to the IdP</p>
            <p>3. Login with <code>test@test.com</code> / <code>test</code></p>
            <p>4. You'll be redirected back with tokens</p>
        </div>
    </div>
</body>
</html>`

const successHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Login Successful</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%);
            min-height: 100vh;
            display: flex;
            align-items: center;
            justify-content: center;
            padding: 20px;
        }
        .container {
            background: #1e293b;
            border-radius: 16px;
            padding: 40px;
            max-width: 600px;
            width: 100%;
            box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
            border: 1px solid #334155;
        }
        .success-badge {
            background: #22c55e;
            color: white;
            padding: 8px 16px;
            border-radius: 20px;
            font-size: 14px;
            font-weight: 600;
            display: inline-block;
            margin-bottom: 24px;
        }
        h1 {
            color: #f1f5f9;
            font-size: 24px;
            margin-bottom: 24px;
        }
        .field {
            margin-bottom: 16px;
        }
        .field label {
            color: #94a3b8;
            font-size: 12px;
            text-transform: uppercase;
            letter-spacing: 0.5px;
            display: block;
            margin-bottom: 4px;
        }
        .field .value {
            color: #f1f5f9;
            font-size: 16px;
            word-break: break-all;
        }
        .token-field .value {
            background: #334155;
            padding: 12px;
            border-radius: 8px;
            font-family: monospace;
            font-size: 12px;
            color: #22d3ee;
        }
        .logout-btn {
            display: inline-block;
            margin-top: 24px;
            padding: 12px 24px;
            background: #475569;
            color: white;
            text-decoration: none;
            border-radius: 8px;
            font-weight: 500;
        }
        .logout-btn:hover {
            background: #64748b;
        }
    </style>
</head>
<body>
    <div class="container">
        <span class="success-badge">✓ Authenticated</span>
        <h1>SSO Login Successful!</h1>

        <div class="field">
            <label>Email</label>
            <div class="value">{{.Email}}</div>
        </div>

        <div class="field">
            <label>Subject (User ID)</label>
            <div class="value">{{.Subject}}</div>
        </div>

        <div class="field token-field">
            <label>Access Token (truncated)</label>
            <div class="value">{{.AccessToken}}</div>
        </div>

        <div class="field token-field">
            <label>ID Token (truncated)</label>
            <div class="value">{{.IDToken}}</div>
        </div>

        <a href="/logout" class="logout-btn">Logout</a>
    </div>
</body>
</html>`
