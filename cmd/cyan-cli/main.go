package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cyan-cli",
	Short: "CyanStack CLI for container management",
	Long:  "CyanStack CLI - Docker execution engine with semaphore-based concurrency control",
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
