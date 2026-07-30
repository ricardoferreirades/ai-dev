package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	mcpTransportStdio = "stdio"
	mcpTransportHTTP  = "http"
)

const (
	mcpCodeInvalidServerName        = "invalid_mcp_server_name"
	mcpCodeDuplicateServer          = "duplicate_mcp_server"
	mcpCodeUnsupportedTransport     = "unsupported_mcp_transport"
	mcpCodeMissingCommand           = "missing_mcp_command"
	mcpCodeInvalidCommand           = "invalid_mcp_command"
	mcpCodeMissingURL               = "missing_mcp_url"
	mcpCodeInvalidURL               = "invalid_mcp_url"
	mcpCodeConflictingFields        = "conflicting_mcp_fields"
	mcpCodeInvalidArgs              = "invalid_mcp_args"
	mcpCodeInvalidEnvironment       = "invalid_mcp_environment"
	mcpCodeInvalidHeaders           = "invalid_mcp_headers"
	mcpCodeInvalidTimeout           = "invalid_mcp_timeout"
	mcpCodeCommandNotFound          = "mcp_command_not_found"
	mcpCodeWorkingDirectoryNotFound = "mcp_working_directory_not_found"
	mcpCodeSecretResolutionFailed   = "mcp_secret_resolution_failed"
	mcpCodeServerNotFound           = "mcp_server_not_found"
)

type mcpError struct {
	Code    string
	Message string
}

func (err mcpError) Error() string {
	if err.Code == "" {
		return err.Message
	}
	return fmt.Sprintf("code=%s %s", err.Code, err.Message)
}

type MCPServer struct {
	Name               string
	Transport          string
	Command            string
	Args               []string
	Cwd                string
	URL                string
	Headers            map[string]string
	Environment        map[string]string
	Enabled            bool
	TimeoutSeconds     int64
	InheritEnvironment bool
	Scope              string
}

type mcpDiagnostic struct {
	Name    string
	Path    string
	Code    string
	Message string
}

type MCPListEntry struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	Enabled   bool   `json:"enabled"`
	Scope     string `json:"scope"`
}

type MCPShowEntry struct {
	Name               string            `json:"name"`
	Scope              string            `json:"scope"`
	Transport          string            `json:"transport"`
	Command            string            `json:"command,omitempty"`
	Args               []string          `json:"args,omitempty"`
	Cwd                string            `json:"cwd,omitempty"`
	URL                string            `json:"url,omitempty"`
	Headers            map[string]string `json:"headers,omitempty"`
	Environment        map[string]string `json:"environment,omitempty"`
	Enabled            bool              `json:"enabled"`
	TimeoutSeconds     int64             `json:"timeout_seconds,omitempty"`
	InheritEnvironment bool              `json:"inherit_environment,omitempty"`
}

type MCPResolveOutput struct {
	Servers map[string]MCPResolveServer `json:"servers"`
}

type MCPResolveServer struct {
	Transport          string            `json:"transport"`
	Command            string            `json:"command,omitempty"`
	Args               []string          `json:"args,omitempty"`
	Cwd                string            `json:"cwd,omitempty"`
	URL                string            `json:"url,omitempty"`
	Headers            map[string]string `json:"headers,omitempty"`
	Environment        map[string]string `json:"environment,omitempty"`
	Enabled            bool              `json:"enabled"`
	TimeoutSeconds     int64             `json:"timeout_seconds,omitempty"`
	InheritEnvironment bool              `json:"inherit_environment,omitempty"`
}

type MCPCheckIssue struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type MCPCheckResult struct {
	Name      string          `json:"name"`
	Transport string          `json:"transport"`
	Valid     bool            `json:"valid"`
	Checks    []string        `json:"checks"`
	Errors    []MCPCheckIssue `json:"errors"`
}

type MCPCheckOutput struct {
	Valid   bool             `json:"valid"`
	Results []MCPCheckResult `json:"results"`
}

type MCPDoctorSummary struct {
	ConfiguredServers       int
	EnabledServers          int
	InvalidDefinitions      int
	UnavailableExecutables  int
	InvalidWorkingDirectory int
	UnresolvedSecrets       int
	UnsupportedTransports   int
}

func mcpCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "mcp requires a subcommand"}
	}

	switch arguments[0] {
	case "list":
		return mcpListCommand(paths, arguments[1:])
	case "show":
		return mcpShowCommand(paths, arguments[1:])
	case "resolve":
		return mcpResolveCommand(paths, arguments[1:])
	case "check":
		return mcpCheckCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown mcp subcommand: %s", arguments[0])}
	}
}

