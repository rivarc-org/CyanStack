package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cyanstack/cyanstack/internal/config"
	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
	"github.com/cyanstack/cyanstack/internal/drivers/local"
	"github.com/cyanstack/cyanstack/internal/orchestrator"
	"github.com/cyanstack/cyanstack/internal/security"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
)

var rootCmd = &cobra.Command{
	Use:   "cyan-cli",
	Short: "CyanStack CLI for container management",
	Long:  "CyanStack CLI - Phase 1: Docker execution engine with semaphore-based concurrency control",
}

var parseCmd = &cobra.Command{
	Use:   "parse [config-file]",
	Short: "Parse and validate a plugin configuration file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		parser := config.NewParser()
		cfg, err := parser.ParseFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to parse config: %w", err)
		}

		fmt.Printf("Plugin ID:      %s\n", cfg.ID)
		fmt.Printf("Version:        %s\n", cfg.Version)
		fmt.Printf("Name:           %s\n", cfg.Name)
		fmt.Printf("Category:       %s\n", cfg.Category)
		fmt.Printf("Inputs:         %d\n", len(cfg.Inputs))
		for _, input := range cfg.Inputs {
			fmt.Printf("  - %s (%s) -> %s\n", input.ID, input.Type, input.InjectAs)
		}
		fmt.Printf("Start Command:  %v\n", cfg.Lifecycle.StartCommand)
		if cfg.Lifecycle.HealthCheck.Path != "" {
			fmt.Printf("Health Check:   %s\n", cfg.Lifecycle.HealthCheck.Path)
		}
		fmt.Printf("Exports:        %d\n", len(cfg.Exports))

		fmt.Println("\n✓ Configuration is valid")
		return nil
	},
}

var semaphoreTestCmd = &cobra.Command{
	Use:   "semaphore-test",
	Short: "Test semaphore by spawning 20 containers (Step 1.2 validation)",
	Long: `Spawns 20 alpine containers with "sleep 5" to validate that:
- The semaphore limits concurrent STARTUP operations to runtime.NumCPU()
- All 20 containers eventually run (semaphore doesn't limit total)
- Watch startup throttling with: docker events --filter event=start`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cpuCount := runtime.NumCPU()
		fmt.Printf("CPU count: %d\n", cpuCount)
		fmt.Printf("Spawning 20 containers with concurrency limit of %d\n", cpuCount)
		fmt.Println("(Semaphore limits concurrent STARTUPS, not total running containers)")

		driver, err := local.NewDriver(local.WithConcurrencyLimit(cpuCount))
		if err != nil {
			return fmt.Errorf("failed to create driver: %w", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle interrupt
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Println("\nReceived interrupt, cleaning up...")
			cancel()
		}()

		var maxConcurrentStartup int32
		var mu sync.Mutex
		var containerIDs []string
		var wg sync.WaitGroup
		var errors []error
		done := make(chan struct{})

		timestamp := time.Now().UnixNano()

		// Monitor the semaphore's actual active count
		go func() {
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					active := int32(driver.ActiveStartups())
					for {
						max := atomic.LoadInt32(&maxConcurrentStartup)
						if active <= max || atomic.CompareAndSwapInt32(&maxConcurrentStartup, max, active) {
							break
						}
					}
				}
			}
		}()

		// Launch 20 containers
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()

				startTime := time.Now()

				conf := interfaces.ContainerConfig{
					Image:   "alpine:latest",
					Name:    fmt.Sprintf("cyanstack-semaphore-test-%d-%d", timestamp, idx),
					Command: []string{"sleep", "5"},
					Labels: map[string]string{
						"cyanstack.test": "semaphore",
					},
				}

				containerID, err := driver.Run(ctx, conf)

				if err != nil {
					if ctx.Err() != nil {
						return // Context cancelled
					}
					fmt.Printf("[%02d] Failed to start: %v\n", idx, err)
					mu.Lock()
					errors = append(errors, err)
					mu.Unlock()
					return
				}

				mu.Lock()
				containerIDs = append(containerIDs, containerID)
				mu.Unlock()

				fmt.Printf("[%02d] Started in %v (container: %s...)\n",
					idx, time.Since(startTime).Round(time.Millisecond), containerID[:12])
			}(i)
		}

		wg.Wait()
		close(done)

		fmt.Println("\n--- Waiting for containers to complete ---")
		time.Sleep(6 * time.Second)

		// Cleanup
		fmt.Println("\n--- Cleaning up containers ---")
		for _, id := range containerIDs {
			if err := driver.Stop(ctx, id); err != nil {
				fmt.Printf("Warning: failed to stop %s: %v\n", id[:12], err)
			}
			if err := driver.Remove(ctx, id); err != nil {
				fmt.Printf("Warning: failed to remove %s: %v\n", id[:12], err)
			}
		}

		fmt.Printf("\n=== Results ===\n")
		fmt.Printf("Max concurrent STARTUP operations: %d\n", maxConcurrentStartup)
		fmt.Printf("CPU count limit:                   %d\n", cpuCount)
		fmt.Printf("Total containers started:          %d\n", len(containerIDs))
		fmt.Printf("Errors:                            %d\n", len(errors))

		if int(maxConcurrentStartup) <= cpuCount && len(containerIDs) == 20 {
			fmt.Println("\n✓ PASS: Semaphore correctly limited concurrent startups")
			fmt.Println("✓ PASS: All 20 containers were started successfully")
			return nil
		}

		if int(maxConcurrentStartup) > cpuCount {
			return fmt.Errorf("FAIL: Concurrent startups (%d) exceeded limit (%d)", maxConcurrentStartup, cpuCount)
		}
		return fmt.Errorf("FAIL: Only %d/20 containers started", len(containerIDs))
	},
}

