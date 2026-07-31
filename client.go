package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

const (
	clientNameCodex  = "codex"
	clientNameClaude = "claude"
	clientNameCursor = "cursor"
	clientNameVSCode = "vscode"
)

const (
	clientCapabilityMCP         = "mcp"
	clientCapabilityEnvironment = "environment"
	clientCapabilityPrompts     = "prompts"
	clientCapabilityRules       = "rules"
)

const (
	clientScopeUser    = "user"
	clientScopeProject = "project"
)

const (
	clientFormatJSON = "json"
	clientFormatTOML = "toml"
	clientFormatYAML = "yaml"
	clientFormatText = "text"
)

const (
	clientCodeUnknownClient               = "unknown_client"
	clientCodeClientDisabled              = "client_disabled"
	clientCodeUnsupportedClientFormat     = "unsupported_client_format"
	clientCodeUnsupportedClientScope      = "unsupported_client_scope"
	clientCodeUnsupportedClientTransport  = "unsupported_client_transport"
	clientCodeUnsupportedClientField      = "unsupported_client_field"
	clientCodeClientGenerationFailed      = "client_generation_failed"
	clientCodeClientValidationFailed      = "client_validation_failed"
	clientCodeClientPathAmbiguous         = "client_path_ambiguous"
	clientCodeClientPathUnavailable       = "client_path_unavailable"
	clientCodeClientOutputExists          = "client_output_exists"
	clientCodeClientOutputWriteFailed     = "client_output_write_failed"
	clientCodeClientSecretResolutionFail  = "client_secret_resolution_failed"
	clientCodeClientConfigurationMismatch = "client_configuration_incompatible"
)

const adapterSchemaVersion = "v1"

type clientError struct {
	Code    string
	Message string
}

func (err clientError) Error() string {
	if err.Code == "" {
		return err.Message
	}
	return fmt.Sprintf("code=%s %s", err.Code, err.Message)
}

type ClientDiagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Client   string `json:"client,omitempty"`
	Server   string `json:"server,omitempty"`
	Field    string `json:"field,omitempty"`
}

type ClientValidationResult struct {
	Client      string             `json:"client"`
	Valid       bool               `json:"valid"`
	Diagnostics []ClientDiagnostic `json:"diagnostics"`
}

type ClientDestination struct {
	Path      string `json:"path"`
	Scope     string `json:"scope"`
	Candidate bool   `json:"candidate"`
	Exists    bool   `json:"exists"`
}

type ClientPathResult struct {
	Client       string              `json:"client"`
	Destinations []ClientDestination `json:"destinations"`
	Ambiguous    bool                `json:"ambiguous"`
	Available    bool                `json:"available"`
}

type ClientShowResult struct {
	Name                          string            `json:"name"`
	DefaultFormat                 string            `json:"default_format"`
	SupportedFormats              []string          `json:"supported_formats"`
	SupportedScopes               []string          `json:"supported_scopes"`
	SupportedTransports           []string          `json:"supported_transports"`
	SupportedFields               []string          `json:"supported_fields"`
	KnownLimitations              []string          `json:"known_limitations"`
	Capabilities                  map[string]string `json:"capabilities"`
	DestinationDiscoverySupported bool              `json:"destination_discovery_supported"`
	CanKeepUnresolvedSecrets      bool              `json:"can_keep_unresolved_secrets"`
	DefaultDestinationCandidates  []ClientPathEntry `json:"default_destination_candidates"`
}

type ClientPathEntry struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

type ClientListEntry struct {
	Name                          string            `json:"name"`
	Available                     bool              `json:"available"`
	DefaultFormat                 string            `json:"default_format"`
	SupportedFeatures             map[string]string `json:"supported_features"`
	DestinationDiscoverySupported bool              `json:"destination_discovery_supported"`
}

type ClientGenerateOptions struct {
	Format          string
	IncludeDisabled bool
	ResolveSecrets  bool
	Scope           string
	WithMetadata    bool
	Strict          bool
}

type ClientValidateOptions struct {
	Format string
	Scope  string
	Strict bool
}

type ClientGenerateResult struct {
	Client      string             `json:"client"`
	Format      string             `json:"format"`
	Scope       string             `json:"scope"`
	Warnings    []ClientDiagnostic `json:"warnings,omitempty"`
	Diagnostics []ClientDiagnostic `json:"diagnostics,omitempty"`
	Payload     map[string]any     `json:"payload,omitempty"`
	Text        string             `json:"text,omitempty"`
}

type ClientSourceModel struct {
	Info         ProjectInfo
	Resolved     map[string]any
	Sources      []string
	Servers      map[string]MCPServer
	LoadedSource []loadedConfigSource
}

type clientAdapter interface {
	Name() string
	DefaultFormat() string
	SupportedFormats() []string
	SupportedScopes() []string
	SupportedTransports() []string
	SupportedFields() []string
	KnownLimitations() []string
	Capabilities() map[string]string
	CanKeepUnresolvedSecrets() bool
	Destinations(Paths, ProjectInfo, string) (ClientPathResult, error)
	Generate(ClientSourceModel, ClientGenerateOptions) (ClientGenerateResult, error)
	Validate(ClientSourceModel, ClientValidateOptions) ClientValidationResult
}

type clientAdapterSpec struct {
	name                      string
	defaultFormat             string
	formats                   []string
	scopes                    []string
	transports                []string
	fields                    []string
	limitations               []string
	capabilities              map[string]string
	canKeepUnresolvedSecrets  bool
	supportsDisabledSemantics bool
	supportsCwd               bool
	supportsTimeout           bool
	supportsServerEnvironment bool
	supportsHTTPHeaders       bool
}

type staticAdapter struct {
	spec clientAdapterSpec
}

func (adapter staticAdapter) Name() string {
	return adapter.spec.name
}

func (adapter staticAdapter) DefaultFormat() string {
	return adapter.spec.defaultFormat
}

func (adapter staticAdapter) SupportedFormats() []string {
	return cloneStringSlice(adapter.spec.formats)
}

func (adapter staticAdapter) SupportedScopes() []string {
	return cloneStringSlice(adapter.spec.scopes)
}

func (adapter staticAdapter) SupportedTransports() []string {
	return cloneStringSlice(adapter.spec.transports)
}

func (adapter staticAdapter) SupportedFields() []string {
	return cloneStringSlice(adapter.spec.fields)
}