func mcpListCommand(paths Paths, arguments []string) error {
	onlyEnabled := false
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--enabled":
			onlyEnabled = true
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown mcp list option: %s", argument)}
		}
	}

	_, servers, err := resolvedMCPServersForCommand(paths)
	if err != nil {
		return err
	}

	entries := make([]MCPListEntry, 0, len(servers))
	for _, name := range sortedMCPServerNames(servers) {
		server := servers[name]
		if onlyEnabled && !server.Enabled {
			continue
		}
		entries = append(entries, MCPListEntry{
			Name:      server.Name,
			Transport: server.Transport,
			Enabled:   server.Enabled,
			Scope:     server.Scope,
		})
	}

	if jsonOutput {
		content, err := json.MarshalIndent(map[string]any{"servers": entries}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode mcp list JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	for _, entry := range entries {
		fmt.Printf(
			"name=%s transport=%s enabled=%t scope=%s\n",
			entry.Name,
			entry.Transport,
			entry.Enabled,
			entry.Scope,
		)
	}

	return nil
}

func mcpShowCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "mcp show requires a server name"}
	}

	name := arguments[0]
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown mcp show option: %s", argument)}
		}
	}

	_, servers, err := resolvedMCPServersForCommand(paths)
	if err != nil {
		return err
	}

	server, exists := servers[name]
	if !exists {
		return mcpError{
			Code:    mcpCodeServerNotFound,
			Message: fmt.Sprintf("MCP server %q does not exist", name),
		}
	}

	entry := MCPShowEntry{
		Name:               server.Name,
		Scope:              server.Scope,
		Transport:          server.Transport,
		Command:            server.Command,
		Args:               cloneStringSlice(server.Args),
		Cwd:                server.Cwd,
		URL:                server.URL,
		Headers:            cloneStringMap(server.Headers),
		Environment:        cloneStringMap(server.Environment),
		Enabled:            server.Enabled,
		TimeoutSeconds:     server.TimeoutSeconds,
		InheritEnvironment: server.InheritEnvironment,
	}

	content, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode mcp show output: %w", err)
	}

	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}

	fmt.Println(string(content))
	return nil
}

func mcpResolveCommand(paths Paths, arguments []string) error {
	includeDisabled := false
	resolveSecrets := false
	transforms := []string{}
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--include-disabled":
			includeDisabled = true
		case "--resolve-secrets":
			resolveSecrets = true
		case "--transform":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--transform requires a value"}
			}
			index++
			value := strings.TrimSpace(arguments[index])
			if value == "" {
				return UsageError{Message: "--transform requires a value"}
			}
			transforms = append(transforms, value)
		default:
			if strings.HasPrefix(argument, "--transform=") {
				value := strings.TrimSpace(strings.TrimPrefix(argument, "--transform="))
				if value == "" {
					return UsageError{Message: "--transform requires a value"}
				}
				transforms = append(transforms, value)
				continue
			}
			return UsageError{Message: fmt.Sprintf("unknown mcp resolve option: %s", argument)}
		}
	}

	resolvedConfig, servers, err := resolvedMCPServersForCommand(paths)
	if err != nil {
		return err
	}

	output := MCPResolveOutput{Servers: map[string]MCPResolveServer{}}

	candidateServers := map[string]MCPServer{}
	for name, server := range servers {
		if !includeDisabled && !server.Enabled {
			continue
		}
		candidateServers[name] = cloneMCPServer(server)
	}

	if resolveSecrets {
		resolver := newProjectSecretResolver(paths, loadSecretCommandDefinitions(resolvedConfig))
		if err := resolveMCPServerSecrets(context.Background(), candidateServers, resolver); err != nil {
			return err
		}
	}

	for _, name := range sortedMCPServerNames(candidateServers) {
		server := candidateServers[name]
		value := MCPResolveServer{
			Transport:          server.Transport,
			Command:            server.Command,
			Args:               cloneStringSlice(server.Args),
			Cwd:                server.Cwd,
			URL:                server.URL,
			Headers:            cloneStringMap(server.Headers),
			Environment:        cloneStringMap(server.Environment),
			Enabled:            server.Enabled,
			TimeoutSeconds:     server.TimeoutSeconds,
			InheritEnvironment: server.InheritEnvironment,
		}
		if value.Environment == nil && server.Transport == mcpTransportStdio {
			value.Environment = map[string]string{}
		}
		output.Servers[name] = value
	}

	if len(transforms) > 0 {
		transformedServers, err := applyMCPPluginTransforms(paths, output.Servers, transforms)
		if err != nil {
			return err
		}
		output.Servers = transformedServers
	}

	content, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("encode mcp resolve output: %w", err)
	}

	fmt.Println(string(content))
	return nil
}

