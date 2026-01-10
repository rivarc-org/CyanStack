package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/cyanstack/cyanstack/internal/api/events"
	"github.com/cyanstack/cyanstack/internal/api/handlers"
	"github.com/cyanstack/cyanstack/internal/api/middleware"
	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
	"github.com/cyanstack/cyanstack/internal/orchestrator"
	"github.com/cyanstack/cyanstack/internal/security"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
	"github.com/gin-gonic/gin"
)

// Server is the API server
type Server struct {
	router       *gin.Engine
	httpServer   *http.Server
	projectRepo  *sqlite.ProjectRepository
	revisionRepo *sqlite.RevisionRepository
	coordinator  *orchestrator.Coordinator
	driver       interfaces.RuntimeDriver
	crypto       *security.AESCrypto
	pluginLoader orchestrator.PluginLoader
	eventBus     *events.InMemoryEventBus
	pluginPath   string
	listenAddr   string
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
		pluginPath:   cfg.PluginPath,
		listenAddr:   cfg.ListenAddr,
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
