package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
	"github.com/cyanstack/cyanstack/internal/drivers/local"
	"github.com/cyanstack/cyanstack/internal/security"
	"github.com/cyanstack/cyanstack/internal/store/sqlite"
)

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
