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
	authoidc "github.com/cyanstack/cyanstack/internal/auth/oidc"
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
		headless, _ := cmd.Flags().GetBool("headless")

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

		// 5. Initialize OIDC Provider
		issuerURL := fmt.Sprintf("http://localhost:%d", port)

		// Ensure signing key exists
		signingKey, err := authoidc.EnsureSigningKey(context.Background(), db.DB)
		if err != nil {
			return fmt.Errorf("failed to ensure signing key: %w", err)
		}
		fmt.Printf("OIDC signing key: %s\n", signingKey.ID)

		// Create OIDC storage
		oidcStorage := authoidc.NewStorage(db.DB, signingKey)

		// Create OIDC provider (allow HTTP for local development)
		oidcProvider, err := authoidc.NewProvider(authoidc.ProviderConfig{
			Issuer:        issuerURL,
			DB:            db.DB,
			Storage:       oidcStorage,
			AllowInsecure: true, // Allow HTTP for localhost development
		})
		if err != nil {
			return fmt.Errorf("failed to create OIDC provider: %w", err)
		}
		fmt.Println("OIDC provider initialized")

		// Ensure default user exists
		if err := authoidc.EnsureDefaultUser(context.Background(), db.DB); err != nil {
			return fmt.Errorf("failed to ensure default user: %w", err)
		}
		fmt.Println("Default user ensured (test@test.com / test)")

		// Ensure test CLI client exists for 'cyan-cli auth test'
		if err := authoidc.EnsureTestCLIClient(context.Background(), db.DB); err != nil {
			return fmt.Errorf("failed to ensure test CLI client: %w", err)
		}
		fmt.Printf("Test CLI client ensured (ID: %s)\n", authoidc.TestCLIClientID)

		// 6. Initialize orchestration components
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

		// Set OIDC registrar on env resolver
		oidcRegistrar := NewOIDCRegistrarAdapter(oidcStorage)
		envResolver.SetOIDCRegistrar(oidcRegistrar, issuerURL)

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

		// 7. Create API server
		server := api.NewServer(api.ServerConfig{
			ListenAddr:   fmt.Sprintf(":%d", port),
			ProjectRepo:  projectRepo,
			RevisionRepo: revisionRepo,
			Coordinator:  coordinator,
			Driver:       driver,
			Crypto:       crypto,
			PluginLoader: pluginLoader,
			PluginPath:   pluginPath,
			OIDCProvider: oidcProvider,
			Headless:     headless,
		})

		// 8. Setup graceful shutdown
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

		// 9. Start server
		fmt.Printf("\nCyanStack Server starting on http://localhost:%d\n", port)
		if headless {
			fmt.Println("Mode: Headless (API only)")
		} else {
			fmt.Printf("Web UI: http://localhost:%d\n", port)
		}
		fmt.Printf("API: http://localhost:%d/api/v1\n", port)
		fmt.Printf("OIDC Discovery: http://localhost:%d/.well-known/openid-configuration\n", port)
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
	serveCmd.Flags().Bool("headless", false, "Run in headless mode (API only, no embedded UI)")
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

// OIDCRegistrarAdapter adapts authoidc.Storage to orchestrator.OIDCRegistrar interface
type OIDCRegistrarAdapter struct {
	storage *authoidc.Storage
}

func NewOIDCRegistrarAdapter(storage *authoidc.Storage) *OIDCRegistrarAdapter {
	return &OIDCRegistrarAdapter{storage: storage}
}

func (a *OIDCRegistrarAdapter) RegisterClient(ctx context.Context, reg orchestrator.OIDCClientRegistration) (*orchestrator.OIDCCredentials, error) {
	creds, err := a.storage.RegisterClient(ctx, authoidc.ClientRegistration{
		Name:         reg.Name,
		PluginID:     reg.PluginID,
		NodeID:       reg.NodeID,
		ProjectID:    reg.ProjectID,
		RedirectURIs: reg.RedirectURIs,
	})
	if err != nil {
		return nil, err
	}
	return &orchestrator.OIDCCredentials{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
	}, nil
}

func (a *OIDCRegistrarAdapter) GetClientByNodeID(ctx context.Context, projectID, nodeID string) (*orchestrator.OIDCCredentials, error) {
	creds, err := a.storage.GetClientByNodeID(ctx, projectID, nodeID)
	if err != nil {
		return nil, err
	}
	return &orchestrator.OIDCCredentials{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
	}, nil
}
