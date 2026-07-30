package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	pluginCapabilitySecretProvider = "secret-provider"
	pluginCapabilityClientAdapter  = "client-adapter"
	pluginCapabilityMCPTransform   = "mcp-transform"
	pluginCapabilityPromptProvider = "prompt-provider"
	pluginCapabilityRuleProvider   = "rule-provider"
	pluginCapabilityValidator      = "validator"
)

const pluginProtocolV1 = "ai-dev-plugin-v1"

const (
	pluginCodeNotFound                  = "plugin_not_found"
	pluginCodeInvalidManifest           = "invalid_plugin_manifest"
	pluginCodeUnsupportedManifestSchema = "unsupported_plugin_manifest_schema"
	pluginCodeInvalidIdentifier         = "invalid_plugin_identifier"
	pluginCodeDuplicateIdentifier       = "duplicate_plugin_identifier"
	pluginCodeExecutableNotFound        = "plugin_executable_not_found"
	pluginCodeExecutableNotExecutable   = "plugin_executable_not_executable"
	pluginCodeUnsupportedProtocol       = "unsupported_plugin_protocol"
	pluginCodeVersionIncompatible       = "plugin_version_incompatible"
	pluginCodePlatformIncompatible      = "plugin_platform_incompatible"
	pluginCodeArchitectureIncompatible  = "plugin_architecture_incompatible"
	pluginCodeCapabilityMismatch        = "plugin_capability_mismatch"
	pluginCodeCapabilityConflict        = "plugin_capability_conflict"
	pluginCodeHandshakeFailed           = "plugin_handshake_failed"
	pluginCodeTimeout                   = "plugin_timeout"
	pluginCodeOutputInvalid             = "plugin_output_invalid"
	pluginCodeOutputTooLarge            = "plugin_output_too_large"
	pluginCodeExecutionFailed           = "plugin_execution_failed"
	pluginCodeOperationUnsupported      = "plugin_operation_unsupported"
	pluginCodeDisabled                  = "plugin_disabled"
	pluginCodeConfigurationInvalid      = "plugin_configuration_invalid"
)

const (
	pluginDefaultTimeoutSeconds        = int64(10)
	pluginDefaultMaxStdoutBytes        = 256 * 1024
	pluginDefaultMaxStderrBytes        = 64 * 1024
	pluginDefaultMaxResponses          = 8
	pluginDefaultMaxMessageBytes       = 64 * 1024
	pluginDefaultMaxInputBytes   int64 = 512 * 1024
)

type pluginError struct {
	Code    string
	Message string
}

func (err pluginError) Error() string {
	if err.Code == "" {
		return err.Message
	}
	return fmt.Sprintf("code=%s %s", err.Code, err.Message)
}

type pluginManifest struct {
	Schema              string
	ID                  string
	Name                string
	Version             string
	Protocol            string
	Executable          string
	Capabilities        []string
	Description         string
	Author              string
	Homepage            string
	License             string
	MinimumAIDevVersion string
	MaximumAIDevVersion string
	Platforms           []string
	Architectures       []string
}

type pluginConfig struct {
	Enabled               bool
	TimeoutSeconds        int64
	Environment           map[string]string
	WorkingDirectory      string
	PassParentEnvironment bool
	Opaque                map[string]any
}

type pluginFinding struct {
	PluginID   string `json:"plugin_id,omitempty"`
	Capability string `json:"capability,omitempty"`
	Operation  string `json:"operation,omitempty"`
	Path       string `json:"path,omitempty"`
	Code       string `json:"code"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
}

type pluginRuntimeCapability struct {
	Name         string         `json:"name"`
	Operations   []string       `json:"operations"`
	InputSchema  string         `json:"input_schema_version"`
	OutputSchema string         `json:"output_schema_version"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

type discoveredPlugin struct {
	ManifestPath   string
	Directory      string
	ExecutablePath string
	SearchPath     string
	SearchSource   string
	Manifest       pluginManifest
	Config         pluginConfig
	Enabled        bool
	Compatible     bool
	Findings       []pluginFinding
	RuntimeCaps    []pluginRuntimeCapability
	HandshakeOK    bool
	HandshakeErr   string
}

type pluginDiscoveryResult struct {
	SearchPaths []pluginSearchPath
	Plugins     []discoveredPlugin
	Findings    []pluginFinding
}

type pluginSearchPath struct {
	Path   string
	Source string
}

type pluginListEntry struct {
	ID            string   `json:"id"`
	Version       string   `json:"version"`
	Protocol      string   `json:"protocol"`
	Capabilities  []string `json:"capabilities"`
	SourcePath    string   `json:"source_path"`
	Enabled       bool     `json:"enabled"`
	Compatibility string   `json:"compatibility"`
}

type pluginShowOutput struct {
	ManifestPath   string                    `json:"manifest_path"`
	ExecutablePath string                    `json:"resolved_executable_path"`
	Manifest       pluginManifest            `json:"manifest"`
	Enabled        bool                      `json:"enabled"`
	Compatibility  string                    `json:"compatibility"`
	RuntimeCaps    []pluginRuntimeCapability `json:"runtime_capabilities,omitempty"`
	Findings       []pluginFinding           `json:"findings"`
}

type pluginStatusOutput struct {
	DiscoveredPlugins   int             `json:"discovered_plugins"`
	EnabledPlugins      int             `json:"enabled_plugins"`
	CompatiblePlugins   int             `json:"compatible_plugins"`
	InvalidPlugins      int             `json:"invalid_plugins"`
	CapabilityConflicts int             `json:"capability_conflicts"`
	HandshakeFailures   int             `json:"handshake_failures"`
	Findings            []pluginFinding `json:"findings"`
}

type pluginRunOutput struct {
	PluginID    string          `json:"plugin_id"`
	Capability  string          `json:"capability"`
	Operation   string          `json:"operation"`
	Output      map[string]any  `json:"output,omitempty"`
	Diagnostics []pluginFinding `json:"diagnostics,omitempty"`
}

var pluginSupportedCapabilities = map[string]bool{
	pluginCapabilitySecretProvider: true,
	pluginCapabilityClientAdapter:  true,
	pluginCapabilityMCPTransform:   true,
	pluginCapabilityPromptProvider: true,
	pluginCapabilityRuleProvider:   true,
	pluginCapabilityValidator:      true,
}

func pluginCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "plugin requires a subcommand"}
	}
	switch arguments[0] {
	case "list":
		return pluginListCommand(paths, arguments[1:])
	case "show":
		return pluginShowCommand(paths, arguments[1:])
	case "validate":
		return pluginValidateCommand(paths, arguments[1:])
	case "status":
		return pluginStatusCommand(paths, arguments[1:])
	case "refresh":
		return pluginRefreshCommand(paths, arguments[1:])
	case "run":
		return pluginRunCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown plugin subcommand: %s", arguments[0])}
	}
}

func pluginListCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown plugin list option: %s", argument)}
		}
	}

	result, err := discoverPluginsForCurrentInvocation(paths)
	if err != nil {
		return err
	}
	entries := make([]pluginListEntry, 0, len(result.Plugins))
	for _, plugin := range sortDiscoveredPlugins(result.Plugins) {
		entries = append(entries, pluginListEntry{
			ID:            plugin.Manifest.ID,
			Version:       plugin.Manifest.Version,
			Protocol:      plugin.Manifest.Protocol,
			Capabilities:  cloneStringSlice(plugin.Manifest.Capabilities),
			SourcePath:    plugin.ManifestPath,
			Enabled:       plugin.Enabled,
			Compatibility: pluginCompatibilityStatus(plugin),
		})
	}

	if jsonOutput {
		content, err := json.MarshalIndent(map[string]any{"plugins": entries}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode plugin list JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	for _, entry := range entries {
		fmt.Printf("id=%s version=%s protocol=%s capabilities=%s source=%s enabled=%t compatibility=%s\n", entry.ID, entry.Version, entry.Protocol, strings.Join(entry.Capabilities, ","), entry.SourcePath, entry.Enabled, entry.Compatibility)
	}
	return nil
}

func pluginShowCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "plugin show requires a plugin identifier"}
	}
	pluginID := arguments[0]
	jsonOutput := false
	handshake := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		case "--handshake":
			handshake = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown plugin show option: %s", argument)}
		}
	}

	result, err := discoverPluginsForCurrentInvocation(paths)
	if err != nil {
		return err
	}
	plugin, err := selectPluginByID(result.Plugins, pluginID)
	if err != nil {
		return err
	}
	if handshake && plugin.Enabled && plugin.Compatible {
		if err := handshakeAndDiscoverCapabilities(&plugin); err != nil {
			plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeHandshakeFailed, Severity: "error", Message: err.Error()})
		}
	}

	output := pluginShowOutput{
		ManifestPath:   plugin.ManifestPath,
		ExecutablePath: plugin.ExecutablePath,
		Manifest:       plugin.Manifest,
		Enabled:        plugin.Enabled,
		Compatibility:  pluginCompatibilityStatus(plugin),
		RuntimeCaps:    append([]pluginRuntimeCapability{}, plugin.RuntimeCaps...),
		Findings:       sortPluginFindings(append([]pluginFinding{}, plugin.Findings...)),
	}

	if jsonOutput {
		content, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("encode plugin show JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	content, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plugin show output: %w", err)
	}
	fmt.Println(string(content))
	return nil
}

func pluginValidateCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	selectedID := ""
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			if strings.HasPrefix(argument, "--") {
				return UsageError{Message: fmt.Sprintf("unknown plugin validate option: %s", argument)}
			}
			if selectedID != "" {
				return UsageError{Message: "plugin validate accepts at most one plugin identifier"}
			}
			selectedID = argument
		}
	}

	result, err := discoverPluginsForCurrentInvocation(paths)
	if err != nil {
		return err
	}
	plugins := sortDiscoveredPlugins(result.Plugins)
	if selectedID != "" {
		plugin, err := selectPluginByID(plugins, selectedID)
		if err != nil {
			return err
		}
		plugins = []discoveredPlugin{plugin}
	}

	findings := append([]pluginFinding{}, result.Findings...)
	for index := range plugins {
		plugin := &plugins[index]
		if !plugin.Enabled || !plugin.Compatible {
			continue
		}
		if err := handshakeAndDiscoverCapabilities(plugin); err != nil {
			plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeHandshakeFailed, Severity: "error", Message: err.Error()})
		}
		findings = append(findings, plugin.Findings...)
	}
	findings = append(findings, pluginRuntimeConflictFindings(plugins)...)
	findings = sortPluginFindings(findings)
	valid := len(pluginErrorFindings(findings)) == 0

	if jsonOutput {
		payload := map[string]any{
			"valid":    valid,
			"findings": findings,
		}
		content, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode plugin validate JSON: %w", err)
		}
		fmt.Println(string(content))
	} else {
		for _, finding := range findings {
			fmt.Printf("[%s] plugin=%s capability=%s operation=%s code=%s path=%s message=%s\n", finding.Severity, finding.PluginID, finding.Capability, finding.Operation, finding.Code, finding.Path, finding.Message)
		}
		fmt.Printf("valid=%t\n", valid)
	}

	if valid {
		return nil
	}
	return pluginError{Code: pluginCodeInvalidManifest, Message: "plugin validation failed"}
}

func pluginStatusCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown plugin status option: %s", argument)}
		}
	}
	status, err := pluginStatus(paths, true)
	if err != nil {
		return err
	}

	if jsonOutput {
		content, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return fmt.Errorf("encode plugin status JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	fmt.Printf("discovered=%d enabled=%d compatible=%d invalid=%d conflicts=%d handshake_failures=%d\n", status.DiscoveredPlugins, status.EnabledPlugins, status.CompatiblePlugins, status.InvalidPlugins, status.CapabilityConflicts, status.HandshakeFailures)
	for _, finding := range status.Findings {
		fmt.Printf("[%s] plugin=%s capability=%s operation=%s code=%s message=%s\n", finding.Severity, finding.PluginID, finding.Capability, finding.Operation, finding.Code, finding.Message)
	}
	return nil
}

func pluginRefreshCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown plugin refresh option: %s", argument)}
		}
	}
	status, err := pluginStatus(paths, false)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"refreshed": true,
		"status":    status,
	}
	if jsonOutput {
		content, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode plugin refresh JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}
	fmt.Printf("plugin metadata refreshed: discovered=%d enabled=%d compatible=%d\n", status.DiscoveredPlugins, status.EnabledPlugins, status.CompatiblePlugins)
	return nil
}

func pluginRunCommand(paths Paths, arguments []string) error {
	if len(arguments) < 2 {
		return UsageError{Message: "plugin run requires <plugin-id> and <operation>"}
	}
	pluginID := arguments[0]
	operation := arguments[1]
	capability := ""
	inputPath := ""
	jsonOutput := false

	for index := 2; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--capability":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--capability requires a value"}
			}
			index++
			capability = arguments[index]
		case "--input":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--input requires a value"}
			}
			index++
			inputPath = arguments[index]
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown plugin run option: %s", argument)}
		}
	}

	input := map[string]any{}
	if inputPath != "" {
		content, err := os.ReadFile(inputPath)
		if err != nil {
			return pluginError{Code: pluginCodeConfigurationInvalid, Message: fmt.Sprintf("read input JSON: %v", err)}
		}
		if int64(len(content)) > pluginDefaultMaxInputBytes {
			return pluginError{Code: pluginCodeOutputTooLarge, Message: "input payload exceeds maximum size"}
		}
		if err := json.Unmarshal(content, &input); err != nil {
			return pluginError{Code: pluginCodeConfigurationInvalid, Message: "input payload must be a JSON object"}
		}
	}

	selectedCapability := capability
	if selectedCapability == "" {
		selectedCapability = "*"
	}
	response, err := runPluginCapabilityOperation(paths, pluginID, selectedCapability, operation, input)
	if err != nil {
		return err
	}

	if capability == "" {
		resolvedCapability, _ := response["_resolved_capability"].(string)
		capability = resolvedCapability
	}

	payload := pluginRunOutput{PluginID: pluginID, Capability: capability, Operation: operation}
	if output, ok := response["output"].(map[string]any); ok {
		payload.Output = output
	}
	if rawDiagnostics, ok := response["diagnostics"].([]any); ok {
		payload.Diagnostics = pluginDiagnosticsFromAny(pluginID, selectedCapability, operation, rawDiagnostics)
	}

	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plugin run output: %w", err)
	}
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func runPluginCapabilityOperation(paths Paths, pluginID string, capability string, operation string, input map[string]any) (map[string]any, error) {
	result, err := discoverPluginsForCurrentInvocation(paths)
	if err != nil {
		return nil, err
	}
	plugin, err := selectPluginByID(result.Plugins, pluginID)
	if err != nil {
		return nil, err
	}
	if !plugin.Enabled {
		return nil, pluginError{Code: pluginCodeDisabled, Message: fmt.Sprintf("plugin %q is disabled", pluginID)}
	}
	if !plugin.Compatible {
		return nil, pluginError{Code: pluginCodeVersionIncompatible, Message: fmt.Sprintf("plugin %q is incompatible", pluginID)}
	}
	if err := handshakeAndDiscoverCapabilities(&plugin); err != nil {
		return nil, pluginError{Code: pluginCodeHandshakeFailed, Message: fmt.Sprintf("plugin %q handshake failed", pluginID)}
	}

	selectedCapability := capability
	if capability == "*" || capability == "" {
		selectedCapability, err = selectCapabilityForOperation(plugin.RuntimeCaps, "", operation)
	} else {
		selectedCapability, err = selectCapabilityForOperation(plugin.RuntimeCaps, capability, operation)
	}
	if err != nil {
		return nil, err
	}

	response, stderrText, err := invokePluginOperation(context.Background(), plugin, map[string]any{
		"protocol":   pluginProtocolV1,
		"type":       "run",
		"plugin_id":  plugin.Manifest.ID,
		"capability": selectedCapability,
		"operation":  operation,
		"input":      input,
	})
	if err != nil {
		if stderrText != "" {
			return nil, pluginError{Code: pluginCodeExecutionFailed, Message: sanitizePluginMessage(stderrText)}
		}
		return nil, err
	}
	response["_resolved_capability"] = selectedCapability
	return response, nil
}