func (adapter staticAdapter) KnownLimitations() []string {
	return cloneStringSlice(adapter.spec.limitations)
}

func (adapter staticAdapter) Capabilities() map[string]string {
	return cloneStringMap(adapter.spec.capabilities)
}

func (adapter staticAdapter) CanKeepUnresolvedSecrets() bool {
	return adapter.spec.canKeepUnresolvedSecrets
}

func (adapter staticAdapter) Destinations(paths Paths, info ProjectInfo, scope string) (ClientPathResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return ClientPathResult{}, err
	}

	if scope != "" && !containsString(adapter.spec.scopes, scope) {
		return ClientPathResult{}, clientError{
			Code:    clientCodeUnsupportedClientScope,
			Message: fmt.Sprintf("client %q does not support scope %q", adapter.spec.name, scope),
		}
	}

	destinations := []ClientDestination{}
	addDestination := func(path string, destinationScope string) {
		if scope != "" && destinationScope != scope {
			return
		}
		destinations = append(destinations, ClientDestination{
			Path:      path,
			Scope:     destinationScope,
			Candidate: true,
			Exists:    fileExists(path),
		})
	}

	switch adapter.spec.name {
	case clientNameCodex:
		addDestination(filepath.Join(home, ".codex"), clientScopeUser)
	case clientNameClaude:
		addDestination(filepath.Join(home, ".claude"), clientScopeUser)
	case clientNameCursor:
		addDestination(filepath.Join(home, ".cursor"), clientScopeUser)
		addDestination(filepath.Join(info.ProjectRoot, ".cursor", "mcp.json"), clientScopeProject)
	case clientNameVSCode:
		addDestination(filepath.Join(home, ".vscode"), clientScopeUser)
		addDestination(filepath.Join(home, ".config", "Code", "User", "settings.json"), clientScopeUser)
		addDestination(filepath.Join(home, ".config", "Code - Insiders", "User", "settings.json"), clientScopeUser)
		addDestination(filepath.Join(info.ProjectRoot, ".vscode", "settings.json"), clientScopeProject)
	default:
		addDestination(filepath.Join(home, "."+adapter.spec.name), clientScopeUser)
	}

	sort.Slice(destinations, func(i, j int) bool {
		if destinations[i].Scope != destinations[j].Scope {
			return destinations[i].Scope < destinations[j].Scope
		}
		return destinations[i].Path < destinations[j].Path
	})

	result := ClientPathResult{
		Client:       adapter.spec.name,
		Destinations: destinations,
		Ambiguous:    countDestinationsForScope(destinations, scopeOrDefault(scope, adapter.spec.scopes[0])) > 1,
		Available:    len(destinations) > 0,
	}
	if scope == "" {
		result.Ambiguous = hasAmbiguousDestinations(destinations)
	}
	if len(destinations) == 0 {
		result.Available = false
	}

	return result, nil
}

func (adapter staticAdapter) Validate(model ClientSourceModel, options ClientValidateOptions) ClientValidationResult {
	diagnostics := adapter.compatibilityDiagnostics(model, options.Scope, false)
	if options.Strict {
		diagnostics = convertClientWarningsToErrors(diagnostics)
	}
	sortClientDiagnostics(diagnostics)
	return ClientValidationResult{
		Client:      adapter.spec.name,
		Valid:       countClientErrors(diagnostics) == 0,
		Diagnostics: diagnostics,
	}
}

func (adapter staticAdapter) Generate(model ClientSourceModel, options ClientGenerateOptions) (ClientGenerateResult, error) {
	if !containsString(adapter.spec.formats, options.Format) {
		return ClientGenerateResult{}, clientError{
			Code:    clientCodeUnsupportedClientFormat,
			Message: fmt.Sprintf("client %q does not support format %q", adapter.spec.name, options.Format),
		}
	}
	if !containsString(adapter.spec.scopes, options.Scope) {
		return ClientGenerateResult{}, clientError{
			Code:    clientCodeUnsupportedClientScope,
			Message: fmt.Sprintf("client %q does not support scope %q", adapter.spec.name, options.Scope),
		}
	}

	diagnostics := adapter.compatibilityDiagnostics(model, options.Scope, options.IncludeDisabled)
	if options.Strict {
		diagnostics = convertClientWarningsToErrors(diagnostics)
	}
	sortClientDiagnostics(diagnostics)
	if countClientErrors(diagnostics) > 0 {
		return ClientGenerateResult{}, clientError{
			Code:    clientCodeClientGenerationFailed,
			Message: formatClientDiagnosticSummary(adapter.spec.name, diagnostics),
		}
	}

	servers := filterClientServers(model.Servers, options.IncludeDisabled)
	payload := adapter.translateServers(model, servers, options)

	result := ClientGenerateResult{
		Client:      adapter.spec.name,
		Format:      options.Format,
		Scope:       options.Scope,
		Warnings:    filterClientDiagnosticsBySeverity(diagnostics, "warning"),
		Diagnostics: diagnostics,
		Payload:     payload,
	}

	if options.WithMetadata {
		payload["metadata"] = map[string]any{
			"adapter_name":               adapter.spec.name,
			"adapter_version":            version,
			"schema_version":             adapterSchemaVersion,
			"project_id":                 model.Info.ProjectID,
			"generation_timestamp":       time.Now().UTC().Format(time.RFC3339),
			"source_configuration_paths": cloneStringSlice(model.Sources),
			"secrets_resolved":           options.ResolveSecrets,
		}
	}

	text, err := serializeClientOutput(options.Format, payload)
	if err != nil {
		return ClientGenerateResult{}, clientError{
			Code:    clientCodeClientGenerationFailed,
			Message: fmt.Sprintf("serialize %s client output: %v", adapter.spec.name, err),
		}
	}
	result.Text = text
	return result, nil
}

