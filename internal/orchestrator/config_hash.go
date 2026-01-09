package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// ConfigHashLabel is the label key used to store the config hash on containers
const ConfigHashLabel = "com.cyanstack.config_hash"

// ComputeConfigHash generates a SHA256 hash of the container configuration.
// Hash includes: Image, sorted Env vars (alphabetically by key), Command.
// This ensures deterministic hashes regardless of map iteration order.
func ComputeConfigHash(image string, env map[string]string, command []string) string {
	h := sha256.New()

	// Image
	h.Write([]byte("image:" + image + "\n"))

	// Sort env keys for determinism
	var keys []string
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h.Write([]byte("env:"))
	for _, k := range keys {
		h.Write([]byte(k + "=" + env[k] + "\n"))
	}

	// Command
	h.Write([]byte("cmd:" + strings.Join(command, " ") + "\n"))

	return hex.EncodeToString(h.Sum(nil))
}