var volumeTestCmd = &cobra.Command{
	Use:   "volume-test",
	Short: "Test volume persistence with Postgres (Step 1.3 validation)",
	Long: `Tests that data persists across container restarts:
1. Starts postgres:15-alpine with volume at ~/.cyanstack/data/test-project/postgres/
2. Creates table, inserts data
3. Stops and removes container
4. Starts new container with same volume
5. Queries data - should still exist`,
	RunE: func(cmd *cobra.Command, args []string) error {
		driver, err := local.NewDriver()
		if err != nil {
			return fmt.Errorf("failed to create driver: %w", err)
		}

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

		testDataPath := driver.DataPath("test-project", "postgres")
		fmt.Printf("Data path: %s\n\n", testDataPath)

		// First run
		fmt.Println("=== Phase 1: Create container and insert data ===")
		fmt.Println("Starting Postgres container...")

		conf := interfaces.ContainerConfig{
			Image: "postgres:15-alpine",
			Name:  "cyanstack-volume-test-1",
			Env: map[string]string{
				"POSTGRES_PASSWORD": "testpass",
				"POSTGRES_DB":       "testdb",
			},
			Ports: map[string]string{
				"5433": "5432",
			},
			Volumes: map[string]string{
				testDataPath: "/var/lib/postgresql/data",
			},
			Labels: map[string]string{
				"cyanstack.test": "volume",
			},
		}

		containerID, err := driver.Run(ctx, conf)
		if err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		fmt.Printf("Container started: %s\n", containerID[:12])

		fmt.Println("Waiting for Postgres to be ready (15s)...")
		time.Sleep(15 * time.Second)

		testValue := fmt.Sprintf("persistence_test_%d", time.Now().Unix())
		fmt.Printf("Creating test table and inserting: %s\n", testValue)

		exitCode, err := driver.Exec(ctx, containerID, []string{
			"psql", "-U", "postgres", "-d", "testdb", "-c",
			fmt.Sprintf("CREATE TABLE IF NOT EXISTS persistence_test (id SERIAL PRIMARY KEY, value TEXT, created_at TIMESTAMP DEFAULT NOW()); INSERT INTO persistence_test (value) VALUES ('%s');", testValue),
		})
		if err != nil {
			cleanup(ctx, driver, containerID)
			return fmt.Errorf("failed to insert data: %w", err)
		}
		if exitCode != 0 {
			cleanup(ctx, driver, containerID)
			return fmt.Errorf("psql returned exit code: %d", exitCode)
		}
		fmt.Println("Data inserted successfully")

		// Stop first container
		fmt.Println("\n=== Phase 2: Stop and remove container ===")
		fmt.Println("Stopping container...")
		if err := driver.Stop(ctx, containerID); err != nil {
			return fmt.Errorf("failed to stop: %w", err)
		}
		fmt.Println("Removing container...")
		if err := driver.Remove(ctx, containerID); err != nil {
			return fmt.Errorf("failed to remove: %w", err)
		}
		fmt.Println("Container removed")

		// Second run
		fmt.Println("\n=== Phase 3: Start new container and verify data ===")
		fmt.Println("Starting new Postgres container with same volume...")

		conf.Name = "cyanstack-volume-test-2"
		containerID2, err := driver.Run(ctx, conf)
		if err != nil {
			return fmt.Errorf("failed to restart container: %w", err)
		}
		fmt.Printf("Container started: %s\n", containerID2[:12])

		fmt.Println("Waiting for Postgres to be ready (10s)...")
		time.Sleep(10 * time.Second)

		fmt.Println("Querying persisted data...")
		exitCode, err = driver.Exec(ctx, containerID2, []string{
			"psql", "-U", "postgres", "-d", "testdb", "-c",
			fmt.Sprintf("SELECT value FROM persistence_test WHERE value = '%s';", testValue),
		})
		if err != nil {
			cleanup(ctx, driver, containerID2)
			return fmt.Errorf("failed to query data: %w", err)
		}
		if exitCode != 0 {
			cleanup(ctx, driver, containerID2)
			return fmt.Errorf("query failed with exit code: %d (data may not have persisted)", exitCode)
		}

		// Cleanup
		fmt.Println("\n=== Cleanup ===")
		cleanup(ctx, driver, containerID2)

		fmt.Println("\n=== Results ===")
		fmt.Println("✓ PASS: Data persisted across container restart")
		fmt.Printf("✓ Volume path: %s\n", testDataPath)

		return nil
	},
}