func (adapter staticAdapter) compatibilityDiagnostics(
	model ClientSourceModel,
	scope string,
	includeDisabled bool,
) []ClientDiagnostic {
	diagnostics := []ClientDiagnostic{}

	if !containsString(adapter.spec.scopes, scope) {
		diagnostics = append(diagnostics, ClientDiagnostic{
			Severity: "error",
			Code:     clientCodeUnsupportedClientScope,
			Message:  fmt.Sprintf("client %q does not support scope %q", adapter.spec.name, scope),
			Client:   adapter.spec.name,
		})
	}

	servers := filterClientServers(model.Servers, includeDisabled)

	for _, name := range sortedMCPServerNames(servers) {
		server := servers[name]
		if !containsString(adapter.spec.transports, server.Transport) {
			diagnostics = append(diagnostics, ClientDiagnostic{
				Severity: "error",
				Code:     clientCodeUnsupportedClientTransport,
				Message:  fmt.Sprintf("transport %q is not supported", server.Transport),
				Client:   adapter.spec.name,
				Server:   name,
				Field:    "transport",
			})
			continue
		}

		if !server.Enabled && !adapter.spec.supportsDisabledSemantics && includeDisabled {
			diagnostics = append(diagnostics, ClientDiagnostic{
				Severity: "warning",
				Code:     clientCodeUnsupportedClientField,
				Message:  "disabled server semantics are not representable and will be omitted",
				Client:   adapter.spec.name,
				Server:   name,
				Field:    "enabled",
			})
		}

		if server.Transport == mcpTransportStdio {
			if server.Cwd != "" && !adapter.spec.supportsCwd {
				diagnostics = append(diagnostics, ClientDiagnostic{
					Severity: "warning",
					Code:     clientCodeUnsupportedClientField,
					Message:  "cwd is not representable",
					Client:   adapter.spec.name,
					Server:   name,
					Field:    "cwd",
				})
			}
			if len(server.Environment) > 0 && !adapter.spec.supportsServerEnvironment {
				diagnostics = append(diagnostics, ClientDiagnostic{
					Severity: "error",
					Code:     clientCodeUnsupportedClientField,
					Message:  "server environment values are required but unsupported",
					Client:   adapter.spec.name,
					Server:   name,
					Field:    "environment",
				})
			}
		}

		if server.Transport == mcpTransportHTTP {
			if len(server.Headers) > 0 && !adapter.spec.supportsHTTPHeaders {
				diagnostics = append(diagnostics, ClientDiagnostic{
					Severity: "warning",
					Code:     clientCodeUnsupportedClientField,
					Message:  "http headers are not representable",
					Client:   adapter.spec.name,
					Server:   name,
					Field:    "headers",
				})
			}
		}

		if server.TimeoutSeconds > 0 && !adapter.spec.supportsTimeout {
			diagnostics = append(diagnostics, ClientDiagnostic{
				Severity: "warning",
				Code:     clientCodeUnsupportedClientField,
				Message:  "timeout_seconds is not representable",
				Client:   adapter.spec.name,
				Server:   name,
				Field:    "timeout_seconds",
			})
		}

		if !adapter.spec.canKeepUnresolvedSecrets {
			for _, key := range sortedStringMapKeys(server.Environment) {
				if strings.HasPrefix(server.Environment[key], "secret://") {
					diagnostics = append(diagnostics, ClientDiagnostic{
						Severity: "error",
						Code:     clientCodeClientConfigurationMismatch,
						Message:  "client cannot consume unresolved secret references",
						Client:   adapter.spec.name,
						Server:   name,
						Field:    "environment." + key,
					})
				}
			}
			for _, key := range sortedStringMapKeys(server.Headers) {
				if strings.HasPrefix(server.Headers[key], "secret://") {
					diagnostics = append(diagnostics, ClientDiagnostic{
						Severity: "error",
						Code:     clientCodeClientConfigurationMismatch,
						Message:  "client cannot consume unresolved secret references",
						Client:   adapter.spec.name,
						Server:   name,
						Field:    "headers." + key,
					})
				}
			}
		}
	}

	return diagnostics
}

func (adapter staticAdapter) translateServers(
	model ClientSourceModel,
	servers map[string]MCPServer,
	options ClientGenerateOptions,
) map[string]any {
	names := sortedMCPServerNames(servers)
	payloadServers := map[string]any{}

	for _, name := range names {
		server := servers[name]
		if !server.Enabled && !adapter.spec.supportsDisabledSemantics {
			continue
		}
		entry := map[string]any{
			"transport": server.Transport,
			"enabled":   server.Enabled,
		}
		if server.Command != "" {
			entry["command"] = server.Command
		}
		if len(server.Args) > 0 {
			entry["args"] = cloneStringSlice(server.Args)
		}
		if server.Cwd != "" && adapter.spec.supportsCwd {
			entry["cwd"] = server.Cwd
		}
		if server.URL != "" {
			entry["url"] = server.URL
		}
		if adapter.spec.supportsServerEnvironment && len(server.Environment) > 0 {
			entry["environment"] = cloneStringMap(server.Environment)
		}
		if adapter.spec.supportsHTTPHeaders && len(server.Headers) > 0 {
			entry["headers"] = cloneStringMap(server.Headers)
		}
		if adapter.spec.supportsTimeout && server.TimeoutSeconds > 0 {
			entry["timeout_seconds"] = server.TimeoutSeconds
		}

		payloadServers[name] = entry
	}

	switch adapter.spec.name {
	case clientNameCodex:
		return map[string]any{"codex": map[string]any{"mcp_servers": payloadServers, "scope": options.Scope}}
	case clientNameClaude:
		return map[string]any{"claude": map[string]any{"mcp": payloadServers, "scope": options.Scope}}
	case clientNameCursor:
		return map[string]any{"cursor": map[string]any{"mcpServers": payloadServers, "scope": options.Scope}}
	case clientNameVSCode:
		return map[string]any{"vscode": map[string]any{"mcp": payloadServers, "scope": options.Scope}}
	default:
		return map[string]any{"mcp": payloadServers}
	}
}

func clientCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "client requires a subcommand"}
	}

	switch arguments[0] {
	case "list":
		return clientListCommand(paths, arguments[1:])
	case "show":
		return clientShowCommand(paths, arguments[1:])
	case "path":
		return clientPathCommand(paths, arguments[1:])
	case "validate":
		return clientValidateCommand(paths, arguments[1:])
	case "generate":
		return clientGenerateCommand(paths, arguments[1:])
	case "compare":
		return clientCompareCommand(paths, arguments[1:])
	case "snapshot":
		return clientSnapshotCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown client subcommand: %s", arguments[0])}
	}
}

func clientListCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown client list option: %s", argument)}
		}
	}

	entries := []ClientListEntry{}
	for _, adapter := range sortedAdapters() {
		entries = append(entries, ClientListEntry{
			Name:                          adapter.Name(),
			Available:                     true,
			DefaultFormat:                 adapter.DefaultFormat(),
			SupportedFeatures:             adapter.Capabilities(),
			DestinationDiscoverySupported: true,
		})
	}

	if jsonOutput {
		content, err := json.MarshalIndent(map[string]any{"clients": entries}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode client list JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	for _, entry := range entries {
		fmt.Printf(
			"name=%s available=%t default_format=%s destination_discovery=%t capabilities=%s\n",
			entry.Name,
			entry.Available,
			entry.DefaultFormat,
			entry.DestinationDiscoverySupported,
			formatCapabilitySummary(entry.SupportedFeatures),
		)
	}
	return nil
}

func clientShowCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "client show requires a client name"}
	}
	name := arguments[0]
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown client show option: %s", argument)}
		}
	}

	adapter, err := adapterByName(name)
	if err != nil {
		return err
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	pathResult, err := adapter.Destinations(paths, info, "")
	if err != nil {
		return err
	}
	pathEntries := make([]ClientPathEntry, 0, len(pathResult.Destinations))
	for _, item := range pathResult.Destinations {
		pathEntries = append(pathEntries, ClientPathEntry{Scope: item.Scope, Path: item.Path})
	}

	show := ClientShowResult{
		Name:                          adapter.Name(),
		DefaultFormat:                 adapter.DefaultFormat(),
		SupportedFormats:              adapter.SupportedFormats(),
		SupportedScopes:               adapter.SupportedScopes(),
		SupportedTransports:           adapter.SupportedTransports(),
		SupportedFields:               adapter.SupportedFields(),
		KnownLimitations:              adapter.KnownLimitations(),
		Capabilities:                  adapter.Capabilities(),
		DestinationDiscoverySupported: true,
		CanKeepUnresolvedSecrets:      adapter.CanKeepUnresolvedSecrets(),
		DefaultDestinationCandidates:  pathEntries,
	}

	if jsonOutput {
		content, err := json.MarshalIndent(show, "", "  ")
		if err != nil {
			return fmt.Errorf("encode client show JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	content, err := json.MarshalIndent(show, "", "  ")
	if err != nil {
		return fmt.Errorf("encode client show output: %w", err)
	}
	fmt.Println(string(content))
	return nil
}

func clientPathCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "client path requires a client name"}
	}
	name := arguments[0]
	jsonOutput := false
	scope := ""
	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--json":
			jsonOutput = true
		case "--scope":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--scope requires a value"}
			}
			index++
			scope = arguments[index]
		default:
			return UsageError{Message: fmt.Sprintf("unknown client path option: %s", argument)}
		}
	}

	adapter, err := adapterByName(name)
	if err != nil {
		return err
	}
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	pathResult, err := adapter.Destinations(paths, info, scope)
	if err != nil {
		return err
	}

	if jsonOutput {
		content, err := json.MarshalIndent(pathResult, "", "  ")
		if err != nil {
			return fmt.Errorf("encode client path JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	if len(pathResult.Destinations) == 0 {
		return clientError{Code: clientCodeClientPathUnavailable, Message: fmt.Sprintf("no destination path is available for client %q", name)}
	}

	for _, destination := range pathResult.Destinations {
		fmt.Printf("client=%s scope=%s candidate_path=%s exists=%t\n", name, destination.Scope, destination.Path, destination.Exists)
	}
	if pathResult.Ambiguous {
		return clientError{Code: clientCodeClientPathAmbiguous, Message: fmt.Sprintf("destination path for client %q is ambiguous", name)}
	}
	return nil
}

func clientValidateCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "client validate requires a client name"}
	}
	name := arguments[0]
	jsonOutput := false
	options := ClientValidateOptions{Scope: clientScopeProject, Format: "", Strict: false}

	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--json":
			jsonOutput = true
		case "--scope":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--scope requires a value"}
			}
			index++
			options.Scope = arguments[index]
		case "--format":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--format requires a value"}
			}
			index++
			options.Format = arguments[index]
		case "--strict":
			options.Strict = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown client validate option: %s", argument)}
		}
	}

	adapter, err := adapterByName(name)
	if err != nil {
		return err
	}
	if options.Format != "" && !containsString(adapter.SupportedFormats(), options.Format) {
		return clientError{
			Code:    clientCodeUnsupportedClientFormat,
			Message: fmt.Sprintf("client %q does not support format %q", name, options.Format),
		}
	}

	model, err := resolveClientSourceModel(paths)
	if err != nil {
		return err
	}
	if err := ensureClientEnabled(model.Resolved, name); err != nil {
		return err
	}

	result := adapter.Validate(model, options)
	if jsonOutput {
		content, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("encode client validate JSON: %w", err)
		}
		fmt.Println(string(content))
	} else {
		for _, finding := range result.Diagnostics {
			fmt.Printf("[%s] client=%s server=%s field=%s code=%s message=%s\n", finding.Severity, name, finding.Server, finding.Field, finding.Code, finding.Message)
		}
		fmt.Printf("valid=%t\n", result.Valid)
	}

	if result.Valid {
		return nil
	}
	return clientError{Code: clientCodeClientValidationFailed, Message: fmt.Sprintf("client %q configuration is not compatible", name)}
}

func clientGenerateCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "client generate requires a client name"}
	}
	name := arguments[0]
	options := ClientGenerateOptions{
		Format:          "",
		IncludeDisabled: false,
		ResolveSecrets:  false,
		Scope:           clientScopeProject,
		WithMetadata:    false,
		Strict:          false,
	}
	outputPath := ""
	forceWrite := false
	jsonMode := false

	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--json":
			jsonMode = true
		case "--include-disabled":
			options.IncludeDisabled = true
		case "--resolve-secrets":
			options.ResolveSecrets = true
		case "--scope":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--scope requires a value"}
			}
			index++
			options.Scope = arguments[index]
		case "--format":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--format requires a value"}
			}
			index++
			options.Format = arguments[index]
		case "--with-metadata":
			options.WithMetadata = true
		case "--strict":
			options.Strict = true
		case "--output":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--output requires a value"}
			}
			index++
			outputPath = arguments[index]
		case "--force":
			forceWrite = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown client generate option: %s", argument)}
		}
	}

	adapter, err := adapterByName(name)
	if err != nil {
		return err
	}

	if jsonMode {
		if !containsString(adapter.SupportedFormats(), clientFormatJSON) {
			return clientError{
				Code:    clientCodeUnsupportedClientFormat,
				Message: fmt.Sprintf("client %q does not support JSON output", name),
			}
		}
		options.Format = clientFormatJSON
	}
	if options.Format == "" {
		options.Format = adapter.DefaultFormat()
	}
	if !containsString(adapter.SupportedFormats(), options.Format) {
		return clientError{
			Code:    clientCodeUnsupportedClientFormat,
			Message: fmt.Sprintf("client %q does not support format %q", name, options.Format),
		}
	}

	model, err := resolveClientSourceModel(paths)
	if err != nil {
		return err
	}
	if err := ensureClientEnabled(model.Resolved, name); err != nil {
		return err
	}

	workingModel := cloneClientSourceModel(model)
	if options.ResolveSecrets {
		resolver := newProjectSecretResolver(paths, loadSecretCommandDefinitions(workingModel.Resolved))
		if err := resolveMCPServerSecrets(context.Background(), workingModel.Servers, resolver); err != nil {
			return clientError{Code: clientCodeClientSecretResolutionFail, Message: "client secret resolution failed"}
		}
	} else if !adapter.CanKeepUnresolvedSecrets() {
		if hasUnresolvedSecretReferences(workingModel.Servers) {
			return clientError{
				Code:    clientCodeClientConfigurationMismatch,
				Message: fmt.Sprintf("client %q cannot consume unresolved secret references; retry with --resolve-secrets", name),
			}
		}
	}

	result, err := adapter.Generate(workingModel, options)
	if err != nil {
		return err
	}

	if outputPath == "" {
		fmt.Println(result.Text)
		return nil
	}

	if err := writeClientOutputFile(workingModel.Info.ProjectRoot, outputPath, result.Text, options.ResolveSecrets, forceWrite); err != nil {
		return err
	}
	return nil
}

func clientSnapshotCommand(paths Paths, arguments []string) error {
	outputPath := ""
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--output":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--output requires a value"}
			}
			index++
			outputPath = arguments[index]
		default:
			return UsageError{Message: fmt.Sprintf("unknown client snapshot option: %s", argument)}
		}
	}

	snapshot := buildClientStructureSnapshot()
	if outputPath != "" {
		if err := os.WriteFile(outputPath, []byte(snapshot), 0o600); err != nil {
			return fmt.Errorf("write client snapshot: %w", err)
		}
		return nil
	}

	fmt.Print(snapshot)
	return nil
}

func buildClientStructureSnapshot() string {
	var builder strings.Builder
	builder.WriteString("# AI client structure snapshot\n\n")
	builder.WriteString("This file is the source of truth for the current client-facing structure.\n")
	builder.WriteString("It keeps the nearest possible hierarchy between AI clients and their target files.\n\n")
	builder.WriteString("## Version\n\n")
	builder.WriteString("1\n\n")
	builder.WriteString("## Sync rules\n\n")
	builder.WriteString("- Preserve hierarchy before transforming content.\n")
	builder.WriteString("- Prefer single-file to single-file mappings when both clients support them.\n")
	builder.WriteString("- Keep target files in the target client’s native folder.\n")
	builder.WriteString("- Update this snapshot first when structure changes, then sync.\n\n")
	builder.WriteString("## File kind meanings\n\n")
	builder.WriteString("- `instructions`: user-facing operating guidance for instruction-oriented clients.\n")
	builder.WriteString("- `rules`: policy and constraint files for rule-oriented clients.\n")
	builder.WriteString("- `prompts`: reusable task templates and prompt assets.\n")
	builder.WriteString("- `agents`: role definitions and delegated behaviors.\n")
	builder.WriteString("- `mcp`: MCP server and transport definitions.\n")
	builder.WriteString("- `context`: resolved project context exported for the client.\n\n")
	builder.WriteString("## Clients\n\n")
	clients := []struct {
		name      string
		folder    string
		hierarchy string
		files     []struct{ path, meaning string }
	}{
		{name: "copilot", folder: ".github", hierarchy: "single-file guidance", files: []struct{ path, meaning string }{{".github/copilot-instructions.md", "repository guidance for GitHub Copilot"}}},
		{name: "claude", folder: ".claude", hierarchy: "single-file rules", files: []struct{ path, meaning string }{{".claude/rules.md", "repository rules for Claude"}}},
		{name: "codex", folder: ".codex", hierarchy: "single-file context plus snapshot", files: []struct{ path, meaning string }{{".codex/ai-dev-context.md", "resolved ai-dev project context used by Codex"}, {".codex/config/ai-client-structure.snapshot.md", "canonical snapshot for cross-client translation and sync"}}},
	}
	for _, client := range clients {
		builder.WriteString("### ")
		builder.WriteString(client.name)
		builder.WriteString("\n\n")
		builder.WriteString("Folder: `")
		builder.WriteString(client.folder)
		builder.WriteString("`\n\n")
		builder.WriteString("Hierarchy: ")
		builder.WriteString(client.hierarchy)
		builder.WriteString("\n\n")
		builder.WriteString("Files:\n\n")
		for _, file := range client.files {
			builder.WriteString("- `")
			builder.WriteString(file.path)
			builder.WriteString("` — ")
			builder.WriteString(file.meaning)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString("## Cross-client translation scenarios\n\n")
	builder.WriteString("### Single file to single file\n\n")
	builder.WriteString("Use this when the source client has one top-level guidance file and the target client expects one top-level guidance file.\n\n")
	builder.WriteString("### Folder tree to single file\n\n")
	builder.WriteString("Flatten only what is necessary and keep the target file as close as possible to the source hierarchy.\n\n")
	builder.WriteString("### Folder tree to folder tree\n\n")
	builder.WriteString("Preserve the relative hierarchy of the source tree inside the target client’s folder.\n")
	return builder.String()
}

func clientCompareCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown client compare option: %s", argument)}
		}
	}

	model, err := resolveClientSourceModel(paths)
	if err != nil {
		return err
	}

	type clientComparison struct {
		Client              string             `json:"client"`
		FeatureSupport      map[string]string  `json:"feature_support"`
		GenerationBlockers  []ClientDiagnostic `json:"generation_blockers"`
		PartiallySupported  []string           `json:"partially_supported"`
		UnsupportedFeatures []string           `json:"unsupported_features"`
	}

	comparisons := []clientComparison{}
	for _, adapter := range sortedAdapters() {
		if err := ensureClientEnabled(model.Resolved, adapter.Name()); err != nil {
			comparisons = append(comparisons, clientComparison{
				Client:             adapter.Name(),
				FeatureSupport:     adapter.Capabilities(),
				GenerationBlockers: []ClientDiagnostic{{Severity: "error", Code: clientCodeClientDisabled, Message: err.Error(), Client: adapter.Name()}},
			})
			continue
		}

		validation := adapter.Validate(model, ClientValidateOptions{Scope: clientScopeProject, Strict: false})
		partial := []string{}
		unsupported := []string{}
		for capability, support := range adapter.Capabilities() {
			switch support {
			case "unsupported":
				unsupported = append(unsupported, capability)
			case "partial":
				partial = append(partial, capability)
			}
		}
		sort.Strings(partial)
		sort.Strings(unsupported)

		comparisons = append(comparisons, clientComparison{
			Client:              adapter.Name(),
			FeatureSupport:      adapter.Capabilities(),
			GenerationBlockers:  filterClientDiagnosticsBySeverity(validation.Diagnostics, "error"),
			PartiallySupported:  partial,
			UnsupportedFeatures: unsupported,
		})
	}

	sort.Slice(comparisons, func(i, j int) bool {
		return comparisons[i].Client < comparisons[j].Client
	})

	if jsonOutput {
		content, err := json.MarshalIndent(map[string]any{"clients": comparisons}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode client compare JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	for _, comparison := range comparisons {
		fmt.Printf("client=%s supported=%s partially_supported=%s unsupported=%s blockers=%d\n", comparison.Client, formatCapabilitySummary(comparison.FeatureSupport), strings.Join(comparison.PartiallySupported, ","), strings.Join(comparison.UnsupportedFeatures, ","), len(comparison.GenerationBlockers))
	}
	return nil
}

func resolveClientSourceModel(paths Paths) (ClientSourceModel, error) {
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return ClientSourceModel{}, err
	}

	validation, err := validateConfigurationForProject(paths, info, false)
	if err != nil {
		return ClientSourceModel{}, err
	}
	if len(validation.Errors) > 0 {
		return ClientSourceModel{}, configurationValidationError(validation)
	}
	printConfigurationWarnings(validation.Warnings)

	resolved, sources, err := resolveConfiguration(paths, info)
	if err != nil {
		return ClientSourceModel{}, err
	}

	loadedSources, _, _ := loadConfigurationSources(paths, info)
	scopes := mcpServerScopes(loadedSources)
	servers, diagnostics := parseMCPServers(resolved, scopes)
	if len(diagnostics) > 0 {
		sortMCPDiagnostics(diagnostics)
		first := diagnostics[0]
		return ClientSourceModel{}, mcpError{Code: first.Code, Message: fmt.Sprintf("MCP server %q field %q: %s", first.Name, first.Path, first.Message)}
	}

	return ClientSourceModel{
		Info:         info,
		Resolved:     resolved,
		Sources:      sources,
		Servers:      servers,
		LoadedSource: loadedSources,
	}, nil
}

