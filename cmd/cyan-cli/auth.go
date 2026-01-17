package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	authoidc "github.com/cyanstack/cyanstack/internal/auth/oidc"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "OIDC authentication commands",
	Long:  `Commands for testing and managing OIDC authentication.`,
}

var authTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test OIDC authentication flow",
	Long: `Starts a temporary HTTP server, registers a test OIDC client,
and initiates an authorization code flow to verify the OIDC provider is working.

This command:
1. Connects to the running CyanStack server
2. Registers a test OIDC client dynamically
3. Opens your browser to the authorize endpoint
4. Handles the callback and exchanges the code for tokens
5. Verifies the received tokens

Make sure 'cyan-cli serve' is running before using this command.`,
	RunE: runAuthTest,
}

func init() {
	authCmd.AddCommand(authTestCmd)

	authTestCmd.Flags().StringP("issuer", "i", "http://localhost:8080", "OIDC issuer URL")
	authTestCmd.Flags().IntP("callback-port", "p", 9999, "Local port for OAuth callback")
	authTestCmd.Flags().BoolP("no-browser", "n", false, "Don't open browser automatically")
}

func runAuthTest(cmd *cobra.Command, args []string) error {
	issuerURL, _ := cmd.Flags().GetString("issuer")
	callbackPort, _ := cmd.Flags().GetInt("callback-port")
	noBrowser, _ := cmd.Flags().GetBool("no-browser")

	fmt.Println("=== CyanStack OIDC Authentication Test ===")
	fmt.Println()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// 1. Discover OIDC configuration
	fmt.Printf("Discovering OIDC provider at %s...\n", issuerURL)
	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return fmt.Errorf("failed to discover OIDC provider: %w\n\nMake sure 'cyan-cli serve' is running", err)
	}
	fmt.Println("  OIDC provider discovered successfully")

	// 2. Generate test client credentials
	// For the test, we'll use a pre-registered client or register one via API
	clientID := generateTestClientID()
	clientSecret := generateTestSecret()
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", callbackPort)

	fmt.Printf("  Test Client ID: %s\n", clientID)
	fmt.Printf("  Callback URL: %s\n", redirectURI)
	fmt.Println()

	// 3. Configure OAuth2
	oauth2Config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	// 4. Create verifier
	verifier := provider.Verifier(&oidc.Config{
		ClientID: clientID,
		// Skip client ID verification since we use a test client
		SkipClientIDCheck: true,
	})

	// 5. Generate state for CSRF protection
	state := generateState()

	// 6. Setup callback server
	resultCh := make(chan *authResult, 1)
	server := startCallbackServer(callbackPort, oauth2Config, verifier, state, resultCh)
	defer server.Shutdown(ctx)

	// 7. Build and display authorization URL
	authURL := oauth2Config.AuthCodeURL(state)
	fmt.Println("Authorization URL:")
	fmt.Printf("  %s\n", authURL)
	fmt.Println()

	// 8. Open browser or prompt user
	if !noBrowser {
		fmt.Println("Opening browser for authentication...")
		if err := openBrowser(authURL); err != nil {
			fmt.Printf("Could not open browser: %v\n", err)
			fmt.Println("Please open the URL manually")
		}
	} else {
		fmt.Println("Please open the URL in your browser to authenticate")
	}

	fmt.Println()
	fmt.Println("Waiting for authentication callback...")
	fmt.Printf("(Login with: %s / %s)\n", "test@test.com", "test")
	fmt.Println()

	// 9. Wait for result
	select {
	case result := <-resultCh:
		if result.err != nil {
			return fmt.Errorf("authentication failed: %w", result.err)
		}

		fmt.Println("=== Authentication Successful ===")
		fmt.Println()
		fmt.Printf("Subject (User ID): %s\n", result.subject)
		fmt.Printf("Email: %s\n", result.email)
		fmt.Printf("Name: %s\n", result.name)
		fmt.Println()
		fmt.Printf("Access Token: %s...\n", truncateToken(result.accessToken, 40))
		fmt.Printf("ID Token: %s...\n", truncateToken(result.idToken, 50))
		if result.refreshToken != "" {
			fmt.Printf("Refresh Token: %s...\n", truncateToken(result.refreshToken, 30))
		}
		fmt.Println()
		fmt.Println("OIDC flow completed successfully!")
		return nil

	case <-ctx.Done():
		return fmt.Errorf("authentication timed out")
	}
}

type authResult struct {
	accessToken  string
	idToken      string
	refreshToken string
	subject      string
	email        string
	name         string
	err          error
}