func pluginStatus(paths Paths, includeHandshake bool) (pluginStatusOutput, error) {
	result, err := discoverPluginsForCurrentInvocation(paths)
	if err != nil {
		return pluginStatusOutput{}, err
	}

	status := pluginStatusOutput{Findings: append([]pluginFinding{}, result.Findings...)}
	status.DiscoveredPlugins = len(result.Plugins)
	for index := range result.Plugins {
		plugin := &result.Plugins[index]
		if plugin.Enabled {
			status.EnabledPlugins++
		}
		if plugin.Compatible {
			status.CompatiblePlugins++
		}
		if len(pluginErrorFindings(plugin.Findings)) > 0 {
			status.InvalidPlugins++
		}
		status.Findings = append(status.Findings, plugin.Findings...)
		if includeHandshake && plugin.Enabled && plugin.Compatible {
			if err := handshakeAndDiscoverCapabilities(plugin); err != nil {
				status.HandshakeFailures++
				status.Findings = append(status.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeHandshakeFailed, Severity: "error", Message: err.Error()})
			} else {
				status.Findings = append(status.Findings, pluginCapabilityFindings(*plugin)...)
			}
		}
	}
	status.Findings = append(status.Findings, pluginRuntimeConflictFindings(result.Plugins)...)
	for _, finding := range status.Findings {
		if finding.Code == pluginCodeCapabilityConflict {
			status.CapabilityConflicts++
		}
	}
	status.Findings = sortPluginFindings(status.Findings)
	return status, nil
}

func discoverPluginsForCurrentInvocation(paths Paths) (pluginDiscoveryResult, error) {
	resolved := bestEffortResolvedConfiguration(paths)
	searchPaths := resolvedPluginSearchPaths(paths, activeRuntimeOptions, resolved)
	plugins := []discoveredPlugin{}
	findings := []pluginFinding{}

	for _, searchPath := range searchPaths {
		directoryEntries, err := os.ReadDir(searchPath.Path)
		if err != nil {
			continue
		}
		sort.Slice(directoryEntries, func(i, j int) bool { return directoryEntries[i].Name() < directoryEntries[j].Name() })
		for _, entry := range directoryEntries {
			if !entry.IsDir() {
				continue
			}
			pluginDir := filepath.Join(searchPath.Path, entry.Name())
			manifestPath := filepath.Join(pluginDir, "plugin.toml")
			plugin := discoveredPlugin{Directory: pluginDir, ManifestPath: manifestPath, SearchPath: searchPath.Path, SearchSource: searchPath.Source, Config: pluginConfig{Enabled: true, TimeoutSeconds: pluginDefaultTimeoutSeconds, Environment: map[string]string{}, Opaque: map[string]any{}}, Compatible: true}
			if !fileExists(manifestPath) {
				plugin.Compatible = false
				plugin.Findings = append(plugin.Findings, pluginFinding{Path: manifestPath, Code: pluginCodeInvalidManifest, Severity: "error", Message: "missing plugin manifest"})
				plugins = append(plugins, plugin)
				continue
			}
			manifestMap, err := readTOML(manifestPath)
			if err != nil {
				plugin.Compatible = false
				plugin.Findings = append(plugin.Findings, pluginFinding{Path: manifestPath, Code: pluginCodeInvalidManifest, Severity: "error", Message: "invalid plugin manifest TOML"})
				plugins = append(plugins, plugin)
				continue
			}
			manifest, manifestFindings := parsePluginManifest(manifestMap, manifestPath)
			plugin.Manifest = manifest
			plugin.Findings = append(plugin.Findings, manifestFindings...)
			applyPluginConfiguration(&plugin, resolved)
			plugin.Enabled = plugin.Config.Enabled
			if len(pluginErrorFindings(plugin.Findings)) > 0 {
				plugin.Compatible = false
			}
			if manifest.Executable != "" {
				executablePath := manifest.Executable
				if !filepath.IsAbs(executablePath) {
					executablePath = filepath.Join(pluginDir, executablePath)
				}
				plugin.ExecutablePath = executablePath
				if !fileExists(executablePath) {
					plugin.Compatible = false
					plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: manifest.ID, Path: executablePath, Code: pluginCodeExecutableNotFound, Severity: "error", Message: "plugin executable not found"})
				} else if !isExecutableFile(executablePath) {
					plugin.Compatible = false
					plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: manifest.ID, Path: executablePath, Code: pluginCodeExecutableNotExecutable, Severity: "error", Message: "plugin executable is not executable"})
				}
			}
			plugin.Findings = append(plugin.Findings, pluginCompatibilityFindings(plugin)...)
			if len(pluginErrorFindings(plugin.Findings)) > 0 {
				plugin.Compatible = false
			}
			plugins = append(plugins, plugin)
		}
	}

	idToIndexes := map[string][]int{}
	for index := range plugins {
		plugin := plugins[index]
		if plugin.Manifest.ID == "" {
			continue
		}
		idToIndexes[plugin.Manifest.ID] = append(idToIndexes[plugin.Manifest.ID], index)
	}
	for id, indexes := range idToIndexes {
		if len(indexes) < 2 {
			continue
		}
		for _, index := range indexes {
			plugins[index].Compatible = false
			plugins[index].Findings = append(plugins[index].Findings, pluginFinding{PluginID: id, Code: pluginCodeDuplicateIdentifier, Severity: "error", Message: "duplicate plugin identifier discovered"})
		}
		findings = append(findings, pluginFinding{PluginID: id, Code: pluginCodeDuplicateIdentifier, Severity: "error", Message: "duplicate plugin identifier discovered"})
	}

	return pluginDiscoveryResult{SearchPaths: searchPaths, Plugins: plugins, Findings: findings}, nil
}