func ensureClientEnabled(configuration map[string]any, clientName string) error {
	enabled, err := clientEnabled(configuration, clientName)
	if err != nil {
		return err
	}
	if !enabled {
		return clientError{Code: clientCodeClientDisabled, Message: fmt.Sprintf("client %q is disabled in configuration", clientName)}
	}
	return nil
}

func clientEnabled(configuration map[string]any, clientName string) (bool, error) {
	clientsValue, exists := configuration["clients"]
	if !exists {
		return true, nil
	}
	clients, ok := clientsValue.(map[string]any)
	if !ok {
		return true, nil
	}
	clientValue, exists := clients[clientName]
	if !exists {
		return true, nil
	}
	clientTable, ok := clientValue.(map[string]any)
	if !ok {
		return false, clientError{Code: clientCodeClientValidationFailed, Message: fmt.Sprintf("clients.%s must be a table", clientName)}
	}
	enabledValue, exists := clientTable["enabled"]
	if !exists {
		return true, nil
	}
	enabled, ok := enabledValue.(bool)
	if !ok {
		return false, clientError{Code: clientCodeClientValidationFailed, Message: fmt.Sprintf("clients.%s.enabled must be a boolean", clientName)}
	}
	return enabled, nil
}

func adapterByName(name string) (clientAdapter, error) {
	adapter := registeredClientAdapters()[name]
	if adapter == nil {
		return nil, clientError{Code: clientCodeUnknownClient, Message: fmt.Sprintf("unknown client %q", name)}
	}
	return adapter, nil
}

func sortedAdapters() []clientAdapter {
	adapters := registeredClientAdapters()
	names := make([]string, 0, len(adapters))
	for name := range adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]clientAdapter, 0, len(names))
	for _, name := range names {
		result = append(result, adapters[name])
	}
	return result
}