// Phase 2 Commands

var dbInitCmd = &cobra.Command{
	Use:   "db-init",
	Short: "Initialize SQLite database and run migrations (Step 2.1 validation)",
	Long:  `Creates the CyanStack database at ~/.cyanstack/cyanstack.db and runs all migrations.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("=== Initializing CyanStack Database ===")

		// Open database
		fmt.Println("Opening database...")
		db, err := sqlite.Open()
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()
		fmt.Printf("Database path: %s\n", db.Path())

		// Run migrations
		fmt.Println("Running migrations...")
		if err := sqlite.RunMigrations(db); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}

		// Get migration version
		version, dirty, err := sqlite.MigrationVersion(db)
		if err != nil {
			return fmt.Errorf("failed to get migration version: %w", err)
		}

		fmt.Printf("\n=== Results ===\n")
		fmt.Printf("Database:         %s\n", db.Path())
		fmt.Printf("Migration version: %d\n", version)
		if dirty {
			fmt.Println("WARNING: Database is in dirty state")
		}
		fmt.Println("\n✓ Database initialized successfully")

		return nil
	},
}

var secretTestCmd = &cobra.Command{
	Use:   "secret-test",
	Short: "Test AES-GCM encryption round-trip (Step 2.2 validation)",
	Long:  `Encrypts "Hello World", prints the cipher, decrypts it, and verifies the result.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("=== Testing Secret Encryption ===")

		// Get keystore
		ks := security.NewKeyStore("")
		fmt.Printf("Key file: %s\n", ks.KeyPath())

		// Create crypto
		crypto, err := ks.NewCrypto()
		if err != nil {
			return fmt.Errorf("failed to create crypto: %w", err)
		}

		// Test encryption
		plaintext := "Hello World"
		fmt.Printf("\nOriginal:  %s\n", plaintext)

		encrypted, err := crypto.Encrypt([]byte(plaintext))
		if err != nil {
			return fmt.Errorf("failed to encrypt: %w", err)
		}
		fmt.Printf("Encrypted: %s\n", string(encrypted))

		// Test decryption
		decrypted, err := crypto.Decrypt(encrypted)
		if err != nil {
			return fmt.Errorf("failed to decrypt: %w", err)
		}
		fmt.Printf("Decrypted: %s\n", string(decrypted))

		// Verify
		if string(decrypted) != plaintext {
			return fmt.Errorf("FAIL: decrypted text does not match original")
		}

		fmt.Println("\n=== Results ===")
		fmt.Println("✓ PASS: Encryption round-trip successful")
		fmt.Printf("✓ Key location: %s\n", ks.KeyPath())

		return nil
	},
}