func applyMCPPluginTransforms(paths Paths, servers map[string]MCPResolveServer, transforms []string) (map[string]MCPResolveServer, error) {
	model := map[string]any{"servers": cloneMCPResolveServerMap(servers)}
	for _, transform := range transforms {
		parts := strings.SplitN(transform, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, UsageError{Message: fmt.Sprintf("invalid transform selector %q (expected plugin-id:operation)", transform)}
		}
		pluginID := strings.TrimSpace(parts[0])
		operation := strings.TrimSpace(parts[1])
		response, err := runPluginCapabilityOperation(paths, pluginID, pluginCapabilityMCPTransform, operation, map[string]any{"mcp_model": cloneMap(model)})
		if err != nil {
			return nil, err
		}
		output, ok := response["output"].(map[string]any)
		if !ok {
			return nil, pluginError{Code: pluginCodeOutputInvalid, Message: fmt.Sprintf("transform %q did not return output object", transform)}
		}
		transformedModel, ok := output["mcp_model"].(map[string]any)
		if !ok {
			return nil, pluginError{Code: pluginCodeOutputInvalid, Message: fmt.Sprintf("transform %q did not return output.mcp_model", transform)}
		}
		model = transformedModel
	}

	serversRaw, exists := model["servers"]
	if !exists {
		return nil, pluginError{Code: pluginCodeOutputInvalid, Message: "transformed MCP model is missing servers"}
	}
	encoded, err := json.Marshal(serversRaw)
	if err != nil {
		return nil, pluginError{Code: pluginCodeOutputInvalid, Message: "failed to encode transformed servers"}
	}
	decoded := map[string]MCPResolveServer{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return nil, pluginError{Code: pluginCodeOutputInvalid, Message: "transformed servers have invalid schema"}
	}
	return decoded, nil
}

func cloneMCPResolveServerMap(servers map[string]MCPResolveServer) map[string]any {
	result := map[string]any{}
	for name, server := range servers {
		result[name] = map[string]any{
			"transport":           server.Transport,
			"command":             server.Command,
			"args":                cloneStringSlice(server.Args),
			"cwd":                 server.Cwd,
			"url":                 server.URL,
			"headers":             cloneStringMap(server.Headers),
			"environment":         cloneStringMap(server.Environment),
			"enabled":             server.Enabled,
			"timeout_seconds":     server.TimeoutSeconds,
			"inherit_environment": server.InheritEnvironment,
		}
	}
	return result
}

func mcpCheckCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown mcp check option: %s", argument)}
		}
	}

	resolvedConfig, servers, err := resolvedMCPServersForCommand(paths)
	if err != nil {
		return err
	}

	resolver := newProjectSecretResolver(paths, loadSecretCommandDefinitions(resolvedConfig))
	results := make([]MCPCheckResult, 0)
	allValid := true

	for _, name := range sortedMCPServerNames(servers) {
		server := servers[name]
		if !server.Enabled {
			continue
		}

		result := checkMCPServer(context.Background(), server, resolver)
		if !result.Valid {
			allValid = false
		}
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Name < results[j].Name
	})

	if jsonOutput {
		content, err := json.MarshalIndent(MCPCheckOutput{Valid: allValid, Results: results}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode mcp check JSON: %w", err)
		}
		fmt.Println(string(content))
	} else {
		for _, result := range results {
			if result.Valid {
				fmt.Printf("[ok] mcp server=%s transport=%s\n", result.Name, result.Transport)
				continue
			}

			for _, issue := range result.Errors {
				fmt.Printf(
					"[error] mcp server=%s transport=%s path=%s code=%s message=%s\n",
					result.Name,
					result.Transport,
					issue.Path,
					issue.Code,
					issue.Message,
				)
			}
		}
		fmt.Printf("valid=%t\n", allValid)
	}

	if allValid {
		return nil
	}

	return errors.New("mcp check failed")
}

func resolvedMCPServersForCommand(paths Paths) (map[string]any, map[string]MCPServer, error) {
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return nil, nil, err
	}

	validation, err := validateConfigurationForProject(paths, info, false)
	if err != nil {
		return nil, nil, err
	}
	if len(validation.Errors) > 0 {
		return nil, nil, configurationValidationError(validation)
	}
	printConfigurationWarnings(validation.Warnings)

	resolvedConfig, _, err := resolveConfiguration(paths, info)
	if err != nil {
		return nil, nil, err
	}

	sources, _, _ := loadConfigurationSources(paths, info)
	scopes := mcpServerScopes(sources)

	servers, diagnostics := parseMCPServers(resolvedConfig, scopes)
	if len(diagnostics) > 0 {
		sortMCPDiagnostics(diagnostics)
		first := diagnostics[0]
		return nil, nil, mcpError{
			Code:    first.Code,
			Message: fmt.Sprintf("MCP server %q field %q: %s", first.Name, first.Path, first.Message),
		}
	}

	return resolvedConfig, servers, nil
}

