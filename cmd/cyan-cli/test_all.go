package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var testAllCmd = &cobra.Command{
	Use:   "test-all",
	Short: "Run all tests (Phase 1 + Phase 2 + Phase 3)",
	Long: `Runs all CyanStack validation tests in sequence:

Phase 1:
  - parse: Validates example-config.yaml
  - semaphore-test: Tests concurrent container startup limits
  - volume-test: Tests data persistence with Postgres

Phase 2:
  - db-init: Initializes SQLite database
  - secret-test: Tests AES-GCM encryption
  - network-test: Tests Docker networking

Phase 3:
  - deploy-test: Tests deployment orchestration with 2-node stack`,
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
		fmt.Println("[1/7] parse")
		fmt.Println("--------------------------------------------")
		if err := parseCmd.RunE(parseCmd, []string{"example-config.yaml"}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["parse"] = "FAILED"
		} else {
			fmt.Println()
			results["parse"] = "PASSED"
		}

		// 2. Semaphore test
		fmt.Println("[2/7] semaphore-test")
		fmt.Println("--------------------------------------------")
		if err := semaphoreTestCmd.RunE(semaphoreTestCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["semaphore-test"] = "FAILED"
		} else {
			fmt.Println()
			results["semaphore-test"] = "PASSED"
		}

		// 3. Volume test
		fmt.Println("[3/7] volume-test")
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
		fmt.Println("[4/7] db-init")
		fmt.Println("--------------------------------------------")
		if err := dbInitCmd.RunE(dbInitCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["db-init"] = "FAILED"
		} else {
			fmt.Println()
			results["db-init"] = "PASSED"
		}

		// 5. Secret test
		fmt.Println("[5/7] secret-test")
		fmt.Println("--------------------------------------------")
		if err := secretTestCmd.RunE(secretTestCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["secret-test"] = "FAILED"
		} else {
			fmt.Println()
			results["secret-test"] = "PASSED"
		}

		// 6. Network test
		fmt.Println("[6/7] network-test")
		fmt.Println("--------------------------------------------")
		if err := networkTestCmd.RunE(networkTestCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["network-test"] = "FAILED"
		} else {
			fmt.Println()
			results["network-test"] = "PASSED"
		}

		// Phase 3
		fmt.Println(">>> PHASE 3 TESTS <<<")
		fmt.Println()

		// 7. Deploy test
		fmt.Println("[7/7] deploy-test")
		fmt.Println("--------------------------------------------")
		if err := deployTestCmd.RunE(deployTestCmd, []string{}); err != nil {
			fmt.Printf("RESULT: FAILED (%v)\n\n", err)
			results["deploy-test"] = "FAILED"
		} else {
			fmt.Println()
			results["deploy-test"] = "PASSED"
		}

		// Summary
		fmt.Println("============================================")
		fmt.Println("  TEST SUMMARY")
		fmt.Println("============================================")
		passed := 0
		failed := 0
		tests := []string{"parse", "semaphore-test", "volume-test", "db-init", "secret-test", "network-test", "deploy-test"}
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