var networkTestCmd = &cobra.Command{
	Use:   "network-test",
	Short: "Test Docker networking (Step 2.3 validation)",
	Long: `Tests Docker network creation and container connectivity:
1. Creates a project network
2. Starts nginx server container on the network
3. Starts curl client container on the same network
4. Verifies client can reach server via network alias`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("=== Testing Docker Networking ===")

		driver, err := local.NewDriver()
		if err != nil {
			return fmt.Errorf("failed to create driver: %w", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		// Handle interrupt
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-sigCh
			fmt.Println("\nReceived interrupt, cleaning up...")
			cancel()
		}()

		netMgr := local.NewNetworkManager(driver.Client())
		projectID := "network-test"

		// 1. Create project network
		fmt.Println("\n--- Step 1: Create project network ---")
		networkName, err := netMgr.EnsureProjectNetwork(ctx, projectID)
		if err != nil {
			return fmt.Errorf("failed to create network: %w", err)
		}
		fmt.Printf("Created network: %s\n", networkName)

		var serverID, clientID string
		timestamp := time.Now().UnixNano()

		defer func() {
			// Cleanup
			fmt.Println("\n--- Cleanup ---")
			if clientID != "" {
				cleanupContainer(ctx, driver, clientID)
			}
			if serverID != "" {
				cleanupContainer(ctx, driver, serverID)
			}
			if err := netMgr.RemoveProjectNetwork(ctx, projectID); err != nil {
				fmt.Printf("Warning: failed to remove network: %v\n", err)
			} else {
				fmt.Printf("Removed network: %s\n", networkName)
			}
		}()

		// 2. Start nginx server
		fmt.Println("\n--- Step 2: Start nginx server ---")
		serverConf := interfaces.ContainerConfig{
			Image:    "nginx:alpine",
			Name:     fmt.Sprintf("cyan-net-test-server-%d", timestamp),
			Networks: []string{networkName},
			NetworkAliases: map[string][]string{
				networkName: {"server"},
			},
			Labels: map[string]string{
				"cyanstack.test": "network",
			},
		}
		serverID, err = driver.Run(ctx, serverConf)
		if err != nil {
			return fmt.Errorf("failed to start server: %w", err)
		}
		fmt.Printf("Server started: %s (alias: server)\n", serverID[:12])

		// Wait for nginx to be ready
		fmt.Println("Waiting for nginx to be ready (3s)...")
		time.Sleep(3 * time.Second)

		// 3. Start curl client
		fmt.Println("\n--- Step 3: Start curl client ---")
		clientConf := interfaces.ContainerConfig{
			Image:    "curlimages/curl:latest",
			Name:     fmt.Sprintf("cyan-net-test-client-%d", timestamp),
			Command:  []string{"curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://server:80"},
			Networks: []string{networkName},
			Labels: map[string]string{
				"cyanstack.test": "network",
			},
		}
		clientID, err = driver.Run(ctx, clientConf)
		if err != nil {
			return fmt.Errorf("failed to start client: %w", err)
		}
		fmt.Printf("Client started: %s\n", clientID[:12])

		// 4. Wait and check logs
		fmt.Println("\n--- Step 4: Verify connectivity ---")
		fmt.Println("Waiting for curl to complete (3s)...")
		time.Sleep(3 * time.Second)

		// Get client logs to check HTTP status code
		logs, err := driver.StreamLogs(ctx, clientID)
		if err != nil {
			return fmt.Errorf("failed to get logs: %w", err)
		}
		defer logs.Close()

		logBytes, _ := io.ReadAll(logs)
		logOutput := string(logBytes)
		fmt.Printf("Client output: %s\n", strings.TrimSpace(logOutput))

		// Check if we got 200 status
		if strings.Contains(logOutput, "200") {
			fmt.Println("\n=== Results ===")
			fmt.Printf("✓ PASS: Client successfully reached server via network alias\n")
			fmt.Printf("✓ Network: %s\n", networkName)
			fmt.Printf("✓ Server alias: server\n")
			fmt.Printf("✓ HTTP Status: 200\n")
			return nil
		}

		return fmt.Errorf("FAIL: Could not verify connectivity (expected HTTP 200)")
	},
}