func parsePluginManifest(configuration map[string]any, manifestPath string) (pluginManifest, []pluginFinding) {
	manifest := pluginManifest{}
	findings := []pluginFinding{}

	if schema, ok := configuration["schema"].(string); ok {
		manifest.Schema = schema
	}
	if manifest.Schema == "" {
		manifest.Schema = "v1"
	}
	if manifest.Schema != "v1" {
		findings = append(findings, pluginFinding{Path: manifestPath, Code: pluginCodeUnsupportedManifestSchema, Severity: "error", Message: "unsupported plugin manifest schema"})
	}

	manifest.ID, _ = configuration["id"].(string)
	manifest.Name, _ = configuration["name"].(string)
	manifest.Version, _ = configuration["version"].(string)
	manifest.Protocol, _ = configuration["protocol"].(string)
	manifest.Executable, _ = configuration["executable"].(string)
	manifest.Description, _ = configuration["description"].(string)
	manifest.Author, _ = configuration["author"].(string)
	manifest.Homepage, _ = configuration["homepage"].(string)
	manifest.License, _ = configuration["license"].(string)
	manifest.MinimumAIDevVersion, _ = configuration["minimum_ai_dev_version"].(string)
	manifest.MaximumAIDevVersion, _ = configuration["maximum_ai_dev_version"].(string)
	manifest.Capabilities = stringArrayFromAny(configuration["capabilities"])
	manifest.Platforms = stringArrayFromAny(configuration["platforms"])
	manifest.Architectures = stringArrayFromAny(configuration["architectures"])

	if err := validatePluginIdentifier(manifest.ID); err != nil {
		findings = append(findings, pluginFinding{PluginID: manifest.ID, Path: manifestPath, Code: pluginCodeInvalidIdentifier, Severity: "error", Message: err.Error()})
	}
	if strings.TrimSpace(manifest.Name) == "" {
		findings = append(findings, pluginFinding{PluginID: manifest.ID, Path: manifestPath, Code: pluginCodeInvalidManifest, Severity: "error", Message: "manifest field name is required"})
	}
	if strings.TrimSpace(manifest.Version) == "" {
		findings = append(findings, pluginFinding{PluginID: manifest.ID, Path: manifestPath, Code: pluginCodeInvalidManifest, Severity: "error", Message: "manifest field version is required"})
	}
	if strings.TrimSpace(manifest.Protocol) == "" {
		findings = append(findings, pluginFinding{PluginID: manifest.ID, Path: manifestPath, Code: pluginCodeInvalidManifest, Severity: "error", Message: "manifest field protocol is required"})
	}
	if strings.TrimSpace(manifest.Executable) == "" {
		findings = append(findings, pluginFinding{PluginID: manifest.ID, Path: manifestPath, Code: pluginCodeInvalidManifest, Severity: "error", Message: "manifest field executable is required"})
	}
	if len(manifest.Capabilities) == 0 {
		findings = append(findings, pluginFinding{PluginID: manifest.ID, Path: manifestPath, Code: pluginCodeInvalidManifest, Severity: "error", Message: "manifest field capabilities must contain at least one value"})
	}
	for _, capability := range manifest.Capabilities {
		if !pluginSupportedCapabilities[capability] {
			findings = append(findings, pluginFinding{PluginID: manifest.ID, Path: manifestPath, Capability: capability, Code: pluginCodeCapabilityMismatch, Severity: "error", Message: "unsupported plugin capability"})
		}
	}
	return manifest, findings
}

func validatePluginIdentifier(identifier string) error {
	if identifier == "" {
		return fmt.Errorf("plugin identifier cannot be empty")
	}
	for index, character := range identifier {
		isLetter := character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isHyphen := character == '-'
		isUnderscore := character == '_'
		isDot := character == '.'
		if index == 0 {
			if !isLetter && !isDigit {
				return fmt.Errorf("plugin identifier must start with ASCII letter or digit")
			}
			continue
		}
		if !isLetter && !isDigit && !isHyphen && !isUnderscore && !isDot {
			return fmt.Errorf("plugin identifier contains invalid characters")
		}
	}
	return nil
}

func pluginCompatibilityFindings(plugin discoveredPlugin) []pluginFinding {
	findings := []pluginFinding{}
	if plugin.Manifest.Protocol != "" && plugin.Manifest.Protocol != pluginProtocolV1 {
		findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeUnsupportedProtocol, Severity: "error", Message: fmt.Sprintf("unsupported plugin protocol %q", plugin.Manifest.Protocol)})
	}
	if min := strings.TrimSpace(plugin.Manifest.MinimumAIDevVersion); min != "" {
		if compareSemanticVersions(version, min) < 0 {
			findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeVersionIncompatible, Severity: "error", Message: fmt.Sprintf("requires ai-dev >= %s", min)})
		}
	}
	if max := strings.TrimSpace(plugin.Manifest.MaximumAIDevVersion); max != "" {
		if compareSemanticVersions(version, max) > 0 {
			findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeVersionIncompatible, Severity: "error", Message: fmt.Sprintf("requires ai-dev <= %s", max)})
		}
	}
	if len(plugin.Manifest.Platforms) > 0 && !containsString(plugin.Manifest.Platforms, runtime.GOOS) {
		findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodePlatformIncompatible, Severity: "error", Message: fmt.Sprintf("plugin does not support platform %s", runtime.GOOS)})
	}
	if len(plugin.Manifest.Architectures) > 0 && !containsString(plugin.Manifest.Architectures, runtime.GOARCH) {
		findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeArchitectureIncompatible, Severity: "error", Message: fmt.Sprintf("plugin does not support architecture %s", runtime.GOARCH)})
	}
	if plugin.Config.TimeoutSeconds <= 0 {
		findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeConfigurationInvalid, Severity: "error", Message: "timeout_seconds must be a positive integer"})
	}
	return findings
}