func parseMCPServers(configuration map[string]any, scopes map[string]string) (map[string]MCPServer, []mcpDiagnostic) {
	servers := map[string]MCPServer{}
	diagnostics := []mcpDiagnostic{}

	mcpValue, exists := configuration["mcp"]
	if !exists {
		return servers, diagnostics
	}

	mcp, ok := mcpValue.(map[string]any)
	if !ok {
		diagnostics = append(diagnostics, mcpDiagnostic{
			Path:    "mcp",
			Code:    validationCodeInvalidType,
			Message: "mcp must be a table",
		})
		return servers, diagnostics
	}

	serversValue, exists := mcp["servers"]
	if !exists {
		return servers, diagnostics
	}

	baseEnvironment, envDiagnostics := normalizeMCPEnvironmentTable(configuration["environment"], "environment")
	diagnostics = append(diagnostics, envDiagnostics...)

	switch typed := serversValue.(type) {
	case []any:
		seen := map[string]bool{}
		for _, item := range typed {
			name, ok := item.(string)
			if !ok {
				diagnostics = append(diagnostics, mcpDiagnostic{
					Path:    "mcp.servers",
					Code:    validationCodeInvalidType,
					Message: "legacy mcp.servers array must contain only strings",
				})
				continue
			}

			if err := validateMCPServerName(name); err != nil {
				diagnostics = append(diagnostics, mcpDiagnostic{
					Name:    name,
					Path:    "mcp.servers",
					Code:    mcpCodeInvalidServerName,
					Message: "invalid MCP server name",
				})
				continue
			}

			if seen[name] {
				diagnostics = append(diagnostics, mcpDiagnostic{
					Name:    name,
					Path:    "mcp.servers",
					Code:    mcpCodeDuplicateServer,
					Message: "duplicate server in legacy mcp.servers array",
				})
				continue
			}
			seen[name] = true

			servers[name] = MCPServer{
				Name:        name,
				Scope:       scopeForMCPServer(name, scopes),
				Transport:   mcpTransportStdio,
				Command:     name,
				Args:        []string{},
				Environment: cloneStringMap(baseEnvironment),
				Enabled:     true,
			}
		}

	case map[string]any:
		for _, name := range mapKeys(typed) {
			raw := typed[name]
			server, serverDiagnostics := parseMCPServerDefinition(name, raw, baseEnvironment, scopes)
			diagnostics = append(diagnostics, serverDiagnostics...)
			if len(serverDiagnostics) == 0 {
				servers[name] = server
			}
		}

	default:
		diagnostics = append(diagnostics, mcpDiagnostic{
			Path:    "mcp.servers",
			Code:    validationCodeInvalidType,
			Message: "mcp.servers must be a table of server definitions or a legacy array of strings",
		})
	}

	return servers, diagnostics
}