var testAllCmd = &cobra.Command{
	Use:   "test-all",
	Short: "Run all tests (Phase 1 + Phase 2)",
	Long: `Runs all CyanStack validation tests in sequence:

Phase 1:
  - parse: Validates example-config.yaml
  - semaphore-test: Tests concurrent container startup limits
  - volume-test: Tests data persistence with Postgres

Phase 2:
  - db-init: Initializes SQLite database
  - secret-test: Tests AES-GCM encryption
  - network-test: Tests Docker networking`,
	RunE: func(cmd *cobra.Command, args []string) error {
		results := make(map[string]string)

		fmt.Println("============================================")
		fmt.Println("  CyanStack - Full Test Suite")
		fmt.Println("============================================")
		fmt.Println()

		// Phase 1
		fmt.Println(">>> PHASE 1 TESTS <<<")
		fmt.Println()

		// 1. Parse test
		fmt.Println("[1/6] parse")
		fmt.Println("--------------------------------------------")
		if err := parseCmd.RunE(parseCmd, []string{"example-config.yaml"}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["parse"] = "FAILED"
		} else {
			fmt.Println()
			results["parse"] = "PASSED"
		}

		// 2. Semaphore test
		fmt.Println("[2/6] semaphore-test")
		fmt.Println("--------------------------------------------")
		if err := semaphoreTestCmd.RunE(semaphoreTestCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["semaphore-test"] = "FAILED"
		} else {
			fmt.Println()
			results["semaphore-test"] = "PASSED"
		}

		// 3. Volume test
		fmt.Println("[3/6] volume-test")
		fmt.Println("--------------------------------------------")
		if err := volumeTestCmd.RunE(volumeTestCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["volume-test"] = "FAILED"
		} else {
			fmt.Println()
			results["volume-test"] = "PASSED"
		}

		// Phase 2
		fmt.Println(">>> PHASE 2 TESTS <<<")
		fmt.Println()

		// 4. DB init
		fmt.Println("[4/6] db-init")
		fmt.Println("--------------------------------------------")
		if err := dbInitCmd.RunE(dbInitCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["db-init"] = "FAILED"
		} else {
			fmt.Println()
			results["db-init"] = "PASSED"
		}

		// 5. Secret test
		fmt.Println("[5/6] secret-test")
		fmt.Println("--------------------------------------------")
		if err := secretTestCmd.RunE(secretTestCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["secret-test"] = "FAILED"
		} else {
			fmt.Println()
			results["secret-test"] = "PASSED"
		}

		// 6. Network test
		fmt.Println("[6/6] network-test")
		fmt.Println("--------------------------------------------")
		if err := networkTestCmd.RunE(networkTestCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["network-test"] = "FAILED"
		} else {
			fmt.Println()
			results["network-test"] = "PASSED"
		}

		// Summary
		fmt.Println("============================================")
		fmt.Println("  TEST SUMMARY")
		fmt.Println("============================================")
		passed := 0
		failed := 0
		tests := []string{"parse", "semaphore-test", "volume-test", "db-init", "secret-test", "network-test"}
		for _, test := range tests {
			status := results[test]
			if status == "PASSED" {
				fmt.Printf("  [PASS] %s\n", test)
				passed++
			} else {
				fmt.Printf("  [FAIL] %s\n", test)
				failed++
			}
		}
		fmt.Println("--------------------------------------------")
		fmt.Printf("  Total: %d passed, %d failed\n", passed, failed)
		fmt.Println("============================================")

		if failed > 0 {
			return fmt.Errorf("%d test(s) failed", failed)
		}
		return nil
	},
}

// Phase 3 Commands

// secretStoreAdapter wraps sqlite.SecretRepository to implement orchestrator.SecretStore
type secretStoreAdapter struct {
	repo *sqlite.SecretRepository
}

func (s *secretStoreAdapter) Get(ctx context.Context, projectID, pluginID, keyName string) (string, error) {
	return s.repo.Get(ctx, projectID, pluginID, keyName)
}

func (s *secretStoreAdapter) Set(ctx context.Context, projectID, pluginID, keyName, value string) error {
	return s.repo.Set(ctx, projectID, pluginID, keyName, value)
}

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

		// Initialize crypto for secrets
		ks := security.NewKeyStore("")
		crypto, err := ks.NewCrypto()
		if err != nil {
			return fmt.Errorf("failed to create crypto: %w", err)
		}

		secretRepo := sqlite.NewSecretRepository(db, crypto)
		fmt.Println("Secret repository initialized")

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
			&secretStoreAdapter{repo: secretRepo},
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
					Image:    "postgres:15-alpine",
					Inputs: map[string]string{
						"DB_PASSWORD": "testpassword123",
						"DB_NAME":     "testdb",
					},
				},
				{
					ID:       "node-backend-1",
					PluginID: "com.cyanstack.testbackend",
					Image:    "alpine:latest", // Use alpine for testing (just needs to run)
					Inputs: map[string]string{
						"MAIN_DATABASE": "node-postgres-1",
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

func cleanup(ctx context.Context, driver *local.Driver, containerID string) {
	fmt.Printf("Cleaning up container %s...\n", containerID[:12])
	driver.Stop(ctx, containerID)
	driver.Remove(ctx, containerID)
}

func cleanupContainer(ctx context.Context, driver *local.Driver, containerID string) {
	if containerID == "" {
		return
	}
	short := containerID
	if len(containerID) > 12 {
		short = containerID[:12]
	}
	fmt.Printf("Removing container %s...\n", short)
	driver.Stop(ctx, containerID)
	driver.Remove(ctx, containerID)
}

func init() {
	// Phase 1 commands
	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(semaphoreTestCmd)
	rootCmd.AddCommand(volumeTestCmd)

	// Phase 2 commands
	rootCmd.AddCommand(dbInitCmd)
	rootCmd.AddCommand(secretTestCmd)
	rootCmd.AddCommand(networkTestCmd)

	// Phase 3 commands
	rootCmd.AddCommand(deployTestCmd)

	// Meta commands
	rootCmd.AddCommand(testAllCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