func applyPluginConfiguration(plugin *discoveredPlugin, resolved map[string]any) {
	plugin.Config = pluginConfig{Enabled: true, TimeoutSeconds: pluginDefaultTimeoutSeconds, Environment: map[string]string{}, Opaque: map[string]any{}}
	pluginsValue, ok := resolved["plugins"].(map[string]any)
	if !ok {
		plugin.Enabled = true
		return
	}
	entryRaw, exists := pluginsValue[plugin.Manifest.ID]
	if !exists {
		plugin.Enabled = true
		return
	}
	entry, ok := entryRaw.(map[string]any)
	if !ok {
		plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeConfigurationInvalid, Severity: "error", Message: "plugin configuration must be a table"})
		plugin.Compatible = false
		plugin.Enabled = false
		return
	}
	if enabled, exists := entry["enabled"]; exists {
		value, ok := enabled.(bool)
		if !ok {
			plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeConfigurationInvalid, Severity: "error", Message: "plugins.<id>.enabled must be a boolean"})
		} else {
			plugin.Config.Enabled = value
		}
	}
	if timeout, exists := entry["timeout_seconds"]; exists {
		switch typed := timeout.(type) {
		case int64:
			plugin.Config.TimeoutSeconds = typed
		case float64:
			plugin.Config.TimeoutSeconds = int64(typed)
		default:
			plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeConfigurationInvalid, Severity: "error", Message: "plugins.<id>.timeout_seconds must be numeric"})
		}
	}
	if workdir, exists := entry["working_directory"]; exists {
		value, ok := workdir.(string)
		if !ok {
			plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeConfigurationInvalid, Severity: "error", Message: "plugins.<id>.working_directory must be a string"})
		} else {
			plugin.Config.WorkingDirectory = value
		}
	}
	if envValue, exists := entry["environment"]; exists {
		envMap, ok := envValue.(map[string]any)
		if !ok {
			plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeConfigurationInvalid, Severity: "error", Message: "plugins.<id>.environment must be a table"})
		} else {
			for key, raw := range envMap {
				value, ok := raw.(string)
				if !ok {
					plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeConfigurationInvalid, Severity: "error", Message: fmt.Sprintf("plugins.<id>.environment.%s must be a string", key)})
					continue
				}
				plugin.Config.Environment[key] = value
			}
		}
	}
	if passParent, exists := entry["inherit_environment"]; exists {
		value, ok := passParent.(bool)
		if !ok {
			plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeConfigurationInvalid, Severity: "error", Message: "plugins.<id>.inherit_environment must be a boolean"})
		} else {
			plugin.Config.PassParentEnvironment = value
		}
	}
	if opaque, exists := entry["config"]; exists {
		if table, ok := opaque.(map[string]any); ok {
			plugin.Config.Opaque = cloneMap(table)
		} else {
			plugin.Findings = append(plugin.Findings, pluginFinding{PluginID: plugin.Manifest.ID, Code: pluginCodeConfigurationInvalid, Severity: "error", Message: "plugins.<id>.config must be a table"})
		}
	}
	plugin.Enabled = plugin.Config.Enabled
}

func resolvedPluginSearchPaths(paths Paths, options runtimeResolutionOptions, resolved map[string]any) []pluginSearchPath {
	result := []pluginSearchPath{}
	seen := map[string]bool{}
	appendPath := func(path string, source string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		resolvedPath := expandUserPath(path)
		if !filepath.IsAbs(resolvedPath) {
			resolvedPath = filepath.Clean(resolvedPath)
		}
		if seen[resolvedPath] {
			return
		}
		seen[resolvedPath] = true
		result = append(result, pluginSearchPath{Path: resolvedPath, Source: source})
	}
	for _, path := range options.PluginPaths {
		appendPath(path, "command-line")
	}
	for _, path := range strings.Split(os.Getenv("AI_DEV_PLUGIN_PATH"), string(os.PathListSeparator)) {
		appendPath(path, "environment")
	}
	for _, path := range configuredPluginSearchPaths(resolved) {
		appendPath(path, "configuration")
	}
	appendPath(filepath.Join(paths.DataHome, "plugins"), "default")
	return result
}

func configuredPluginSearchPaths(resolved map[string]any) []string {
	pluginsTable, ok := resolved["plugins"].(map[string]any)
	if !ok {
		return nil
	}
	return stringArrayFromAny(pluginsTable["paths"])
}

func bestEffortResolvedConfiguration(paths Paths) map[string]any {
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return map[string]any{}
	}
	_, resolved, _ := loadConfigurationSources(paths, info)
	if resolved == nil {
		return map[string]any{}
	}
	return resolved
}

func selectPluginByID(plugins []discoveredPlugin, identifier string) (discoveredPlugin, error) {
	matches := []discoveredPlugin{}
	for _, plugin := range plugins {
		if plugin.Manifest.ID == identifier {
			matches = append(matches, plugin)
		}
	}
	if len(matches) == 0 {
		return discoveredPlugin{}, pluginError{Code: pluginCodeNotFound, Message: fmt.Sprintf("plugin %q not found", identifier)}
	}
	if len(matches) > 1 {
		return discoveredPlugin{}, pluginError{Code: pluginCodeDuplicateIdentifier, Message: fmt.Sprintf("duplicate plugin identifier %q", identifier)}
	}
	return matches[0], nil
}

func handshakeAndDiscoverCapabilities(plugin *discoveredPlugin) error {
	handshakeResponse, stderrText, err := invokePluginOperation(context.Background(), *plugin, map[string]any{
		"protocol":       pluginProtocolV1,
		"type":           "handshake",
		"ai_dev_version": version,
		"plugin_id":      plugin.Manifest.ID,
	})
	if err != nil {
		plugin.HandshakeOK = false
		if stderrText != "" {
			plugin.HandshakeErr = sanitizePluginMessage(stderrText)
		}
		return err
	}

	responseID, _ := handshakeResponse["plugin_id"].(string)
	if responseID != "" && responseID != plugin.Manifest.ID {
		plugin.HandshakeOK = false
		plugin.HandshakeErr = "handshake plugin identifier mismatch"
		return pluginError{Code: pluginCodeHandshakeFailed, Message: plugin.HandshakeErr}
	}
	responseVersion, _ := handshakeResponse["plugin_version"].(string)
	if responseVersion != "" && responseVersion != plugin.Manifest.Version {
		plugin.HandshakeOK = false
		plugin.HandshakeErr = "handshake plugin version mismatch"
		return pluginError{Code: pluginCodeHandshakeFailed, Message: plugin.HandshakeErr}
	}

	capabilityResponse, stderrText, err := invokePluginOperation(context.Background(), *plugin, map[string]any{
		"protocol":  pluginProtocolV1,
		"type":      "capabilities",
		"plugin_id": plugin.Manifest.ID,
	})
	if err != nil {
		plugin.HandshakeOK = false
		if stderrText != "" {
			plugin.HandshakeErr = sanitizePluginMessage(stderrText)
		}
		return err
	}

	runtimeCaps := parsePluginRuntimeCapabilities(capabilityResponse)
	plugin.RuntimeCaps = runtimeCaps
	plugin.HandshakeOK = true
	plugin.HandshakeErr = ""
	return nil
}