func parseMCPServerDefinition(
	name string,
	raw any,
	baseEnvironment map[string]string,
	scopes map[string]string,
) (MCPServer, []mcpDiagnostic) {
	server := MCPServer{
		Name:               name,
		Scope:              scopeForMCPServer(name, scopes),
		Enabled:            true,
		Environment:        map[string]string{},
		Headers:            map[string]string{},
		Args:               []string{},
		InheritEnvironment: false,
	}
	findings := []mcpDiagnostic{}

	if err := validateMCPServerName(name); err != nil {
		return server, []mcpDiagnostic{{
			Name:    name,
			Path:    "mcp.servers." + name,
			Code:    mcpCodeInvalidServerName,
			Message: "invalid MCP server name",
		}}
	}

	definition, ok := raw.(map[string]any)
	if !ok {
		return server, []mcpDiagnostic{{
			Name:    name,
			Path:    "mcp.servers." + name,
			Code:    validationCodeInvalidType,
			Message: "MCP server definition must be a table",
		}}
	}

	allowed := map[string]bool{
		"transport":           true,
		"command":             true,
		"args":                true,
		"cwd":                 true,
		"environment":         true,
		"enabled":             true,
		"timeout_seconds":     true,
		"url":                 true,
		"headers":             true,
		"inherit_environment": true,
	}
	for _, key := range mapKeys(definition) {
		if !allowed[key] {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + "." + key,
				Code:    validationCodeUnknownField,
				Message: "unknown MCP server field",
			})
		}
	}

	transportValue, ok := definition["transport"].(string)
	if !ok || transportValue == "" {
		findings = append(findings, mcpDiagnostic{
			Name:    name,
			Path:    "mcp.servers." + name + ".transport",
			Code:    mcpCodeUnsupportedTransport,
			Message: "transport must be stdio or http",
		})
		return server, findings
	}
	server.Transport = transportValue

	enabledValue, exists := definition["enabled"]
	if exists {
		enabled, ok := enabledValue.(bool)
		if !ok {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".enabled",
				Code:    validationCodeInvalidType,
				Message: "enabled must be a boolean",
			})
		} else {
			server.Enabled = enabled
		}
	}

	timeoutValue, exists := definition["timeout_seconds"]
	if exists {
		timeout, ok := timeoutValue.(int64)
		if !ok || timeout <= 0 {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".timeout_seconds",
				Code:    mcpCodeInvalidTimeout,
				Message: "timeout_seconds must be a positive integer",
			})
		} else {
			server.TimeoutSeconds = timeout
		}
	}

	inheritEnvironmentValue, exists := definition["inherit_environment"]
	if exists {
		inheritEnvironment, ok := inheritEnvironmentValue.(bool)
		if !ok {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".inherit_environment",
				Code:    validationCodeInvalidType,
				Message: "inherit_environment must be a boolean",
			})
		} else {
			server.InheritEnvironment = inheritEnvironment
		}
	}

	switch server.Transport {
	case mcpTransportStdio:
		if _, exists := definition["url"]; exists {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".url",
				Code:    mcpCodeConflictingFields,
				Message: "url is not allowed for stdio transport",
			})
		}
		if _, exists := definition["headers"]; exists {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".headers",
				Code:    mcpCodeConflictingFields,
				Message: "headers is not allowed for stdio transport",
			})
		}

		commandValue, exists := definition["command"]
		if !exists {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".command",
				Code:    mcpCodeMissingCommand,
				Message: "stdio transport requires command",
			})
		} else if command, ok := commandValue.(string); !ok || strings.TrimSpace(command) == "" {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".command",
				Code:    mcpCodeInvalidCommand,
				Message: "command must be a non-empty string",
			})
		} else {
			server.Command = command
		}

		if argsValue, exists := definition["args"]; exists {
			args, ok := argsValue.([]any)
			if !ok {
				findings = append(findings, mcpDiagnostic{
					Name:    name,
					Path:    "mcp.servers." + name + ".args",
					Code:    mcpCodeInvalidArgs,
					Message: "args must be an array of strings",
				})
			} else {
				parsedArgs := make([]string, 0, len(args))
				valid := true
				for _, item := range args {
					argument, ok := item.(string)
					if !ok {
						valid = false
						break
					}
					parsedArgs = append(parsedArgs, argument)
				}
				if !valid {
					findings = append(findings, mcpDiagnostic{
						Name:    name,
						Path:    "mcp.servers." + name + ".args",
						Code:    mcpCodeInvalidArgs,
						Message: "args must be an array of strings",
					})
				} else {
					server.Args = parsedArgs
				}
			}
		}

		if cwdValue, exists := definition["cwd"]; exists {
			cwd, ok := cwdValue.(string)
			if !ok {
				findings = append(findings, mcpDiagnostic{
					Name:    name,
					Path:    "mcp.servers." + name + ".cwd",
					Code:    validationCodeInvalidType,
					Message: "cwd must be a string",
				})
			} else {
				server.Cwd = cwd
			}
		}

		serverEnvironment, serverEnvironmentFindings := normalizeMCPEnvironmentTable(
			definition["environment"],
			"mcp.servers."+name+".environment",
		)
		for _, finding := range serverEnvironmentFindings {
			finding.Name = name
			findings = append(findings, finding)
		}

		composed := map[string]string{}
		if server.InheritEnvironment {
			for key, value := range processEnvironmentMap() {
				composed[key] = value
			}
		}
		for key, value := range baseEnvironment {
			composed[key] = value
		}
		for key, value := range serverEnvironment {
			composed[key] = value
		}
		server.Environment = composed
		server.Headers = nil

	case mcpTransportHTTP:
		if _, exists := definition["command"]; exists {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".command",
				Code:    mcpCodeConflictingFields,
				Message: "command is not allowed for http transport",
			})
		}
		if _, exists := definition["args"]; exists {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".args",
				Code:    mcpCodeConflictingFields,
				Message: "args is not allowed for http transport",
			})
		}
		if _, exists := definition["cwd"]; exists {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".cwd",
				Code:    mcpCodeConflictingFields,
				Message: "cwd is not allowed for http transport",
			})
		}
		if _, exists := definition["environment"]; exists {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".environment",
				Code:    mcpCodeConflictingFields,
				Message: "environment is not allowed for http transport",
			})
		}
		if _, exists := definition["inherit_environment"]; exists {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".inherit_environment",
				Code:    mcpCodeConflictingFields,
				Message: "inherit_environment is not allowed for http transport",
			})
		}

		urlValue, exists := definition["url"]
		if !exists {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".url",
				Code:    mcpCodeMissingURL,
				Message: "http transport requires url",
			})
		} else if parsedURL, ok := urlValue.(string); !ok || strings.TrimSpace(parsedURL) == "" {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".url",
				Code:    mcpCodeInvalidURL,
				Message: "url must be a non-empty absolute HTTP or HTTPS URL",
			})
		} else if err := validateMCPHTTPURL(parsedURL); err != nil {
			findings = append(findings, mcpDiagnostic{
				Name:    name,
				Path:    "mcp.servers." + name + ".url",
				Code:    mcpCodeInvalidURL,
				Message: "url must be a valid absolute HTTP or HTTPS URL",
			})
		} else {
			server.URL = parsedURL
		}

		if headersValue, exists := definition["headers"]; exists {
			headersTable, ok := headersValue.(map[string]any)
			if !ok {
				findings = append(findings, mcpDiagnostic{
					Name:    name,
					Path:    "mcp.servers." + name + ".headers",
					Code:    mcpCodeInvalidHeaders,
					Message: "headers must be a table with string values",
				})
			} else {
				headers := map[string]string{}
				for _, key := range mapKeys(headersTable) {
					value, ok := headersTable[key].(string)
					if !ok {
						findings = append(findings, mcpDiagnostic{
							Name:    name,
							Path:    "mcp.servers." + name + ".headers." + key,
							Code:    mcpCodeInvalidHeaders,
							Message: "headers must be a table with string values",
						})
						continue
					}
					headers[key] = value
				}
				server.Headers = headers
			}
		}
		server.Environment = nil

	default:
		findings = append(findings, mcpDiagnostic{
			Name:    name,
			Path:    "mcp.servers." + name + ".transport",
			Code:    mcpCodeUnsupportedTransport,
			Message: "transport must be stdio or http",
		})
	}

	if len(findings) > 0 {
		return server, findings
	}

	return server, nil
}

