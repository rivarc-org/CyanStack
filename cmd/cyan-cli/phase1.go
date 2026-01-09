package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cyanstack/cyanstack/internal/config"
	"github.com/cyanstack/cyanstack/internal/drivers/interfaces"
	"github.com/cyanstack/cyanstack/internal/drivers/local"
)

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
				"5432": "",
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
		}, nil)
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
		}, nil)
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