func invokePluginOperation(ctx context.Context, plugin discoveredPlugin, request map[string]any) (map[string]any, string, error) {
	if plugin.ExecutablePath == "" {
		return nil, "", pluginError{Code: pluginCodeExecutableNotFound, Message: fmt.Sprintf("plugin executable for %q is not configured", plugin.Manifest.ID)}
	}
	timeout := plugin.Config.TimeoutSeconds
	if timeout <= 0 {
		timeout = pluginDefaultTimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, plugin.ExecutablePath)
	workingDirectory := plugin.Directory
	if strings.TrimSpace(plugin.Config.WorkingDirectory) != "" {
		workingDirectory = plugin.Config.WorkingDirectory
		if !filepath.IsAbs(workingDirectory) {
			workingDirectory = filepath.Join(plugin.Directory, workingDirectory)
		}
	}
	command.Dir = workingDirectory
	command.Env = pluginProcessEnvironment(plugin)

	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, "", pluginError{Code: pluginCodeExecutionFailed, Message: "open plugin stdin"}
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, "", pluginError{Code: pluginCodeExecutionFailed, Message: "open plugin stdout"}
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, "", pluginError{Code: pluginCodeExecutionFailed, Message: "open plugin stderr"}
	}

	if err := command.Start(); err != nil {
		return nil, "", pluginError{Code: pluginCodeExecutionFailed, Message: fmt.Sprintf("start plugin process: %v", err)}
	}

	stderrBuffer := &limitedBuffer{Limit: pluginDefaultMaxStderrBytes}
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(stderrBuffer, stderr)
		close(stderrDone)
	}()

	encoded, err := json.Marshal(request)
	if err != nil {
		_ = command.Process.Kill()
		return nil, "", pluginError{Code: pluginCodeConfigurationInvalid, Message: "encode plugin request"}
	}
	if _, err := stdin.Write(append(encoded, '\n')); err != nil {
		_ = stdin.Close()
		_ = command.Process.Kill()
		return nil, "", pluginError{Code: pluginCodeExecutionFailed, Message: "write plugin request"}
	}
	_ = stdin.Close()

	stdoutData, readErr := io.ReadAll(io.LimitReader(stdout, int64(pluginDefaultMaxStdoutBytes+1)))
	if readErr != nil {
		_ = command.Process.Kill()
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeOutputInvalid, Message: "read plugin stdout"}
	}
	if len(stdoutData) > pluginDefaultMaxStdoutBytes {
		_ = command.Process.Kill()
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeOutputTooLarge, Message: "plugin stdout exceeded allowed size"}
	}

	waitErr := command.Wait()
	<-stderrDone
	if ctx.Err() == context.DeadlineExceeded {
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeTimeout, Message: fmt.Sprintf("plugin %q timed out", plugin.Manifest.ID)}
	}
	if waitErr != nil {
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeExecutionFailed, Message: fmt.Sprintf("plugin process failed: %v", waitErr)}
	}
	if stderrBuffer.Overflowed {
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeOutputTooLarge, Message: "plugin stderr exceeded allowed size"}
	}

	responses := parseNDJSONMessages(stdoutData)
	if len(responses) == 0 {
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeOutputInvalid, Message: "plugin produced no protocol response"}
	}
	if len(responses) > pluginDefaultMaxResponses {
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeOutputTooLarge, Message: "plugin produced too many protocol responses"}
	}

	first := responses[0]
	if len(first.Raw) > pluginDefaultMaxMessageBytes {
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeOutputTooLarge, Message: "plugin response message exceeded allowed size"}
	}
	if first.ParseErr != nil {
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeOutputInvalid, Message: "plugin response is not valid JSON"}
	}

	response := first.Data
	protocol, _ := response["protocol"].(string)
	if protocol != pluginProtocolV1 {
		return nil, stderrBuffer.String(), pluginError{Code: pluginCodeUnsupportedProtocol, Message: "plugin response protocol mismatch"}
	}
	if ok, exists := response["ok"]; exists {
		if boolValue, cast := ok.(bool); cast && !boolValue {
			message, _ := response["message"].(string)
			if message == "" {
				message = "plugin reported failure"
			}
			code, _ := response["code"].(string)
			if code == "" {
				code = pluginCodeExecutionFailed
			}
			return nil, stderrBuffer.String(), pluginError{Code: code, Message: sanitizePluginMessage(message)}
		}
	}
	return response, stderrBuffer.String(), nil
}

func pluginProcessEnvironment(plugin discoveredPlugin) []string {
	base := map[string]string{}
	for _, key := range []string{"PATH", "HOME", "TMPDIR"} {
		if value := os.Getenv(key); value != "" {
			base[key] = value
		}
	}
	base["AI_DEV_PLUGIN_PROTOCOL"] = pluginProtocolV1
	base["AI_DEV_PLUGIN_ID"] = plugin.Manifest.ID

	if plugin.Config.PassParentEnvironment {
		for _, value := range os.Environ() {
			parts := strings.SplitN(value, "=", 2)
			if len(parts) != 2 {
				continue
			}
			if _, exists := base[parts[0]]; exists {
				continue
			}
			base[parts[0]] = parts[1]
		}
	}
	for key, value := range plugin.Config.Environment {
		base[key] = value
	}

	keys := make([]string, 0, len(base))
	for key := range base {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+base[key])
	}
	return result
}

type ndjsonMessage struct {
	Raw      []byte
	Data     map[string]any
	ParseErr error
}

func parseNDJSONMessages(content []byte) []ndjsonMessage {
	messages := []ndjsonMessage{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		message := ndjsonMessage{Raw: append([]byte{}, line...), Data: map[string]any{}}
		message.ParseErr = json.Unmarshal(line, &message.Data)
		messages = append(messages, message)
	}
	if err := scanner.Err(); err != nil {
		messages = append(messages, ndjsonMessage{ParseErr: err})
	}
	return messages
}

type limitedBuffer struct {
	Limit      int
	Buffer     bytes.Buffer
	Overflowed bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	if buffer.Overflowed {
		return len(data), nil
	}
	remaining := buffer.Limit - buffer.Buffer.Len()
	if remaining <= 0 {
		buffer.Overflowed = true
		return len(data), nil
	}
	if len(data) > remaining {
		_, _ = buffer.Buffer.Write(data[:remaining])
		buffer.Overflowed = true
		return len(data), nil
	}
	return buffer.Buffer.Write(data)
}

func (buffer *limitedBuffer) String() string {
	return buffer.Buffer.String()
}

func parsePluginRuntimeCapabilities(response map[string]any) []pluginRuntimeCapability {
	rawCapabilities, ok := response["capabilities"].([]any)
	if !ok {
		return nil
	}
	capabilities := make([]pluginRuntimeCapability, 0, len(rawCapabilities))
	for _, raw := range rawCapabilities {
		table, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		capability := pluginRuntimeCapability{}
		capability.Name, _ = table["name"].(string)
		capability.InputSchema, _ = table["input_schema_version"].(string)
		capability.OutputSchema, _ = table["output_schema_version"].(string)
		capability.Operations = stringArrayFromAny(table["operations"])
		if metadata, ok := table["metadata"].(map[string]any); ok {
			capability.Metadata = cloneMap(metadata)
		}
		capabilities = append(capabilities, capability)
	}
	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i].Name < capabilities[j].Name
	})
	return capabilities
}

func selectCapabilityForOperation(capabilities []pluginRuntimeCapability, capabilityName string, operation string) (string, error) {
	matches := []string{}
	for _, capability := range capabilities {
		if capabilityName != "" && capability.Name != capabilityName {
			continue
		}
		if containsString(capability.Operations, operation) {
			matches = append(matches, capability.Name)
		}
	}
	if len(matches) == 0 {
		return "", pluginError{Code: pluginCodeOperationUnsupported, Message: fmt.Sprintf("operation %q is not declared by plugin", operation)}
	}
	if len(matches) > 1 {
		return "", pluginError{Code: pluginCodeOperationUnsupported, Message: fmt.Sprintf("operation %q is ambiguous; specify --capability", operation)}
	}
	return matches[0], nil
}

func pluginCapabilityFindings(plugin discoveredPlugin) []pluginFinding {
	findings := []pluginFinding{}
	manifestCapabilities := map[string]bool{}
	for _, capability := range plugin.Manifest.Capabilities {
		manifestCapabilities[capability] = true
	}
	for _, runtimeCapability := range plugin.RuntimeCaps {
		if !manifestCapabilities[runtimeCapability.Name] {
			findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: runtimeCapability.Name, Code: pluginCodeCapabilityMismatch, Severity: "error", Message: "runtime capability is not declared in manifest"})
		}
	}
	for capability := range manifestCapabilities {
		found := false
		for _, runtimeCapability := range plugin.RuntimeCaps {
			if runtimeCapability.Name == capability {
				found = true
				break
			}
		}
		if !found {
			findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: capability, Code: pluginCodeCapabilityMismatch, Severity: "error", Message: "manifest capability is missing from runtime declaration"})
		}
	}
	return findings
}