func normalizeMCPEnvironmentTable(value any, path string) (map[string]string, []mcpDiagnostic) {
	result := map[string]string{}
	findings := []mcpDiagnostic{}
	if value == nil {
		return result, findings
	}

	table, ok := value.(map[string]any)
	if !ok {
		findings = append(findings, mcpDiagnostic{
			Path:    path,
			Code:    mcpCodeInvalidEnvironment,
			Message: "environment must be a table",
		})
		return result, findings
	}

	for _, key := range mapKeys(table) {
		if err := validateEnvironmentName(key); err != nil {
			findings = append(findings, mcpDiagnostic{
				Path:    path + "." + key,
				Code:    mcpCodeInvalidEnvironment,
				Message: "invalid environment variable name",
			})
			continue
		}

		stringValue, err := environmentStringValue(key, table[key])
		if err != nil {
			findings = append(findings, mcpDiagnostic{
				Path:    path + "." + key,
				Code:    mcpCodeInvalidEnvironment,
				Message: "environment variable must be string, boolean, integer, or float",
			})
			continue
		}

		if strings.HasPrefix(stringValue, "secret://") {
			if err := validateSecretReferenceForMCP(stringValue); err != nil {
				findings = append(findings, mcpDiagnostic{
					Path:    path + "." + key,
					Code:    mcpCodeInvalidEnvironment,
					Message: "environment secret reference is invalid",
				})
				continue
			}
		}

		result[key] = stringValue
	}

	return result, findings
}

func validateMCPServerName(name string) error {
	if name == "" {
		return errors.New("empty name")
	}

	for index, character := range name {
		if character > 127 {
			return errors.New("non-ASCII")
		}

		isLetter := character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isUnderscore := character == '_'
		isHyphen := character == '-'

		if index == 0 {
			if !isLetter && !isDigit {
				return errors.New("must begin with letter or digit")
			}
			continue
		}

		if !isLetter && !isDigit && !isUnderscore && !isHyphen {
			return errors.New("invalid character")
		}
	}

	return nil
}

func validateMCPHTTPURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if !parsed.IsAbs() {
		return errors.New("url must be absolute")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("unsupported scheme")
	}
	if parsed.Host == "" {
		return errors.New("missing host")
	}
	return nil
}

func validateSecretReferenceForMCP(raw string) error {
	reference, err := parseSecretReference(raw)
	if err != nil {
		return err
	}
	switch reference.Provider {
	case secretProviderEnv, secretProviderCommand:
		return nil
	default:
		return errors.New("unknown provider")
	}
}

func mcpServerScopes(sources []loadedConfigSource) map[string]string {
	inGlobal := map[string]bool{}
	inProject := map[string]bool{}

	for _, source := range sources {
		if source.ParseError != nil {
			continue
		}

		names := mcpServerNamesFromSource(source.Config)
		isProjectSource := source.SourceType == sourceTypeProject ||
			(source.SourceType == sourceTypeProfile && source.SelectedBy == sourceTypeProject)
		for _, name := range names {
			if isProjectSource {
				inProject[name] = true
			} else {
				inGlobal[name] = true
			}
		}
	}

	scopes := map[string]string{}
	for name := range inGlobal {
		scopes[name] = "global"
	}
	for name := range inProject {
		if inGlobal[name] {
			scopes[name] = "merged"
		} else {
			scopes[name] = "project"
		}
	}

	return scopes
}

