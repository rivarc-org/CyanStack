package api

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/cyanstack/cyanstack/internal/api/events"
	"github.com/cyanstack/cyanstack/internal/api/handlers"
	"github.com/cyanstack/cyanstack/internal/api/middleware"
	authoidc "github.com/cyanstack/cyanstack/internal/auth/oidc"
	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
	"github.com/cyanstack/cyanstack/internal/orchestrator"
	"github.com/cyanstack/cyanstack/internal/security"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
	"github.com/cyanstack/cyanstack/internal/ui"
	"github.com/gin-gonic/gin"
)

// Server is the API server
type Server struct {
	router        *gin.Engine
	httpServer    *http.Server
	projectRepo   *sqlite.ProjectRepository
	revisionRepo  *sqlite.RevisionRepository
	coordinator   *orchestrator.Coordinator
	driver        interfaces.RuntimeDriver
	crypto        *security.AESCrypto
	pluginLoader  orchestrator.PluginLoader
	eventBus      *events.InMemoryEventBus
	oidcProvider  *authoidc.Provider
	pluginPath    string
	listenAddr    string
	headless      bool
}

// ServerConfig contains the configuration for creating a new Server
type ServerConfig struct {
	ListenAddr   string
	ProjectRepo  *sqlite.ProjectRepository
	RevisionRepo *sqlite.RevisionRepository
	Coordinator  *orchestrator.Coordinator
	Driver       interfaces.RuntimeDriver
	Crypto       *security.AESCrypto
	PluginLoader orchestrator.PluginLoader
	PluginPath   string
	OIDCProvider *authoidc.Provider
	Headless     bool // If true, don't serve the embedded UI
}

// NewServer creates a new API server
func NewServer(cfg ServerConfig) *Server {
	gin.SetMode(gin.ReleaseMode)

	router := gin.New()
	router.Use(gin.Logger())
	router.Use(middleware.ErrorHandler())
	router.Use(middleware.CORS())

	eventBus := events.NewInMemoryEventBus()

	s := &Server{
		router:       router,
		projectRepo:  cfg.ProjectRepo,
		revisionRepo: cfg.RevisionRepo,
		coordinator:  cfg.Coordinator,
		driver:       cfg.Driver,
		crypto:       cfg.Crypto,
		pluginLoader: cfg.PluginLoader,
		eventBus:     eventBus,
		oidcProvider: cfg.OIDCProvider,
		pluginPath:   cfg.PluginPath,
		listenAddr:   cfg.ListenAddr,
		headless:     cfg.Headless,
	}

	s.setupRoutes()

	return s
}

// setupRoutes configures all API routes
func (s *Server) setupRoutes() {
	v1 := s.router.Group("/api/v1")

	// Project routes
	projectHandler := handlers.NewProjectHandler(s.projectRepo, s.revisionRepo, s.coordinator)
	v1.GET("/projects", projectHandler.List)
	v1.POST("/projects", projectHandler.Create)
	v1.GET("/projects/:id", projectHandler.Get)
	v1.DELETE("/projects/:id", projectHandler.Delete)

	// Revision routes
	revisionHandler := handlers.NewRevisionHandler(s.revisionRepo, s.projectRepo)
	v1.GET("/projects/:id/revisions", revisionHandler.List)
	v1.GET("/projects/:id/revisions/:rev_id", revisionHandler.Get)
	v1.POST("/projects/:id/revisions", revisionHandler.Create)
	v1.POST("/projects/:id/revisions/:rev_id", revisionHandler.Update)
	v1.DELETE("/projects/:id/revisions/:rev_id", revisionHandler.Delete)

	// Crypto routes
	cryptoHandler := handlers.NewCryptoHandler(s.crypto)
	v1.POST("/crypto/encrypt", cryptoHandler.Encrypt)
	v1.POST("/crypto/decrypt", cryptoHandler.Decrypt)
	v1.POST("/crypto/generate", cryptoHandler.Generate)

	// Deploy routes
	deployHandler := handlers.NewDeployHandler(s.projectRepo, s.revisionRepo, s.coordinator, s.eventBus)
	v1.POST("/projects/:id/deploy", deployHandler.Deploy)
	v1.POST("/projects/:id/stop", deployHandler.Stop)

	// WebSocket routes
	logsHandler := handlers.NewLogsHandler(s.driver)
	v1.GET("/projects/:id/containers/:node_id/logs", logsHandler.StreamLogs)

	eventsHandler := handlers.NewEventsHandler(s.eventBus)
	v1.GET("/projects/:id/events", eventsHandler.StreamEvents)

	// Plugin library routes
	libraryHandler := handlers.NewLibraryHandler(s.pluginPath)
	v1.GET("/library", libraryHandler.List)
	v1.GET("/library/:plugin_id", libraryHandler.Get)

	// OIDC routes (if provider is configured)
	if s.oidcProvider != nil {
		s.setupOIDCRoutes()
	}

	// Serve embedded frontend static files (unless running in headless mode)
	if !s.headless {
		s.setupStaticFileServing()
	}
}

