package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"
	"strconv"

	"github.com/cyanstack/cyanstack/internal/config"
)

// templatePattern matches {{VARIABLE_NAME}} patterns
var templatePattern = regexp.MustCompile(`\{\{([A-Z_][A-Z0-9_]*)\}\}`)

// SecretStore defines the interface for secret retrieval and storage
type SecretStore interface {
	Get(ctx context.Context, projectID, pluginID, keyName string) (string, error)
	Set(ctx context.Context, projectID, pluginID, keyName, value string) error
}

// EnvResolver resolves environment variables for nodes
type EnvResolver struct {
	secretStore   SecretStore
	pluginLoader  PluginLoader
	portAllocator PortAllocator
}

// NewEnvResolver creates a new environment resolver
func NewEnvResolver(
	secretStore SecretStore,
	pluginLoader PluginLoader,
	portAllocator PortAllocator,
) *EnvResolver {
	return &EnvResolver{
		secretStore:   secretStore,
		pluginLoader:  pluginLoader,
		portAllocator: portAllocator,
	}
}

// ResolveNodeEnv resolves all environment variables for a node
// Must be called after dependencies have been started and their outputs registered
func (r *EnvResolver) ResolveNodeEnv(
	ctx context.Context,
	node NodeState,
	runtimeCtx *RuntimeContext,
) (map[string]string, error) {

	// Load plugin configuration
	pluginConfig, ok := runtimeCtx.PluginConfigs[node.PluginID]
	if !ok {
		var err error
		pluginConfig, err = r.pluginLoader.Load(node.PluginID)
		if err != nil {
			return nil, fmt.Errorf("loading plugin config for %s: %w", node.PluginID, err)
		}
		runtimeCtx.PluginConfigs[node.PluginID] = pluginConfig
	}

	env := make(map[string]string)

	// Process each input definition from the plugin
	for _, inputDef := range pluginConfig.Inputs {
		switch inputDef.Type {
		case "port":
			if err := r.resolvePortInput(ctx, node, inputDef, runtimeCtx, env); err != nil {
				return nil, &EnvResolutionError{
					NodeID:    node.ID,
					InputID:   inputDef.ID,
					InputType: "port",
					Cause:     err,
				}
			}

		case "secret":
			if err := r.resolveSecretInput(ctx, node, inputDef, runtimeCtx, env); err != nil {
				return nil, &EnvResolutionError{
					NodeID:    node.ID,
					InputID:   inputDef.ID,
					InputType: "secret",
					Cause:     err,
				}
			}

		case "plugin":
			if err := r.resolvePluginInput(ctx, node, inputDef, runtimeCtx, env); err != nil {
				return nil, &EnvResolutionError{
					NodeID:    node.ID,
					InputID:   inputDef.ID,
					InputType: "plugin",
					Cause:     err,
				}
			}
		}
	}

	// Generate and store auto-exports for this node
	if err := r.generateAutoExports(node, pluginConfig, runtimeCtx, env); err != nil {
		return nil, fmt.Errorf("generating auto-exports: %w", err)
	}

	// Resolve custom exports (templates like {{INTERNAL_HOST}})
	if err := r.resolveCustomExports(node, pluginConfig, runtimeCtx, env); err != nil {
		return nil, fmt.Errorf("resolving custom exports: %w", err)
	}

	return env, nil
}