func mcpServerNamesFromSource(configuration map[string]any) []string {
	mcpValue, ok := configuration["mcp"].(map[string]any)
	if !ok {
		return nil
	}

	serversValue, exists := mcpValue["servers"]
	if !exists {
		return nil
	}

	result := []string{}
	switch typed := serversValue.(type) {
	case map[string]any:
		result = append(result, mapKeys(typed)...)
	case []any:
		for _, item := range typed {
			if value, ok := item.(string); ok {
				result = append(result, value)
			}
		}
	}

	sort.Strings(result)
	return result
}

func scopeForMCPServer(name string, scopes map[string]string) string {
	scope, exists := scopes[name]
	if !exists {
		return "resolved"
	}
	return scope
}

func sortedMCPServerNames(servers map[string]MCPServer) []string {
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortMCPDiagnostics(findings []mcpDiagnostic) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]
		if left.Name != right.Name {
			return left.Name < right.Name
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Message < right.Message
	})
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := map[string]string{}
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneStringSlice(input []string) []string {
	if input == nil {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}

func cloneMCPServer(input MCPServer) MCPServer {
	return MCPServer{
		Name:               input.Name,
		Transport:          input.Transport,
		Command:            input.Command,
		Args:               cloneStringSlice(input.Args),
		Cwd:                input.Cwd,
		URL:                input.URL,
		Headers:            cloneStringMap(input.Headers),
		Environment:        cloneStringMap(input.Environment),
		Enabled:            input.Enabled,
		TimeoutSeconds:     input.TimeoutSeconds,
		InheritEnvironment: input.InheritEnvironment,
		Scope:              input.Scope,
	}
}

func processEnvironmentMap() map[string]string {
	result := map[string]string{}
	for _, value := range os.Environ() {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 {
			continue
		}
		result[parts[0]] = parts[1]
	}
	return result
}

func resolveMCPServerSecrets(
	ctx context.Context,
	servers map[string]MCPServer,
	resolver *secretResolver,
) error {
	for _, name := range sortedMCPServerNames(servers) {
		server := servers[name]
		switch server.Transport {
		case mcpTransportStdio:
			for _, key := range sortedStringMapKeys(server.Environment) {
				value := server.Environment[key]
				if !strings.HasPrefix(value, "secret://") {
					continue
				}
				reference, err := parseSecretReference(value)
				if err != nil {
					return mcpError{
						Code:    mcpCodeSecretResolutionFailed,
						Message: fmt.Sprintf("MCP server %q has invalid secret reference at environment.%s", name, key),
					}
				}

				resolved, err := resolver.Resolve(ctx, reference)
				if err != nil {
					return mcpError{
						Code:    mcpCodeSecretResolutionFailed,
						Message: fmt.Sprintf("MCP server %q failed to resolve secret at environment.%s", name, key),
					}
				}
				server.Environment[key] = resolved
			}
		case mcpTransportHTTP:
			for _, key := range sortedStringMapKeys(server.Headers) {
				value := server.Headers[key]
				if !strings.HasPrefix(value, "secret://") {
					continue
				}
				reference, err := parseSecretReference(value)
				if err != nil {
					return mcpError{
						Code:    mcpCodeSecretResolutionFailed,
						Message: fmt.Sprintf("MCP server %q has invalid secret reference at headers.%s", name, key),
					}
				}
				resolved, err := resolver.Resolve(ctx, reference)
				if err != nil {
					return mcpError{
						Code:    mcpCodeSecretResolutionFailed,
						Message: fmt.Sprintf("MCP server %q failed to resolve secret at headers.%s", name, key),
					}
				}
				server.Headers[key] = resolved
			}
		}
		servers[name] = server
	}
	return nil
}

func checkMCPServer(
	ctx context.Context,
	server MCPServer,
	resolver *secretResolver,
) MCPCheckResult {
	result := MCPCheckResult{
		Name:      server.Name,
		Transport: server.Transport,
		Checks:    []string{},
		Errors:    []MCPCheckIssue{},
	}

	switch server.Transport {
	case mcpTransportStdio:
		result.Checks = append(result.Checks, "command_field_valid")
		if strings.TrimSpace(server.Command) == "" {
			result.Errors = append(result.Errors, MCPCheckIssue{
				Path:    "mcp.servers." + server.Name + ".command",
				Code:    mcpCodeMissingCommand,
				Message: "stdio command is required",
			})
		} else {
			result.Checks = append(result.Checks, "command_available")
			if err := checkMCPCommandAvailability(server.Command); err != nil {
				result.Errors = append(result.Errors, MCPCheckIssue{
					Path:    "mcp.servers." + server.Name + ".command",
					Code:    mcpCodeCommandNotFound,
					Message: err.Error(),
				})
			}
		}

		if server.Cwd != "" {
			result.Checks = append(result.Checks, "working_directory_exists")
			if info, err := os.Stat(server.Cwd); err != nil || !info.IsDir() {
				result.Errors = append(result.Errors, MCPCheckIssue{
					Path:    "mcp.servers." + server.Name + ".cwd",
					Code:    mcpCodeWorkingDirectoryNotFound,
					Message: "working directory does not exist",
				})
			}
		}

		result.Checks = append(result.Checks, "secret_references_resolvable")
		for _, key := range sortedStringMapKeys(server.Environment) {
			value := server.Environment[key]
			if !strings.HasPrefix(value, "secret://") {
				continue
			}
			reference, err := parseSecretReference(value)
			if err != nil {
				result.Errors = append(result.Errors, MCPCheckIssue{
					Path:    "mcp.servers." + server.Name + ".environment." + key,
					Code:    mcpCodeSecretResolutionFailed,
					Message: "invalid secret reference",
				})
				continue
			}
			if _, err := resolver.Resolve(ctx, reference); err != nil {
				result.Errors = append(result.Errors, MCPCheckIssue{
					Path:    "mcp.servers." + server.Name + ".environment." + key,
					Code:    mcpCodeSecretResolutionFailed,
					Message: "secret reference could not be resolved",
				})
			}
		}

	case mcpTransportHTTP:
		result.Checks = append(result.Checks, "url_valid")
		if err := validateMCPHTTPURL(server.URL); err != nil {
			result.Errors = append(result.Errors, MCPCheckIssue{
				Path:    "mcp.servers." + server.Name + ".url",
				Code:    mcpCodeInvalidURL,
				Message: "url must be a valid absolute HTTP or HTTPS URL",
			})
		}

		result.Checks = append(result.Checks, "secret_references_resolvable")
		for _, key := range sortedStringMapKeys(server.Headers) {
			value := server.Headers[key]
			if !strings.HasPrefix(value, "secret://") {
				continue
			}
			reference, err := parseSecretReference(value)
			if err != nil {
				result.Errors = append(result.Errors, MCPCheckIssue{
					Path:    "mcp.servers." + server.Name + ".headers." + key,
					Code:    mcpCodeSecretResolutionFailed,
					Message: "invalid secret reference",
				})
				continue
			}
			if _, err := resolver.Resolve(ctx, reference); err != nil {
				result.Errors = append(result.Errors, MCPCheckIssue{
					Path:    "mcp.servers." + server.Name + ".headers." + key,
					Code:    mcpCodeSecretResolutionFailed,
					Message: "secret reference could not be resolved",
				})
			}
		}

	default:
		result.Checks = append(result.Checks, "transport_supported")
		result.Errors = append(result.Errors, MCPCheckIssue{
			Path:    "mcp.servers." + server.Name + ".transport",
			Code:    mcpCodeUnsupportedTransport,
			Message: "unsupported transport",
		})
	}

	sort.Slice(result.Errors, func(i, j int) bool {
		left := result.Errors[i]
		right := result.Errors[j]
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Message < right.Message
	})
	result.Valid = len(result.Errors) == 0
	return result
}

func checkMCPCommandAvailability(command string) error {
	if filepath.IsAbs(command) {
		info, err := os.Stat(command)
		if err != nil || info.IsDir() {
			return errors.New("absolute command path does not exist")
		}
		return nil
	}

	if _, err := exec.LookPath(command); err != nil {
		return errors.New("command not found in PATH")
	}
	return nil
}

func sortedStringMapKeys(values map[string]string) []string {
	if values == nil {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func collectMCPDoctorSummary(
	resolvedConfig map[string]any,
	sources []loadedConfigSource,
) MCPDoctorSummary {
	summary := MCPDoctorSummary{}
	scopes := mcpServerScopes(sources)
	servers, diagnostics := parseMCPServers(resolvedConfig, scopes)

	summary.ConfiguredServers = len(servers)
	for _, server := range servers {
		if server.Enabled {
			summary.EnabledServers++
		}
	}

	summary.InvalidDefinitions = len(diagnostics)
	for _, finding := range diagnostics {
		if finding.Code == mcpCodeUnsupportedTransport {
			summary.UnsupportedTransports++
		}
	}

	resolver := newSecretResolver(loadSecretCommandDefinitions(resolvedConfig))
	for _, name := range sortedMCPServerNames(servers) {
		server := servers[name]
		if !server.Enabled {
			continue
		}
		result := checkMCPServer(context.Background(), server, resolver)
		for _, issue := range result.Errors {
			switch issue.Code {
			case mcpCodeCommandNotFound:
				summary.UnavailableExecutables++
			case mcpCodeWorkingDirectoryNotFound:
				summary.InvalidWorkingDirectory++
			case mcpCodeSecretResolutionFailed:
				summary.UnresolvedSecrets++
			case mcpCodeUnsupportedTransport:
				summary.UnsupportedTransports++
			}
		}
	}

	return summary
}