// setupOIDCRoutes configures OIDC/OAuth2 endpoints
func (s *Server) setupOIDCRoutes() {
	oidcHandler := s.oidcProvider.Handler()

	// Standard OIDC endpoints (handled by zitadel/oidc)
	// These must be at the root, not under /api/v1
	s.router.Any("/.well-known/*path", gin.WrapH(oidcHandler))
	s.router.Any("/authorize", gin.WrapH(oidcHandler))
	s.router.Any("/authorize/callback", gin.WrapH(oidcHandler))
	s.router.Any("/oauth/token", gin.WrapH(oidcHandler))
	s.router.Any("/userinfo", gin.WrapH(oidcHandler))
	s.router.Any("/keys", gin.WrapH(oidcHandler))
	s.router.Any("/end_session", gin.WrapH(oidcHandler))

	// Auth API endpoints (under /api/v1)
	authAPIHandler := handlers.NewAuthAPIHandler(s.oidcProvider)
	v1 := s.router.Group("/api/v1")
	v1.GET("/auth/me", authAPIHandler.GetCurrentUser)
	v1.POST("/auth/login", authAPIHandler.Login)
	v1.POST("/auth/logout", authAPIHandler.Logout)
}

// setupStaticFileServing configures serving of embedded frontend assets with SPA fallback.
// This serves the React frontend from the embedded filesystem and handles client-side routing
// by serving index.html for any route that doesn't match an API endpoint or static file.
func (s *Server) setupStaticFileServing() {
	frontendFS := ui.GetFS()

	// Serve static assets (JS, CSS, images, etc.)
	s.router.Use(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Skip API routes - they are handled by registered handlers
		if strings.HasPrefix(path, "/api/") {
			c.Next()
			return
		}

		// Skip OIDC routes
		if strings.HasPrefix(path, "/.well-known/") ||
			path == "/authorize" || strings.HasPrefix(path, "/authorize/") ||
			path == "/oauth/token" || path == "/userinfo" || path == "/keys" ||
			path == "/end_session" {
			c.Next()
			return
		}

		// Try to serve static file
		filePath := strings.TrimPrefix(path, "/")
		if filePath == "" {
			filePath = "index.html"
		}

		// Check if file exists in embedded filesystem
		file, err := frontendFS.Open(filePath)
		if err == nil {
			file.Close()
			c.FileFromFS(path, http.FS(frontendFS))
			c.Abort()
			return
		}

		// For SPA routing: if not a file request (no extension or not found),
		// serve index.html to let React Router handle it
		if !strings.Contains(filePath, ".") || isNotFoundError(err) {
			c.FileFromFS("/index.html", http.FS(frontendFS))
			c.Abort()
			return
		}

		c.Next()
	})
}

// isNotFoundError checks if the error is a file not found error
func isNotFoundError(err error) bool {
	return err != nil && (err == fs.ErrNotExist || strings.Contains(err.Error(), "not found"))
}

// Router returns the Gin router for testing
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Run starts the HTTP server
func (s *Server) Run() error {
	s.httpServer = &http.Server{
		Addr:    s.listenAddr,
		Handler: s.router,
	}

	fmt.Printf("Starting CyanStack API server on %s\n", s.listenAddr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.eventBus.Close()

	if s.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	}
	return nil
}
