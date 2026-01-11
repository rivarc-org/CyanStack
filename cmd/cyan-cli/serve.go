package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cyanstack/cyanstack/internal/api"
	"github.com/cyanstack/cyanstack/internal/drivers/local"
	"github.com/cyanstack/cyanstack/internal/orchestrator"
	"github.com/cyanstack/cyanstack/internal/security"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the CyanStack API server",
	Long: `Starts the REST API server for CyanStack.

The server provides endpoints for:
- Project management (CRUD)
- Revision management (history & state)
- Cryptography helpers (encrypt/decrypt secrets)
- Deployment & actions
- WebSocket log streaming
- Live deployment events
- Plugin library`,
	RunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetInt("port")
		pluginPath, _ := cmd.Flags().GetString("plugins")

		// 1. Initialize database
		db, err := sqlite.Open()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		if err := sqlite.RunMigrations(db); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}
		fmt.Printf("Database: %s\n", db.Path())

		// 2. Initialize driver
		driver, err := local.NewDriver()
		if err != nil {
			return fmt.Errorf("failed to create driver: %w", err)
		}
		fmt.Println("Docker driver initialized")

		// 3. Initialize crypto for secrets
		ks := security.NewKeyStore("")
		crypto, err := ks.NewCrypto()
		if err != nil {
			return fmt.Errorf("failed to create crypto: %w", err)
		}
		fmt.Println("Crypto initialized")

		// 4. Resolve plugin path
		if pluginPath == "" {
			testdataPaths := []string{
				filepath.Join(".", "testdata", "plugins"),
				filepath.Join("..", "..", "testdata", "plugins"),
			}
			for _, p := range testdataPaths {
				if _, err := os.Stat(p); err == nil {
					pluginPath = p
					break
				}
			}
		}
		if pluginPath == "" {
			return fmt.Errorf("could not find plugins directory - specify with --plugins flag")
		}
		fmt.Printf("Plugin path: %s\n", pluginPath)

		// 5. Initialize orchestration components
		pluginLoader := orchestrator.NewFilePluginLoader(pluginPath)
		portAllocator := orchestrator.NewSimplePortAllocator()
		dagResolver := orchestrator.NewDAGResolver()
		netManager := local.NewNetworkManager(driver.Client())
		traefikGen := local.NewTraefikGenerator()

		envResolver := orchestrator.NewEnvResolver(
			crypto,
			pluginLoader,
			portAllocator,
		)

		// Create repositories
		projectRepo := sqlite.NewProjectRepository(db)
		revisionRepo := sqlite.NewRevisionRepository(db)

		// Create coordinator with RevisionStore and ProjectStore adapters
		coordinator := orchestrator.NewCoordinator(orchestrator.CoordinatorConfig{
			Driver:         driver,
			NetworkManager: netManager,
			TraefikGen:     traefikGen,
			DAGResolver:    dagResolver,
			EnvResolver:    envResolver,
			RevisionStore:  NewRevisionStoreAdapter(revisionRepo),
			ProjectStore:   NewProjectStoreAdapter(projectRepo),
			StartupTimeout: 2 * time.Minute,
		})

		// 6. Create API server
		server := api.NewServer(api.ServerConfig{
			ListenAddr:   fmt.Sprintf(":%d", port),
			ProjectRepo:  projectRepo,
			RevisionRepo: revisionRepo,
			Coordinator:  coordinator,
			Driver:       driver,
			Crypto:       crypto,
			PluginLoader: pluginLoader,
			PluginPath:   pluginPath,
		})

		// 7. Setup graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			<-sigCh
			fmt.Println("\nReceived shutdown signal, stopping server...")
			cancel()
			if err := server.Shutdown(ctx); err != nil {
				fmt.Printf("Error during shutdown: %v\n", err)
			}
		}()

		// 8. Start server
		fmt.Printf("\nCyanStack API Server starting on http://localhost:%d\n", port)
		fmt.Println("Press Ctrl+C to stop")
		fmt.Println()

		if err := server.Run(); err != nil && err.Error() != "http: Server closed" {
			return fmt.Errorf("server error: %w", err)
		}

		fmt.Println("Server stopped")
		return nil
	},
}

func init() {
	serveCmd.Flags().IntP("port", "p", 8080, "Port to listen on")
	serveCmd.Flags().StringP("plugins", "", "", "Path to plugins directory")
}

// RevisionStoreAdapter adapts RevisionRepository to the RevisionStore interface
type RevisionStoreAdapter struct {
	repo *sqlite.RevisionRepository
}

func NewRevisionStoreAdapter(repo *sqlite.RevisionRepository) *RevisionStoreAdapter {
	return &RevisionStoreAdapter{repo: repo}
}

func (a *RevisionStoreAdapter) GetByID(ctx context.Context, id string) (*orchestrator.RevisionData, error) {
	rev, err := a.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rev == nil {
		return nil, nil
	}
	return &orchestrator.RevisionData{
		ID:         rev.ID,
		ProjectID:  rev.ProjectID,
		GraphState: rev.GraphState,
	}, nil
}

// ProjectStoreAdapter adapts ProjectRepository to the ProjectStore interface
type ProjectStoreAdapter struct {
	repo *sqlite.ProjectRepository
}

func NewProjectStoreAdapter(repo *sqlite.ProjectRepository) *ProjectStoreAdapter {
	return &ProjectStoreAdapter{repo: repo}
}

func (a *ProjectStoreAdapter) GetByID(ctx context.Context, id string) (*orchestrator.ProjectData, error) {
	proj, err := a.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if proj == nil {
		return nil, nil
	}
	return &orchestrator.ProjectData{
		ID:   proj.ID,
		Name: proj.Name,
	}, nil
}

func (a *ProjectStoreAdapter) SetCurrentRevision(ctx context.Context, projectID, revisionID string) error {
	return a.repo.SetCurrentRevision(ctx, projectID, revisionID)
}

func (a *RevisionStoreAdapter) MarkDeployed(ctx context.Context, id string) error {
	return a.repo.MarkDeployed(ctx, id)
}