func pluginRuntimeConflictFindings(plugins []discoveredPlugin) []pluginFinding {
	findings := []pluginFinding{}
	secretProviders := map[string]string{
		secretProviderEnv:     "built-in",
		secretProviderCommand: "built-in",
	}
	clientIDs := map[string]string{
		clientNameCodex:  "built-in",
		clientNameClaude: "built-in",
		clientNameCursor: "built-in",
		clientNameVSCode: "built-in",
	}

	for _, plugin := range plugins {
		if !plugin.Enabled || !plugin.Compatible || !plugin.HandshakeOK {
			continue
		}
		for _, capability := range plugin.RuntimeCaps {
			switch capability.Name {
			case pluginCapabilitySecretProvider:
				for _, providerName := range stringArrayFromAny(capability.Metadata["providers"]) {
					if owner, exists := secretProviders[providerName]; exists {
						findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: capability.Name, Operation: "register", Code: pluginCodeCapabilityConflict, Severity: "error", Message: fmt.Sprintf("secret provider %q conflicts with %s", providerName, owner)})
						continue
					}
					secretProviders[providerName] = plugin.Manifest.ID
				}
			case pluginCapabilityClientAdapter:
				for _, clientID := range stringArrayFromAny(capability.Metadata["clients"]) {
					if owner, exists := clientIDs[clientID]; exists {
						findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: capability.Name, Operation: "register", Code: pluginCodeCapabilityConflict, Severity: "error", Message: fmt.Sprintf("client identifier %q conflicts with %s", clientID, owner)})
						continue
					}
					clientIDs[clientID] = plugin.Manifest.ID
				}
			}
		}
	}

	return findings
}

func pluginSecretProviderRegistrations(paths Paths) (map[string]discoveredPlugin, []pluginFinding) {
	result := map[string]discoveredPlugin{}
	findings := []pluginFinding{}
	discovery, err := discoverPluginsForCurrentInvocation(paths)
	if err != nil {
		return result, []pluginFinding{{Code: pluginCodeExecutionFailed, Severity: "error", Message: err.Error()}}
	}
	for index := range discovery.Plugins {
		plugin := &discovery.Plugins[index]
		if !plugin.Enabled || !plugin.Compatible {
			continue
		}
		if !containsString(plugin.Manifest.Capabilities, pluginCapabilitySecretProvider) {
			continue
		}
		if err := handshakeAndDiscoverCapabilities(plugin); err != nil {
			findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: pluginCapabilitySecretProvider, Code: pluginCodeHandshakeFailed, Severity: "error", Message: err.Error()})
			continue
		}
		for _, runtimeCapability := range plugin.RuntimeCaps {
			if runtimeCapability.Name != pluginCapabilitySecretProvider {
				continue
			}
			for _, provider := range stringArrayFromAny(runtimeCapability.Metadata["providers"]) {
				if provider == secretProviderEnv || provider == secretProviderCommand {
					findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: pluginCapabilitySecretProvider, Operation: "register", Code: pluginCodeCapabilityConflict, Severity: "error", Message: fmt.Sprintf("secret provider %q conflicts with built-in provider", provider)})
					continue
				}
				if existing, exists := result[provider]; exists {
					findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: pluginCapabilitySecretProvider, Operation: "register", Code: pluginCodeCapabilityConflict, Severity: "error", Message: fmt.Sprintf("secret provider %q conflicts with plugin %q", provider, existing.Manifest.ID)})
					continue
				}
				result[provider] = *plugin
			}
		}
	}
	return result, findings
}

type pluginSecretProvider struct {
	Plugin   discoveredPlugin
	Provider string
}

func (provider pluginSecretProvider) Resolve(ctx context.Context, reference SecretReference) (string, error) {
	response, stderrText, err := invokePluginOperation(ctx, provider.Plugin, map[string]any{
		"protocol":   pluginProtocolV1,
		"type":       "run",
		"plugin_id":  provider.Plugin.Manifest.ID,
		"capability": pluginCapabilitySecretProvider,
		"operation":  "resolve",
		"input": map[string]any{
			"provider":  provider.Provider,
			"reference": reference.Reference,
		},
	})
	if err != nil {
		message := err.Error()
		if stderrText != "" {
			message = sanitizePluginMessage(stderrText)
		}
		return "", secretError{Code: secretCodeResolutionFailed, Provider: reference.Provider, Reference: reference.Reference, Message: message}
	}
	output, ok := response["output"].(map[string]any)
	if !ok {
		return "", secretError{Code: secretCodeResolutionFailed, Provider: reference.Provider, Reference: reference.Reference, Message: "plugin secret provider returned invalid output"}
	}
	value, _ := output["value"].(string)
	if strings.TrimSpace(value) == "" {
		return "", secretError{Code: secretCodeMissingValue, Provider: reference.Provider, Reference: reference.Reference, Message: "plugin secret provider returned empty value"}
	}
	return value, nil
}

func registerPluginSecretProviders(paths Paths, resolver *secretResolver) []pluginFinding {
	registrations, findings := pluginSecretProviderRegistrations(paths)
	for providerName, plugin := range registrations {
		if err := resolver.RegisterProvider(providerName, pluginSecretProvider{Plugin: plugin, Provider: providerName}); err != nil {
			findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: pluginCapabilitySecretProvider, Operation: "register", Code: pluginCodeCapabilityConflict, Severity: "error", Message: err.Error()})
		}
	}
	return findings
}

func pluginProvidedRegistryResources(paths Paths, kind string) ([]registryResource, []pluginFinding) {
	resources := []registryResource{}
	findings := []pluginFinding{}
	targetCapability := pluginCapabilityPromptProvider
	if kind == registryKindRule {
		targetCapability = pluginCapabilityRuleProvider
	}
	discovery, err := discoverPluginsForCurrentInvocation(paths)
	if err != nil {
		return resources, []pluginFinding{{Code: pluginCodeExecutionFailed, Severity: "error", Message: err.Error()}}
	}
	for index := range discovery.Plugins {
		plugin := &discovery.Plugins[index]
		if !plugin.Enabled || !plugin.Compatible {
			continue
		}
		if !containsString(plugin.Manifest.Capabilities, targetCapability) {
			continue
		}
		if err := handshakeAndDiscoverCapabilities(plugin); err != nil {
			findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: targetCapability, Code: pluginCodeHandshakeFailed, Severity: "error", Message: err.Error()})
			continue
		}
		response, stderrText, err := invokePluginOperation(context.Background(), *plugin, map[string]any{
			"protocol":   pluginProtocolV1,
			"type":       "run",
			"plugin_id":  plugin.Manifest.ID,
			"capability": targetCapability,
			"operation":  "list",
			"input":      map[string]any{"kind": kind},
		})
		if err != nil {
			message := err.Error()
			if stderrText != "" {
				message = sanitizePluginMessage(stderrText)
			}
			findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: targetCapability, Operation: "list", Code: pluginCodeExecutionFailed, Severity: "error", Message: message})
			continue
		}
		rawResources, ok := response["resources"].([]any)
		if !ok {
			findings = append(findings, pluginFinding{PluginID: plugin.Manifest.ID, Capability: targetCapability, Operation: "list", Code: pluginCodeOutputInvalid, Severity: "error", Message: "plugin resource response must include resources array"})
			continue
		}
		for _, raw := range rawResources {
			table, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			identifier, _ := table["identifier"].(string)
			content, _ := table["content"].(string)
			format, _ := table["format"].(string)
			if format == "" {
				format = "md"
			}
			if identifier == "" || content == "" {
				continue
			}
			metadata := registryMetadata{}
			if rawMetadata, ok := table["metadata"].(map[string]any); ok {
				metadata = decodeRegistryMetadata(rawMetadata)
			}
			resources = append(resources, registryResource{Kind: kind, Identifier: identifier, Source: fmt.Sprintf("plugin:%s", plugin.Manifest.ID), Format: format, Metadata: metadata, Content: content})
		}
	}
	return resources, findings
}