func registeredClientAdapters() map[string]clientAdapter {
	return map[string]clientAdapter{
		clientNameCodex: staticAdapter{spec: clientAdapterSpec{
			name:          clientNameCodex,
			defaultFormat: clientFormatJSON,
			formats:       []string{clientFormatJSON, clientFormatText},
			scopes:        []string{clientScopeUser, clientScopeProject},
			transports:    []string{mcpTransportStdio, mcpTransportHTTP},
			fields: []string{
				"transport", "command", "args", "cwd", "environment", "url", "headers", "enabled", "timeout_seconds",
			},
			limitations: []string{
				"prompt, rule, and environment adapters are not generated in this checkpoint",
			},
			capabilities: map[string]string{
				clientCapabilityMCP:         "full",
				clientCapabilityEnvironment: "unsupported",
				clientCapabilityPrompts:     "unsupported",
				clientCapabilityRules:       "unsupported",
			},
			canKeepUnresolvedSecrets:  true,
			supportsDisabledSemantics: true,
			supportsCwd:               true,
			supportsTimeout:           true,
			supportsServerEnvironment: true,
			supportsHTTPHeaders:       true,
		}},
		clientNameClaude: staticAdapter{spec: clientAdapterSpec{
			name:          clientNameClaude,
			defaultFormat: clientFormatYAML,
			formats:       []string{clientFormatYAML, clientFormatText},
			scopes:        []string{clientScopeUser, clientScopeProject},
			transports:    []string{mcpTransportStdio, mcpTransportHTTP},
			fields: []string{
				"transport", "command", "args", "environment", "url", "headers", "enabled",
			},
			limitations: []string{
				"claude adapter requires resolved secrets",
				"timeout_seconds and cwd are not representable",
			},
			capabilities: map[string]string{
				clientCapabilityMCP:         "partial",
				clientCapabilityEnvironment: "unsupported",
				clientCapabilityPrompts:     "unsupported",
				clientCapabilityRules:       "unsupported",
			},
			canKeepUnresolvedSecrets:  false,
			supportsDisabledSemantics: false,
			supportsCwd:               false,
			supportsTimeout:           false,
			supportsServerEnvironment: true,
			supportsHTTPHeaders:       true,
		}},
		clientNameCursor: staticAdapter{spec: clientAdapterSpec{
			name:          clientNameCursor,
			defaultFormat: clientFormatJSON,
			formats:       []string{clientFormatJSON, clientFormatText},
			scopes:        []string{clientScopeUser, clientScopeProject},
			transports:    []string{mcpTransportStdio, mcpTransportHTTP},
			fields: []string{
				"transport", "command", "args", "cwd", "environment", "url", "headers", "enabled",
			},
			limitations: []string{
				"timeout_seconds is not representable",
			},
			capabilities: map[string]string{
				clientCapabilityMCP:         "partial",
				clientCapabilityEnvironment: "unsupported",
				clientCapabilityPrompts:     "unsupported",
				clientCapabilityRules:       "unsupported",
			},
			canKeepUnresolvedSecrets:  true,
			supportsDisabledSemantics: true,
			supportsCwd:               true,
			supportsTimeout:           false,
			supportsServerEnvironment: true,
			supportsHTTPHeaders:       true,
		}},
		clientNameVSCode: staticAdapter{spec: clientAdapterSpec{
			name:          clientNameVSCode,
			defaultFormat: clientFormatJSON,
			formats:       []string{clientFormatJSON, clientFormatYAML, clientFormatTOML, clientFormatText},
			scopes:        []string{clientScopeUser, clientScopeProject},
			transports:    []string{mcpTransportStdio, mcpTransportHTTP},
			fields: []string{
				"transport", "command", "args", "cwd", "environment", "url", "headers", "enabled", "timeout_seconds",
			},
			limitations: []string{
				"if multiple VS Code MCP extension schemas are required, explicit format variants must be added",
			},
			capabilities: map[string]string{
				clientCapabilityMCP:         "full",
				clientCapabilityEnvironment: "unsupported",
				clientCapabilityPrompts:     "unsupported",
				clientCapabilityRules:       "unsupported",
			},
			canKeepUnresolvedSecrets:  true,
			supportsDisabledSemantics: true,
			supportsCwd:               true,
			supportsTimeout:           true,
			supportsServerEnvironment: true,
			supportsHTTPHeaders:       true,
		}},
	}
}

func serializeClientOutput(format string, payload map[string]any) (string, error) {
	switch format {
	case clientFormatJSON:
		content, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return "", err
		}
		return string(content), nil
	case clientFormatYAML:
		content, err := yaml.Marshal(payload)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(content)), nil
	case clientFormatTOML:
		content, err := toml.Marshal(payload)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(content)), nil
	case clientFormatText:
		return formatPayloadAsText(payload), nil
	default:
		return "", errors.New("unsupported format")
	}
}

func formatPayloadAsText(payload map[string]any) string {
	lines := []string{}
	for _, key := range mapKeys(payload) {
		lines = append(lines, formatTextLines("", key, payload[key])...)
	}
	return strings.Join(lines, "\n")
}

func formatTextLines(prefix, key string, value any) []string {
	fullKey := key
	if prefix != "" {
		fullKey = prefix + "." + key
	}
	switch typed := value.(type) {
	case map[string]any:
		lines := []string{}
		for _, child := range mapKeys(typed) {
			lines = append(lines, formatTextLines(fullKey, child, typed[child])...)
		}
		return lines
	case []string:
		return []string{fmt.Sprintf("%s=%s", fullKey, strings.Join(typed, ","))}
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
		return []string{fmt.Sprintf("%s=%s", fullKey, strings.Join(parts, ","))}
	default:
		return []string{fmt.Sprintf("%s=%v", fullKey, typed)}
	}
}

func filterClientServers(servers map[string]MCPServer, includeDisabled bool) map[string]MCPServer {
	result := map[string]MCPServer{}
	for _, name := range sortedMCPServerNames(servers) {
		server := cloneMCPServer(servers[name])
		if !includeDisabled && !server.Enabled {
			continue
		}
		result[name] = server
	}
	return result
}

func hasUnresolvedSecretReferences(servers map[string]MCPServer) bool {
	for _, name := range sortedMCPServerNames(servers) {
		server := servers[name]
		for _, key := range sortedStringMapKeys(server.Environment) {
			if strings.HasPrefix(server.Environment[key], "secret://") {
				return true
			}
		}
		for _, key := range sortedStringMapKeys(server.Headers) {
			if strings.HasPrefix(server.Headers[key], "secret://") {
				return true
			}
		}
	}
	return false
}

func convertClientWarningsToErrors(findings []ClientDiagnostic) []ClientDiagnostic {
	result := make([]ClientDiagnostic, 0, len(findings))
	for _, finding := range findings {
		if finding.Severity == "warning" {
			finding.Severity = "error"
		}
		result = append(result, finding)
	}
	return result
}

func sortClientDiagnostics(findings []ClientDiagnostic) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.Client != right.Client {
			return left.Client < right.Client
		}
		if left.Server != right.Server {
			return left.Server < right.Server
		}
		if left.Field != right.Field {
			return left.Field < right.Field
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Message < right.Message
	})
}

func countClientErrors(findings []ClientDiagnostic) int {
	count := 0
	for _, finding := range findings {
		if finding.Severity == "error" {
			count++
		}
	}
	return count
}

func filterClientDiagnosticsBySeverity(findings []ClientDiagnostic, severity string) []ClientDiagnostic {
	result := []ClientDiagnostic{}
	for _, finding := range findings {
		if finding.Severity == severity {
			result = append(result, finding)
		}
	}
	sortClientDiagnostics(result)
	return result
}

