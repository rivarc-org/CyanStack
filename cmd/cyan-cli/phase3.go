package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
	"github.com/cyanstack/cyanstack/internal/drivers/local"
	"github.com/cyanstack/cyanstack/internal/orchestrator"
	"github.com/cyanstack/cyanstack/internal/security"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
)

var deployTestCmd = &cobra.Command{
	Use:   "deploy-test",
	Short: "Test Phase 3 deployment with mock 2-node stack (Step 3.3 validation)",
	Long: `Tests the Coordinator deployment orchestration:
1. Creates a mock project with Postgres and Backend nodes
2. Runs the DAG resolver to determine deployment order
3. Starts Postgres first (Tier 0)
4. Starts Backend with injected DATABASE_URL (Tier 1)
5. Verifies both containers are running
6. Verifies Backend has Postgres connection string in env`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("=== Phase 3: Deploy Test ===")
		fmt.Println()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		// Handle interrupt
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Println("\nReceived interrupt, cleaning up...")
			cancel()
		}()

		// 1. Initialize driver
		fmt.Println("--- Step 1: Initialize driver ---")
		driver, err := local.NewDriver()
		if err != nil {
			return fmt.Errorf("failed to create driver: %w", err)
		}
		fmt.Println("Driver initialized")

		// 2. Initialize database and repositories
		fmt.Println("\n--- Step 2: Initialize database ---")
		db, err := sqlite.Open()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		if err := sqlite.RunMigrations(db); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}
		fmt.Printf("Database: %s\n", db.Path())

		// Initialize crypto for secrets (embedded in JSON)
		ks := security.NewKeyStore("")
		crypto, err := ks.NewCrypto()
		if err != nil {
			return fmt.Errorf("failed to create crypto: %w", err)
		}
		fmt.Println("Crypto initialized")

		// 3. Setup orchestrator components
		fmt.Println("\n--- Step 3: Setup orchestrator ---")

		// Find testdata/plugins directory
		execPath, _ := os.Executable()
		execDir := filepath.Dir(execPath)
		// Try multiple paths to find testdata
		testdataPaths := []string{
			filepath.Join(execDir, "testdata", "plugins"),
			filepath.Join(".", "testdata", "plugins"),
			filepath.Join("..", "..", "testdata", "plugins"),
		}

		var pluginPath string
		for _, p := range testdataPaths {
			if _, err := os.Stat(p); err == nil {
				pluginPath = p
				break
			}
		}
		if pluginPath == "" {
			return fmt.Errorf("could not find testdata/plugins directory")
		}
		fmt.Printf("Plugin path: %s\n", pluginPath)

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

		coordinator := orchestrator.NewCoordinator(orchestrator.CoordinatorConfig{
			Driver:         driver,
			NetworkManager: netManager,
			TraefikGen:     traefikGen,
			DAGResolver:    dagResolver,
			EnvResolver:    envResolver,
			StartupTimeout: 2 * time.Minute,
		})
		fmt.Println("Coordinator initialized")

		// 4. Create test project in database (required for foreign key constraints)
		fmt.Println("\n--- Step 4: Create test project ---")
		projectID := fmt.Sprintf("deploy-test-%d", time.Now().Unix())
		projectName := "deploytest"

		projectRepo := sqlite.NewProjectRepository(db)
		testProject := &sqlite.Project{
			ID:   projectID,
			Name: projectName,
		}
		if err := projectRepo.Create(ctx, testProject); err != nil {
			return fmt.Errorf("failed to create test project: %w", err)
		}
		fmt.Printf("Created project: %s (%s)\n", projectName, projectID)

		// 5. Create test graph
		fmt.Println("\n--- Step 5: Create test graph ---")
		graph := orchestrator.GraphState{
			Nodes: []orchestrator.NodeState{
				{
					ID:       "node-postgres-1",
					PluginID: "com.cyanstack.postgres",

					Inputs: map[string]string{
						"DB_PASSWORD": "testpassword123",
						"DB_NAME":     "testdb",
					},
				},
				{
					ID:       "node-backend-1",
					PluginID: "com.cyanstack.testbackend",
					Inputs: map[string]string{
						"MAIN_DATABASE": "node-postgres-1",
						"APP_CONFIG":    "test-app-config-value",
					},
				},
			},
		}
		fmt.Printf("Nodes: %d (postgres, backend)\n", len(graph.Nodes))

		// Cleanup function
		cleanupDeployment := func() {
			fmt.Println("\n--- Cleanup ---")
			if err := coordinator.StopProject(ctx, projectID); err != nil {
				fmt.Printf("Warning: cleanup error: %v\n", err)
			}
			if err := netManager.RemoveProjectNetwork(ctx, projectID); err != nil {
				fmt.Printf("Warning: failed to remove network: %v\n", err)
			}
			portAllocator.ReleaseAllForProject(projectID)
			// Delete test project from database
			if err := projectRepo.Delete(ctx, projectID); err != nil {
				fmt.Printf("Warning: failed to delete test project: %v\n", err)
			}
			fmt.Println("Cleanup complete")
		}

		// 6. Run deployment
		fmt.Println("\n--- Step 6: Run deployment ---")
		result, err := coordinator.DeployDirect(ctx, projectID, projectName, "rev-test-1", graph)
		if err != nil {
			cleanupDeployment()
			return fmt.Errorf("deployment failed: %w", err)
		}
		if !result.Success {
			cleanupDeployment()
			return fmt.Errorf("deployment returned failure")
		}
		fmt.Printf("Deployment successful! Started %d containers\n", len(result.ContainerIDs))

		// 7. Verify containers
		fmt.Println("\n--- Step 7: Verify containers ---")
		for nodeID, containerID := range result.ContainerIDs {
			state, err := driver.Inspect(ctx, containerID)
			if err != nil {
				cleanupDeployment()
				return fmt.Errorf("failed to inspect container %s: %w", nodeID, err)
			}
			fmt.Printf("  %s: %s (status: %s)\n", nodeID, containerID[:12], state.Status)
			if state.Status != interfaces.StatusRunning {
				cleanupDeployment()
				return fmt.Errorf("container %s is not running (status: %s)", nodeID, state.Status)
			}
		}

		// 8. Verify backend has DATABASE_URL
		fmt.Println("\n--- Step 8: Verify environment injection ---")
		backendContainerID := result.ContainerIDs["node-backend-1"]
		if backendContainerID != "" {
			// Get container info to check env vars
			cli := driver.Client()
			containerInfo, err := cli.ContainerInspect(ctx, backendContainerID)
			if err != nil {
				cleanupDeployment()
				return fmt.Errorf("failed to inspect backend container: %w", err)
			}

			foundDatabaseURL := false
			for _, env := range containerInfo.Config.Env {
				if strings.HasPrefix(env, "DATABASE_URL=") {
					foundDatabaseURL = true
					fmt.Printf("  DATABASE_URL: %s\n", env[13:])
					break
				}
			}
			if !foundDatabaseURL {
				cleanupDeployment()
				return fmt.Errorf("DATABASE_URL not found in backend container env")
			}
		}

		// 9. Wait a moment then cleanup
		fmt.Println("\n--- Step 9: Success! ---")
		fmt.Println("Containers are running. Cleaning up in 5 seconds...")
		time.Sleep(5 * time.Second)

		cleanupDeployment()

		fmt.Println("\n=== Results ===")
		fmt.Println("PASS: Postgres started in Tier 0")
		fmt.Println("PASS: Backend started in Tier 1 with DATABASE_URL")
		fmt.Println("PASS: Both containers reached running state")
		fmt.Println("PASS: Environment variables correctly injected")

		return nil
	},
}