func decodeRegistryMetadata(table map[string]any) registryMetadata {
	metadata := registryMetadata{}
	metadata.Title, _ = table["title"].(string)
	metadata.Description, _ = table["description"].(string)
	metadata.Version, _ = table["version"].(string)
	metadata.Author, _ = table["author"].(string)
	metadata.Tags = stringArrayFromAny(table["tags"])
	return metadata
}

func pluginValidatorFindings(paths Paths, resolved map[string]any) []ValidationFinding {
	findings := []ValidationFinding{}
	discovery, err := discoverPluginsForCurrentInvocation(paths)
	if err != nil {
		findings = append(findings, ValidationFinding{Source: "(plugin)", Path: "$", Code: pluginCodeExecutionFailed, Severity: "error", Message: err.Error()})
		return findings
	}
	for index := range discovery.Plugins {
		plugin := &discovery.Plugins[index]
		if !plugin.Enabled || !plugin.Compatible {
			continue
		}
		if !containsString(plugin.Manifest.Capabilities, pluginCapabilityValidator) {
			continue
		}
		if err := handshakeAndDiscoverCapabilities(plugin); err != nil {
			findings = append(findings, ValidationFinding{Source: plugin.ManifestPath, Path: "$", Code: pluginCodeHandshakeFailed, Severity: "error", Message: sanitizePluginMessage(err.Error())})
			continue
		}
		response, _, err := invokePluginOperation(context.Background(), *plugin, map[string]any{
			"protocol":   pluginProtocolV1,
			"type":       "run",
			"plugin_id":  plugin.Manifest.ID,
			"capability": pluginCapabilityValidator,
			"operation":  "validate",
			"input":      map[string]any{"resolved": cloneMap(resolved)},
		})
		if err != nil {
			findings = append(findings, ValidationFinding{Source: plugin.ManifestPath, Path: "$", Code: pluginCodeExecutionFailed, Severity: "error", Message: sanitizePluginMessage(err.Error())})
			continue
		}
		rawFindings, _ := response["findings"].([]any)
		for _, raw := range rawFindings {
			table, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			path, _ := table["path"].(string)
			code, _ := table["code"].(string)
			severity, _ := table["severity"].(string)
			message, _ := table["message"].(string)
			if severity == "" {
				severity = "error"
			}
			if code == "" {
				code = pluginCodeOutputInvalid
			}
			if path == "" {
				path = "$"
			}
			findings = append(findings, ValidationFinding{Source: fmt.Sprintf("plugin:%s", plugin.Manifest.ID), Path: path, Code: code, Severity: severity, Message: sanitizePluginMessage(message)})
		}
	}
	sortValidationFindings(findings)
	return findings
}

func pluginDiagnosticsFromAny(pluginID string, capability string, operation string, diagnostics []any) []pluginFinding {
	findings := []pluginFinding{}
	for _, raw := range diagnostics {
		table, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		severity, _ := table["severity"].(string)
		if severity == "" {
			severity = "error"
		}
		code, _ := table["code"].(string)
		if code == "" {
			code = pluginCodeOutputInvalid
		}
		message, _ := table["message"].(string)
		path, _ := table["path"].(string)
		findings = append(findings, pluginFinding{PluginID: pluginID, Capability: capability, Operation: operation, Path: path, Code: code, Severity: severity, Message: sanitizePluginMessage(message)})
	}
	return sortPluginFindings(findings)
}

func sortDiscoveredPlugins(plugins []discoveredPlugin) []discoveredPlugin {
	result := append([]discoveredPlugin{}, plugins...)
	sort.Slice(result, func(i, j int) bool {
		left := result[i]
		right := result[j]
		if left.Manifest.ID != right.Manifest.ID {
			return left.Manifest.ID < right.Manifest.ID
		}
		if left.SearchPath != right.SearchPath {
			return left.SearchPath < right.SearchPath
		}
		return left.ManifestPath < right.ManifestPath
	})
	return result
}

func sortPluginFindings(findings []pluginFinding) []pluginFinding {
	result := append([]pluginFinding{}, findings...)
	sort.Slice(result, func(i, j int) bool {
		left := result[i]
		right := result[j]
		if left.PluginID != right.PluginID {
			return left.PluginID < right.PluginID
		}
		if left.Capability != right.Capability {
			return left.Capability < right.Capability
		}
		if left.Operation != right.Operation {
			return left.Operation < right.Operation
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Message != right.Message {
			return left.Message < right.Message
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		return left.Severity < right.Severity
	})
	return result
}

func pluginErrorFindings(findings []pluginFinding) []pluginFinding {
	result := []pluginFinding{}
	for _, finding := range findings {
		if strings.EqualFold(finding.Severity, "error") {
			result = append(result, finding)
		}
	}
	return result
}

func pluginCompatibilityStatus(plugin discoveredPlugin) string {
	if !plugin.Enabled {
		return "disabled"
	}
	if plugin.Compatible {
		return "compatible"
	}
	return "incompatible"
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	mode := info.Mode()
	return mode&0o111 != 0
}

func stringArrayFromAny(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := []string{}
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		result = append(result, text)
	}
	return uniqueStrings(result)
}

func expandUserPath(path string) string {
	if path == "" || !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func compareSemanticVersions(left string, right string) int {
	leftParts := semanticVersionParts(left)
	rightParts := semanticVersionParts(right)
	max := len(leftParts)
	if len(rightParts) > max {
		max = len(rightParts)
	}
	for index := 0; index < max; index++ {
		leftValue := 0
		if index < len(leftParts) {
			leftValue = leftParts[index]
		}
		rightValue := 0
		if index < len(rightParts) {
			rightValue = rightParts[index]
		}
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}

func semanticVersionParts(value string) []int {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "v")
	if trimmed == "" {
		return []int{0}
	}
	segments := strings.Split(trimmed, ".")
	result := make([]int, 0, len(segments))
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			result = append(result, 0)
			continue
		}
		stop := len(segment)
		for index, character := range segment {
			if character < '0' || character > '9' {
				stop = index
				break
			}
		}
		numeric := segment[:stop]
		if numeric == "" {
			result = append(result, 0)
			continue
		}
		value, err := strconv.Atoi(numeric)
		if err != nil {
			result = append(result, 0)
			continue
		}
		result = append(result, value)
	}
	return result
}

func sanitizePluginMessage(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "plugin operation failed"
	}
	lower := strings.ToLower(trimmed)
	for _, token := range []string{"secret://", "token", "password", "authorization", "api_key", "apikey"} {
		if strings.Contains(lower, token) {
			return "plugin operation failed with redacted diagnostics"
		}
	}
	return trimmed
}