func startCallbackServer(port int, config *oauth2.Config, verifier *oidc.IDTokenVerifier, expectedState string, resultCh chan<- *authResult) *http.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		result := &authResult{}

		// Check state
		if r.URL.Query().Get("state") != expectedState {
			result.err = fmt.Errorf("invalid state parameter")
			resultCh <- result
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}

		// Check for errors
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			errDesc := r.URL.Query().Get("error_description")
			result.err = fmt.Errorf("%s: %s", errParam, errDesc)
			resultCh <- result
			http.Error(w, "Authentication error: "+errParam, http.StatusBadRequest)
			return
		}

		// Get authorization code
		code := r.URL.Query().Get("code")
		if code == "" {
			result.err = fmt.Errorf("no authorization code received")
			resultCh <- result
			http.Error(w, "No code received", http.StatusBadRequest)
			return
		}

		// Exchange code for tokens
		ctx := r.Context()
		token, err := config.Exchange(ctx, code)
		if err != nil {
			result.err = fmt.Errorf("token exchange failed: %w", err)
			resultCh <- result
			http.Error(w, "Token exchange failed", http.StatusInternalServerError)
			return
		}

		result.accessToken = token.AccessToken
		result.refreshToken = token.RefreshToken

		// Extract and verify ID token
		rawIDToken, ok := token.Extra("id_token").(string)
		if !ok {
			result.err = fmt.Errorf("no id_token in response")
			resultCh <- result
			http.Error(w, "No ID token", http.StatusInternalServerError)
			return
		}
		result.idToken = rawIDToken

		idToken, err := verifier.Verify(ctx, rawIDToken)
		if err != nil {
			result.err = fmt.Errorf("ID token verification failed: %w", err)
			resultCh <- result
			http.Error(w, "Token verification failed", http.StatusInternalServerError)
			return
		}

		// Extract claims
		var claims struct {
			Email   string `json:"email"`
			Name    string `json:"name"`
			Subject string `json:"sub"`
		}
		if err := idToken.Claims(&claims); err != nil {
			result.err = fmt.Errorf("failed to parse claims: %w", err)
			resultCh <- result
			http.Error(w, "Failed to parse claims", http.StatusInternalServerError)
			return
		}

		result.subject = claims.Subject
		result.email = claims.Email
		result.name = claims.Name

		// Send success response
		resultCh <- result

		// Return HTML success page
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Authentication Successful</title>
    <style>
        body { font-family: system-ui, sans-serif; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; background: linear-gradient(135deg, #1a1a2e 0%%, #16213e 100%%); color: #f1f5f9; }
        .container { text-align: center; padding: 40px; background: #1e293b; border-radius: 16px; box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5); }
        h1 { color: #22d3ee; margin-bottom: 16px; }
        p { color: #94a3b8; }
        .email { color: #f472b6; font-weight: 600; }
    </style>
</head>
<body>
    <div class="container">
        <h1>✓ Authentication Successful</h1>
        <p>Logged in as <span class="email">%s</span></p>
        <p>You can close this window and return to the terminal.</p>
    </div>
</body>
</html>`, claims.Email)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// Check if port is available
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		fmt.Printf("Warning: Could not bind to port %d: %v\n", port, err)
		return server
	}

	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			fmt.Printf("Callback server error: %v\n", err)
		}
	}()

	return server
}

func generateTestClientID() string {
	return authoidc.TestCLIClientID
}

func generateTestSecret() string {
	return authoidc.TestCLIClientSecret
}

func generateState() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func truncateToken(token string, maxLen int) string {
	if len(token) <= maxLen {
		return token
	}
	return token[:maxLen]
}

func openBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // Linux and others
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}

// authDiscoverCmd shows OIDC discovery information
var authDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Show OIDC discovery information",
	Long:  `Fetches and displays the OpenID Connect discovery document from the server.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		issuerURL, _ := cmd.Flags().GetString("issuer")

		fmt.Printf("Fetching OIDC discovery from %s...\n\n", issuerURL)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Fetch discovery document
		discoveryURL := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"
		req, _ := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to fetch discovery document: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("discovery endpoint returned status %d", resp.StatusCode)
		}

		var discovery map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
			return fmt.Errorf("failed to parse discovery document: %w", err)
		}

		// Display formatted discovery info
		fmt.Println("=== OIDC Discovery Document ===")
		fmt.Println()

		// Key fields to display
		fields := []string{
			"issuer",
			"authorization_endpoint",
			"token_endpoint",
			"userinfo_endpoint",
			"jwks_uri",
			"end_session_endpoint",
		}

		for _, field := range fields {
			if val, ok := discovery[field]; ok {
				fmt.Printf("%s:\n  %v\n", formatFieldName(field), val)
			}
		}

		fmt.Println()
		fmt.Println("Supported Response Types:")
		if rt, ok := discovery["response_types_supported"].([]interface{}); ok {
			for _, t := range rt {
				fmt.Printf("  - %v\n", t)
			}
		}

		fmt.Println()
		fmt.Println("Supported Scopes:")
		if scopes, ok := discovery["scopes_supported"].([]interface{}); ok {
			for _, s := range scopes {
				fmt.Printf("  - %v\n", s)
			}
		}

		fmt.Println()
		fmt.Println("Supported Grant Types:")
		if grants, ok := discovery["grant_types_supported"].([]interface{}); ok {
			for _, g := range grants {
				fmt.Printf("  - %v\n", g)
			}
		}

		return nil
	},
}

func init() {
	authCmd.AddCommand(authDiscoverCmd)
	authDiscoverCmd.Flags().StringP("issuer", "i", "http://localhost:8080", "OIDC issuer URL")
}

func formatFieldName(field string) string {
	// Convert snake_case to Title Case
	parts := strings.Split(field, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

// authJWKSCmd shows the JSON Web Key Set
var authJWKSCmd = &cobra.Command{
	Use:   "jwks",
	Short: "Show OIDC public keys (JWKS)",
	Long:  `Fetches and displays the JSON Web Key Set used for token verification.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		issuerURL, _ := cmd.Flags().GetString("issuer")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// First get discovery to find JWKS URI
		provider, err := oidc.NewProvider(ctx, issuerURL)
		if err != nil {
			return fmt.Errorf("failed to discover OIDC provider: %w", err)
		}

		// Get JWKS URI from discovery
		var claims struct {
			JWKSURI string `json:"jwks_uri"`
		}
		if err := provider.Claims(&claims); err != nil {
			return fmt.Errorf("failed to get JWKS URI: %w", err)
		}

		jwksURL := claims.JWKSURI
		if jwksURL == "" {
			jwksURL = strings.TrimSuffix(issuerURL, "/") + "/jwks"
		}

		fmt.Printf("Fetching JWKS from %s...\n\n", jwksURL)

		// Fetch JWKS
		req, _ := http.NewRequestWithContext(ctx, "GET", jwksURL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to fetch JWKS: %w", err)
		}
		defer resp.Body.Close()

		var jwks struct {
			Keys []map[string]interface{} `json:"keys"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
			return fmt.Errorf("failed to parse JWKS: %w", err)
		}

		fmt.Println("=== JSON Web Key Set ===")
		fmt.Println()

		for i, key := range jwks.Keys {
			fmt.Printf("Key %d:\n", i+1)
			fmt.Printf("  Key ID (kid): %v\n", key["kid"])
			fmt.Printf("  Algorithm: %v\n", key["alg"])
			fmt.Printf("  Key Type: %v\n", key["kty"])
			fmt.Printf("  Use: %v\n", key["use"])
			if n, ok := key["n"]; ok {
				nStr := fmt.Sprintf("%v", n)
				fmt.Printf("  Modulus (n): %s...\n", truncateToken(nStr, 40))
			}
			fmt.Println()
		}

		return nil
	},
}

func init() {
	authCmd.AddCommand(authJWKSCmd)
	authJWKSCmd.Flags().StringP("issuer", "i", "http://localhost:8080", "OIDC issuer URL")
}

// authUserCmd validates user credentials against the IdP
var authUserCmd = &cobra.Command{
	Use:   "user",
	Short: "Show default test credentials",
	Long:  `Displays the default test user and client credentials for the CyanStack identity provider.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("=== Default Test User ===")
		fmt.Println()
		fmt.Printf("Email:    %s\n", authoidc.DefaultUserEmail)
		fmt.Printf("Password: %s\n", authoidc.DefaultUserPassword)
		fmt.Println()
		fmt.Println("=== Test CLI Client ===")
		fmt.Println()
		fmt.Printf("Client ID:     %s\n", authoidc.TestCLIClientID)
		fmt.Printf("Client Secret: %s\n", authoidc.TestCLIClientSecret)
		fmt.Println()
		fmt.Println("These credentials are automatically created when the server starts.")
		fmt.Println("Use 'cyan-cli auth test' to test the OIDC flow.")
	},
}

func init() {
	authCmd.AddCommand(authUserCmd)
}

// Helper to parse discovery document
func parseDiscoveryEndpoint(issuerURL string, endpoint string) (*url.URL, error) {
	discoveryURL := strings.TrimSuffix(issuerURL, "/") + "/.well-known/openid-configuration"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", discoveryURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var discovery map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, err
	}

	if ep, ok := discovery[endpoint].(string); ok {
		return url.Parse(ep)
	}

	return nil, fmt.Errorf("endpoint %s not found in discovery", endpoint)
}