// resolvePortInput handles type: port inputs
func (r *EnvResolver) resolvePortInput(
	ctx context.Context,
	node NodeState,
	inputDef config.InputDefinition,
	runtimeCtx *RuntimeContext,
	env map[string]string,
) error {
	var containerPort int
	var hostPort int

	// Check if default is set (fixed port like Postgres 5432)
	if inputDef.Default != "" {
		defaultPort, err := strconv.Atoi(inputDef.Default)
		if err != nil {
			return fmt.Errorf("invalid default port %q: %w", inputDef.Default, err)
		}
		containerPort = defaultPort
	}

	// Check if user provided a port in inputs
	if userPort, ok := node.Inputs[inputDef.ID]; ok && userPort != "" {
		port, err := strconv.Atoi(userPort)
		if err != nil {
			return fmt.Errorf("invalid port value %q: %w", userPort, err)
		}
		// User-provided port overrides the default container port
		containerPort = port
	}

	// Allocate host port for external access
	allocatedPort, err := r.portAllocator.Allocate(runtimeCtx.ProjectID, node.ID)
	if err != nil {
		return fmt.Errorf("allocating host port: %w", err)
	}
	hostPort = allocatedPort

	// If no container port was determined, use the host port
	if containerPort == 0 {
		containerPort = hostPort
	}

	// Store port assignment
	runtimeCtx.AssignedPorts[node.ID] = PortAssignment{
		HostPort:      hostPort,
		ContainerPort: containerPort,
	}

	// Inject as environment variable if specified
	if inputDef.InjectAs != "" {
		env[inputDef.InjectAs] = strconv.Itoa(containerPort)
	}

	return nil
}

// resolveSecretInput handles type: secret inputs
func (r *EnvResolver) resolveSecretInput(
	ctx context.Context,
	node NodeState,
	inputDef config.InputDefinition,
	runtimeCtx *RuntimeContext,
	env map[string]string,
) error {
	var secret string

	// Check if value provided in node inputs (user override)
	if userValue, ok := node.Inputs[inputDef.ID]; ok && userValue != "" {
		secret = userValue
		// Store for future reference
		if r.secretStore != nil {
			if err := r.secretStore.Set(ctx, runtimeCtx.ProjectID, node.PluginID, inputDef.ID, userValue); err != nil {
				// Log but don't fail - user value will still be used
			}
		}
	} else if r.secretStore != nil {
		// Try to fetch existing secret
		var err error
		secret, err = r.secretStore.Get(ctx, runtimeCtx.ProjectID, node.PluginID, inputDef.ID)
		if err != nil {
			// Secret doesn't exist, will try to generate
			secret = ""
		}
	}

	// Generate if not found and generation is enabled
	if secret == "" && inputDef.Generated {
		var err error
		secret, err = generateSecureSecret(32)
		if err != nil {
			return fmt.Errorf("generating secret: %w", err)
		}
		// Store the generated secret
		if r.secretStore != nil {
			if err := r.secretStore.Set(ctx, runtimeCtx.ProjectID, node.PluginID, inputDef.ID, secret); err != nil {
				return fmt.Errorf("storing generated secret: %w", err)
			}
		}
	}

	if secret == "" {
		return fmt.Errorf("secret %s not found and generate=false", inputDef.ID)
	}

	// Inject as environment variable if specified
	if inputDef.InjectAs != "" {
		env[inputDef.InjectAs] = secret
	}

	// Store in runtime context for template resolution
	runtimeCtx.SetNodeOutput(node.ID, inputDef.ID, secret)

	return nil
}

// resolvePluginInput handles type: plugin inputs with env_mapping
func (r *EnvResolver) resolvePluginInput(
	ctx context.Context,
	node NodeState,
	inputDef config.InputDefinition,
	runtimeCtx *RuntimeContext,
	env map[string]string,
) error {
	// Get the dependency node ID from the node's inputs
	depNodeID, ok := node.Inputs[inputDef.ID]
	if !ok || depNodeID == "" {
		return fmt.Errorf("plugin input %s has no connected node", inputDef.ID)
	}

	// Verify dependency outputs exist
	depOutputs, exists := runtimeCtx.NodeOutputs[depNodeID]
	if !exists {
		return &DependencyNotFoundError{NodeID: node.ID, DependencyID: depNodeID}
	}

	// Apply env_mapping: left side = env var to inject, right side = export key from dependency
	for envVarName, exportKey := range inputDef.EnvMapping {
		exportValue, ok := depOutputs[exportKey]
		if !ok {
			return &ExportNotFoundError{NodeID: depNodeID, ExportKey: exportKey}
		}
		env[envVarName] = exportValue
	}

	return nil
}