func formatClientDiagnosticSummary(client string, findings []ClientDiagnostic) string {
	errorsOnly := filterClientDiagnosticsBySeverity(findings, "error")
	if len(errorsOnly) == 0 {
		return fmt.Sprintf("client %q generation failed", client)
	}
	first := errorsOnly[0]
	return fmt.Sprintf("client %q field=%s code=%s message=%s", client, first.Field, first.Code, first.Message)
}

func cloneClientSourceModel(model ClientSourceModel) ClientSourceModel {
	servers := map[string]MCPServer{}
	for _, name := range sortedMCPServerNames(model.Servers) {
		servers[name] = cloneMCPServer(model.Servers[name])
	}
	return ClientSourceModel{
		Info:         model.Info,
		Resolved:     cloneMap(model.Resolved),
		Sources:      cloneStringSlice(model.Sources),
		Servers:      servers,
		LoadedSource: model.LoadedSource,
	}
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func formatCapabilitySummary(capabilities map[string]string) string {
	keys := clientMapKeysStringMap(capabilities)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s:%s", key, capabilities[key]))
	}
	return strings.Join(parts, ",")
}

func clientMapKeysStringMap(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeClientOutputFile(projectRoot, outputPath, content string, containsResolvedSecrets bool, force bool) error {
	absoluteOutput := outputPath
	if !filepath.IsAbs(absoluteOutput) {
		absoluteOutput = filepath.Join(projectRoot, outputPath)
	}
	absoluteOutput, err := filepath.Abs(absoluteOutput)
	if err != nil {
		return clientError{Code: clientCodeClientOutputWriteFailed, Message: "resolve output path failed"}
	}

	relative, err := filepath.Rel(projectRoot, absoluteOutput)
	if err != nil || strings.HasPrefix(relative, "..") || relative == ".." {
		return clientError{Code: clientCodeClientOutputWriteFailed, Message: "output path must be repository-local"}
	}

	parent := filepath.Dir(absoluteOutput)
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() {
		return clientError{Code: clientCodeClientOutputWriteFailed, Message: "output parent directory does not exist"}
	}

	if fileExists(absoluteOutput) && !force {
		return clientError{Code: clientCodeClientOutputExists, Message: "output file already exists; use --force to overwrite"}
	}

	tempFile, err := os.CreateTemp(parent, ".ai-dev-client-*")
	if err != nil {
		return clientError{Code: clientCodeClientOutputWriteFailed, Message: "create temporary output file failed"}
	}
	tempPath := tempFile.Name()
	cleanup := func() {
		_ = os.Remove(tempPath)
	}

	mode := os.FileMode(0o644)
	if containsResolvedSecrets {
		mode = 0o600
	}

	writeErr := func() error {
		defer tempFile.Close()
		if _, err := tempFile.WriteString(content); err != nil {
			return err
		}
		if err := tempFile.Sync(); err != nil {
			return err
		}
		if err := tempFile.Chmod(mode); err != nil {
			return err
		}
		return nil
	}()
	if writeErr != nil {
		cleanup()
		return clientError{Code: clientCodeClientOutputWriteFailed, Message: "write output file failed"}
	}

	if err := os.Rename(tempPath, absoluteOutput); err != nil {
		cleanup()
		return clientError{Code: clientCodeClientOutputWriteFailed, Message: "atomic rename for output file failed"}
	}
	return nil
}

func hasAmbiguousDestinations(destinations []ClientDestination) bool {
	perScope := map[string]int{}
	for _, destination := range destinations {
		perScope[destination.Scope]++
	}
	for _, count := range perScope {
		if count > 1 {
			return true
		}
	}
	return false
}

func countDestinationsForScope(destinations []ClientDestination, scope string) int {
	count := 0
	for _, destination := range destinations {
		if destination.Scope == scope {
			count++
		}
	}
	return count
}

func scopeOrDefault(scope string, fallback string) string {
	if scope != "" {
		return scope
	}
	return fallback
}

func clientDoctorSummary(paths Paths, info ProjectInfo, resolvedConfig map[string]any, loadedSources []loadedConfigSource) []string {
	lines := []string{}
	adapters := sortedAdapters()
	lines = append(lines, fmt.Sprintf("[ok] client adapters: registered=%d initialization_failures=0", len(adapters)))

	executables := []struct {
		Name    string
		Command string
	}{
		{Name: clientNameCodex, Command: "codex"},
		{Name: clientNameClaude, Command: "claude"},
		{Name: clientNameCursor, Command: "cursor"},
		{Name: clientNameVSCode, Command: "code"},
	}
	for _, executable := range executables {
		if path, err := exec.LookPath(executable.Command); err == nil {
			lines = append(lines, fmt.Sprintf("[ok] client executable: client=%s path=%s", executable.Name, path))
		} else {
			lines = append(lines, fmt.Sprintf("[notice] client executable not detected: client=%s command=%s", executable.Name, executable.Command))
		}
	}

	scopes := mcpServerScopes(loadedSources)
	servers, diagnostics := parseMCPServers(resolvedConfig, scopes)
	if len(diagnostics) > 0 {
		lines = append(lines, "[error] client compatibility skipped: invalid MCP definitions")
		return lines
	}

	model := ClientSourceModel{Info: info, Resolved: resolvedConfig, Servers: servers}
	for _, adapter := range adapters {
		pathResult, err := adapter.Destinations(paths, info, "")
		if err != nil {
			lines = append(lines, fmt.Sprintf("[error] client path discovery: client=%s message=%v", adapter.Name(), err))
			continue
		}
		if pathResult.Ambiguous {
			lines = append(lines, fmt.Sprintf("[notice] client path ambiguity: client=%s candidates=%d", adapter.Name(), len(pathResult.Destinations)))
		}
		validation := adapter.Validate(model, ClientValidateOptions{Scope: clientScopeProject})
		if validation.Valid {
			lines = append(lines, fmt.Sprintf("[ok] client compatibility: client=%s status=compatible", adapter.Name()))
		} else {
			lines = append(lines, fmt.Sprintf("[notice] client compatibility: client=%s status=incompatible issues=%d", adapter.Name(), countClientErrors(validation.Diagnostics)))
		}
	}

	return lines
}