// generateAutoExports creates the standard exports for a node
func (r *EnvResolver) generateAutoExports(
	node NodeState,
	pluginConfig *config.PluginConfig,
	runtimeCtx *RuntimeContext,
	env map[string]string,
) error {
	portAssign, ok := runtimeCtx.AssignedPorts[node.ID]
	if !ok {
		// No port assigned - might be a node without port input
		// Use a default of 8080 for internal URL generation
		portAssign = PortAssignment{HostPort: 0, ContainerPort: 8080}
	}

	// INTERNAL_HOST: the node ID serves as DNS name within project network
	internalHost := node.ID
	runtimeCtx.SetNodeOutput(node.ID, "INTERNAL_HOST", internalHost)

	// INTERNAL_PORT: the container's internal port
	internalPort := strconv.Itoa(portAssign.ContainerPort)
	runtimeCtx.SetNodeOutput(node.ID, "INTERNAL_PORT", internalPort)

	// INTERNAL_URL: combined internal URL
	internalURL := fmt.Sprintf("http://%s:%d", internalHost, portAssign.ContainerPort)
	runtimeCtx.SetNodeOutput(node.ID, "INTERNAL_URL", internalURL)

	// HOST_PORT: the allocated host port for external access
	if portAssign.HostPort > 0 {
		runtimeCtx.SetNodeOutput(node.ID, "HOST_PORT", strconv.Itoa(portAssign.HostPort))
	}

	// EXTERNAL_HOST: Traefik hostname
	externalHost := fmt.Sprintf("%s.%s.localhost", node.ID, runtimeCtx.ProjectName)
	runtimeCtx.SetNodeOutput(node.ID, "EXTERNAL_HOST", externalHost)

	// EXTERNAL_PORT: Traefik ingress port (80 for HTTP)
	runtimeCtx.SetNodeOutput(node.ID, "EXTERNAL_PORT", "80")

	// EXTERNAL_URL: full external URL
	externalURL := fmt.Sprintf("http://%s", externalHost)
	runtimeCtx.SetNodeOutput(node.ID, "EXTERNAL_URL", externalURL)

	return nil
}

// resolveCustomExports processes plugin-defined exports with template substitution
func (r *EnvResolver) resolveCustomExports(
	node NodeState,
	pluginConfig *config.PluginConfig,
	runtimeCtx *RuntimeContext,
	env map[string]string,
) error {
	// Get this node's outputs for template resolution
	nodeOutputs := runtimeCtx.NodeOutputs[node.ID]
	if nodeOutputs == nil {
		nodeOutputs = make(map[string]string)
	}

	// Also include resolved env vars for template resolution
	// This allows templates to reference secrets by their input ID
	for k, v := range env {
		// Find the input ID that maps to this env var
		for _, inputDef := range pluginConfig.Inputs {
			if inputDef.InjectAs == k {
				nodeOutputs[inputDef.ID] = v
				break
			}
		}
	}

	// Process each custom export
	for _, export := range pluginConfig.Exports {
		resolvedValue := resolveTemplates(export.Value, nodeOutputs)
		runtimeCtx.SetNodeOutput(node.ID, export.Key, resolvedValue)
	}

	return nil
}

// resolveTemplates replaces {{VARIABLE}} patterns with actual values
func resolveTemplates(template string, values map[string]string) string {
	return templatePattern.ReplaceAllStringFunc(template, func(match string) string {
		// Extract variable name from {{NAME}}
		varName := match[2 : len(match)-2]
		if val, ok := values[varName]; ok {
			return val
		}
		// Leave unresolved templates as-is (will cause visible error)
		return match
	})
}

// generateSecureSecret creates a cryptographically secure random string
func generateSecureSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Use URL-safe encoding and trim to desired length
	encoded := base64.URLEncoding.EncodeToString(bytes)
	if len(encoded) > length {
		encoded = encoded[:length]
	}
	return encoded, nil
}
