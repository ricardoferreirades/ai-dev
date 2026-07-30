package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	sourceTypeGlobal      = "global"
	sourceTypeProfile     = "profile"
	sourceTypeMachine     = "machine"
	sourceTypeProject     = "project"
	sourceTypeCLIProfile  = "command-line profile"
	machineSourceCLI      = "--machine"
	machineSourceEnv      = "AI_DEV_MACHINE"
	machineSourceConfig   = "configuration"
	machineSourceHostname = "hostname"
)

const (
	profileCodeInvalidIdentifier  = "invalid_profile_identifier"
	profileCodeNotFound           = "profile_not_found"
	profileCodeDuplicateReference = "duplicate_profile_reference"
	profileCodeInvalidProfile     = "invalid_profile"
	profileCodeForbiddenField     = "forbidden_profile_field"
	profileCodeRecursiveReference = "recursive_profile_reference"
	profileCodeMergeConflict      = "profile_merge_conflict"
	machineCodeInvalidIdentifier  = "invalid_machine_identifier"
	machineCodeInvalidOverlay     = "invalid_machine_overlay"
	machineCodeForbiddenField     = "forbidden_machine_field"
	machineCodeMergeConflict      = "machine_merge_conflict"
	contextCodeResolutionFailed   = "context_resolution_failed"
	configCodeOriginNotFound      = "configuration_origin_not_found"
)

type runtimeResolutionOptions struct {
	MachineOverride string
	CLIProfiles     []string
	ProfileOnly     bool
	PluginPaths     []string
}

type appliedSource struct {
	Type       string
	Identifier string
	Path       string
	Exists     bool
	Valid      bool
	SelectedBy string
	Precedence int
	Config     map[string]any
	ParseError error
}

type activeProfile struct {
	Identifier string `json:"identifier"`
	SelectedBy string `json:"selected_by"`
	Path       string `json:"path"`
}

type resolvedContext struct {
	Info                 ProjectInfo
	MachineRawSource     string
	MachineRawIdentifier string
	MachineIdentifier    string
	MachineOverlayPath   string
	MachineOverlayExists bool
	Sources              []appliedSource
	ActiveProfiles       []activeProfile
	DuplicateProfiles    []activeProfile
	Resolved             map[string]any
}

var activeRuntimeOptions runtimeResolutionOptions

func parseRuntimeOptions(arguments []string) (runtimeResolutionOptions, []string, error) {
	opts := runtimeResolutionOptions{}
	remaining := []string{}

	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--machine":
			if index+1 >= len(arguments) {
				return runtimeResolutionOptions{}, nil, UsageError{Message: "--machine requires a value"}
			}
			index++
			opts.MachineOverride = arguments[index]
		case "--profile":
			if index+1 >= len(arguments) {
				return runtimeResolutionOptions{}, nil, UsageError{Message: "--profile requires a value"}
			}
			index++
			opts.CLIProfiles = append(opts.CLIProfiles, arguments[index])
		case "--profile-only":
			if index+1 >= len(arguments) {
				return runtimeResolutionOptions{}, nil, UsageError{Message: "--profile-only requires a value"}
			}
			index++
			opts.ProfileOnly = true
			opts.CLIProfiles = append(opts.CLIProfiles, arguments[index])
		case "--plugin-path":
			if index+1 >= len(arguments) {
				return runtimeResolutionOptions{}, nil, UsageError{Message: "--plugin-path requires a value"}
			}
			index++
			opts.PluginPaths = append(opts.PluginPaths, arguments[index])
		default:
			remaining = append(remaining, arguments[index:]...)
			return opts, remaining, nil
		}
	}

	return opts, remaining, nil
}

func resolveConfigurationWithContext(paths Paths, info ProjectInfo, includeProject bool) (resolvedContext, error) {
	return buildResolvedContext(paths, info, activeRuntimeOptions, includeProject, false)
}

func loadConfigurationSourcesWithContext(paths Paths, info ProjectInfo) (resolvedContext, error) {
	return buildResolvedContext(paths, info, activeRuntimeOptions, true, true)
}

func buildResolvedContext(
	paths Paths,
	info ProjectInfo,
	opts runtimeResolutionOptions,
	includeProject bool,
	tolerateErrors bool,
) (resolvedContext, error) {
	ctx := resolvedContext{Info: info, Sources: []appliedSource{}, ActiveProfiles: []activeProfile{}, DuplicateProfiles: []activeProfile{}, Resolved: map[string]any{}}

	globalPath := filepath.Join(paths.ConfigHome, "global.toml")
	projectPath := projectConfigPath(paths, info.ProjectID)
	profilesDir := filepath.Join(paths.ConfigHome, "profiles")
	machinesDir := filepath.Join(paths.ConfigHome, "machines")

	globalSource, hasGlobal, err := loadSource(sourceTypeGlobal, "global", globalPath)
	if err != nil {
		if !tolerateErrors {
			return resolvedContext{}, err
		}
		globalSource.ParseError = err
	}
	if hasGlobal || globalSource.ParseError != nil {
		ctx.appendSource(globalSource)
		if globalSource.ParseError == nil {
			ctx.Resolved = mergeMaps(ctx.Resolved, globalSource.Config)
		}
	}

	projectSource, hasProject, projectErr := loadSource(sourceTypeProject, "project", projectPath)
	if projectErr != nil {
		if !tolerateErrors {
			return resolvedContext{}, projectErr
		}
		projectSource.ParseError = projectErr
	}

	seenProfiles := map[string]bool{}
	configuredGlobalProfiles := []string{}
	if !opts.ProfileOnly && globalSource.ParseError == nil {
		configuredGlobalProfiles = configuredProfileList(globalSource.Config)
	}
	for _, profileID := range configuredGlobalProfiles {
		if err := ctx.applyProfile(profileID, profilesDir, sourceTypeProfile, sourceTypeGlobal, seenProfiles, tolerateErrors); err != nil {
			return resolvedContext{}, err
		}
	}

	ctx.detectMachine(paths, machinesDir, opts, tolerateErrors)
	if ctx.MachineIdentifier != "" {
		machinePath := filepath.Join(machinesDir, ctx.MachineIdentifier+".toml")
		ctx.MachineOverlayPath = machinePath
		machineSource, hasMachine, machineErr := loadSource(sourceTypeMachine, ctx.MachineIdentifier, machinePath)
		if machineErr != nil {
			if !tolerateErrors {
				return resolvedContext{}, registryOrMachineError(machineCodeInvalidOverlay, fmt.Sprintf("invalid machine overlay at %s", machinePath))
			}
			machineSource.ParseError = machineErr
		}
		ctx.MachineOverlayExists = hasMachine
		if hasMachine || machineSource.ParseError != nil {
			if machineSource.ParseError == nil {
				if err := validateMachineOverlayDefinition(machineSource.Config); err != nil {
					if !tolerateErrors {
						return resolvedContext{}, err
					}
					machineSource.ParseError = err
				}
			}
			ctx.appendSource(machineSource)
			if machineSource.ParseError == nil {
				ctx.Resolved = mergeMaps(ctx.Resolved, machineSource.Config)
			}
		}
	}

	for _, profileID := range opts.CLIProfiles {
		if err := ctx.applyProfile(profileID, profilesDir, sourceTypeCLIProfile, sourceTypeCLIProfile, seenProfiles, tolerateErrors); err != nil {
			return resolvedContext{}, err
		}
	}

	if includeProject && (hasProject || projectSource.ParseError != nil) {
		ctx.appendSource(projectSource)
		if projectSource.ParseError == nil {
			ctx.Resolved = mergeMaps(ctx.Resolved, projectSource.Config)
		}

		if !opts.ProfileOnly && projectSource.ParseError == nil {
			for _, profileID := range configuredProfileList(projectSource.Config) {
				if err := ctx.applyProfile(profileID, profilesDir, sourceTypeProfile, sourceTypeProject, seenProfiles, tolerateErrors); err != nil {
					return resolvedContext{}, err
				}
			}
		}
	}

	for index := range ctx.Sources {
		ctx.Sources[index].Precedence = index + 1
	}

	return ctx, nil
}

func loadSource(sourceType, identifier, path string) (appliedSource, bool, error) {
	source := appliedSource{Type: sourceType, Identifier: identifier, Path: path, Exists: false, Valid: true, Config: map[string]any{}}
	if !fileExists(path) {
		return source, false, nil
	}
	source.Exists = true
	config, err := readTOML(path)
	if err != nil {
		source.Valid = false
		return source, true, err
	}
	source.Config = config
	return source, true, nil
}

func (ctx *resolvedContext) appendSource(source appliedSource) {
	if source.Type == "" {
		return
	}
	ctx.Sources = append(ctx.Sources, source)
	if source.ParseError == nil {
		ctx.Resolved = mergeMaps(ctx.Resolved, source.Config)
	}
}

func configuredProfileList(configuration map[string]any) []string {
	value, exists := configuration["profiles"]
	if !exists {
		legacy, ok := configuration["profile"].(string)
		if !ok || strings.TrimSpace(legacy) == "" {
			return nil
		}
		return []string{strings.TrimSpace(legacy)}
	}
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	profiles := []string{}
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			continue
		}
		profiles = append(profiles, text)
	}
	return profiles
}

func validateProfileIdentifier(identifier string) error {
	if identifier == "" {
		return registryOrMachineError(profileCodeInvalidIdentifier, "profile identifier is empty")
	}
	for index, character := range identifier {
		isLetter := character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isHyphen := character == '-'
		isUnderscore := character == '_'
		if index == 0 {
			if !isLetter && !isDigit {
				return registryOrMachineError(profileCodeInvalidIdentifier, fmt.Sprintf("profile identifier %q must begin with a letter or digit", identifier))
			}
			continue
		}
		if !isLetter && !isDigit && !isHyphen && !isUnderscore {
			return registryOrMachineError(profileCodeInvalidIdentifier, fmt.Sprintf("profile identifier %q has invalid characters", identifier))
		}
	}
	return nil
}

func (ctx *resolvedContext) applyProfile(
	profileID string,
	profilesDir string,
	sourceType string,
	selectedBy string,
	seen map[string]bool,
	tolerateErrors bool,
) error {
	if err := validateProfileIdentifier(profileID); err != nil {
		if tolerateErrors {
			ctx.appendSource(appliedSource{Type: sourceType, Identifier: profileID, Path: filepath.Join(profilesDir, profileID+".toml"), Exists: false, Valid: false, ParseError: err, SelectedBy: selectedBy})
			return nil
		}
		return err
	}

	profilePath := filepath.Join(profilesDir, profileID+".toml")
	if seen[profileID] {
		ctx.DuplicateProfiles = append(ctx.DuplicateProfiles, activeProfile{Identifier: profileID, SelectedBy: selectedBy, Path: profilePath})
		return nil
	}
	seen[profileID] = true

	source, exists, err := loadSource(sourceType, profileID, profilePath)
	source.SelectedBy = selectedBy
	if !exists {
		err = registryOrMachineError(profileCodeNotFound, fmt.Sprintf("profile %q does not exist", profileID))
	}
	if err != nil {
		source.Valid = false
		source.ParseError = err
		if tolerateErrors {
			ctx.appendSource(source)
			ctx.ActiveProfiles = append(ctx.ActiveProfiles, activeProfile{Identifier: profileID, SelectedBy: selectedBy, Path: profilePath})
			return nil
		}
		return err
	}

	if err := validateProfileDefinition(source.Config); err != nil {
		source.Valid = false
		source.ParseError = err
		if tolerateErrors {
			ctx.appendSource(source)
			ctx.ActiveProfiles = append(ctx.ActiveProfiles, activeProfile{Identifier: profileID, SelectedBy: selectedBy, Path: profilePath})
			return nil
		}
		return err
	}

	source.Path = profilePath
	ctx.appendSource(source)
	ctx.ActiveProfiles = append(ctx.ActiveProfiles, activeProfile{Identifier: profileID, SelectedBy: selectedBy, Path: profilePath})
	return nil
}

func validateProfileDefinition(configuration map[string]any) error {
	if _, exists := configuration["profiles"]; exists {
		return registryOrMachineError(profileCodeRecursiveReference, "profiles cannot reference additional profiles")
	}
	for _, field := range []string{"machine", "project_id", "project_root", "repository", "config_home", "data_home", "state_home"} {
		if _, exists := configuration[field]; exists {
			return registryOrMachineError(profileCodeForbiddenField, fmt.Sprintf("profiles cannot define field %q", field))
		}
	}
	findings := validateConfigurationDocumentWithOptions("(profile)", configuration, true)
	for _, finding := range findings {
		if finding.Severity == "error" {
			return registryOrMachineError(profileCodeInvalidProfile, "profile schema validation failed")
		}
	}
	return nil
}

func validateMachineOverlayDefinition(configuration map[string]any) error {
	for _, field := range []string{"machine", "profiles", "project_id", "project_root", "repository", "config_home", "data_home", "state_home"} {
		if _, exists := configuration[field]; exists {
			return registryOrMachineError(machineCodeForbiddenField, fmt.Sprintf("machine overlay cannot define field %q", field))
		}
	}
	findings := validateConfigurationDocumentWithOptions("(machine)", configuration, true)
	for _, finding := range findings {
		if finding.Severity == "error" {
			return registryOrMachineError(machineCodeInvalidOverlay, "machine overlay schema validation failed")
		}
	}
	return nil
}

func (ctx *resolvedContext) detectMachine(paths Paths, machinesDir string, opts runtimeResolutionOptions, tolerateErrors bool) {
	raw := ""
	source := ""

	if strings.TrimSpace(opts.MachineOverride) != "" {
		raw = opts.MachineOverride
		source = machineSourceCLI
	} else if envMachine := strings.TrimSpace(os.Getenv("AI_DEV_MACHINE")); envMachine != "" {
		raw = envMachine
		source = machineSourceEnv
	} else if configuredMachine := configuredMachineOverride(ctx.Resolved); configuredMachine != "" {
		raw = configuredMachine
		source = machineSourceConfig
	} else {
		hostname, err := os.Hostname()
		if err == nil {
			raw = hostname
			source = machineSourceHostname
		}
	}

	ctx.MachineRawSource = source
	ctx.MachineRawIdentifier = raw
	identifier := normalizeMachineIdentifier(raw)
	if identifier == "" {
		if tolerateErrors {
			ctx.Sources = append(ctx.Sources, appliedSource{Type: sourceTypeMachine, Identifier: "", Path: filepath.Join(machinesDir, ".toml"), Exists: false, Valid: false, ParseError: registryOrMachineError(machineCodeInvalidIdentifier, "machine identifier is empty after normalization")})
			return
		}
		ctx.Sources = append(ctx.Sources, appliedSource{Type: sourceTypeMachine, Identifier: "", Path: filepath.Join(machinesDir, ".toml"), Exists: false, Valid: false, ParseError: registryOrMachineError(machineCodeInvalidIdentifier, "machine identifier is empty after normalization")})
		return
	}
	ctx.MachineIdentifier = identifier
}

func configuredMachineOverride(configuration map[string]any) string {
	machine, ok := configuration["machine"].(map[string]any)
	if !ok {
		return ""
	}
	value, ok := machine["id"].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func normalizeMachineIdentifier(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	invalid := regexp.MustCompile(`[^a-z0-9]+`)
	value = invalid.ReplaceAllString(value, "-")
	repeated := regexp.MustCompile(`-+`)
	value = repeated.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	return value
}

func registryOrMachineError(code, message string) error {
	return registryError{Code: code, Message: message}
}

func loadConfigurationSources(paths Paths, info ProjectInfo) ([]loadedConfigSource, map[string]any, []string) {
	ctx, err := loadConfigurationSourcesWithContext(paths, info)
	if err != nil {
		return []loadedConfigSource{}, map[string]any{}, []string{}
	}

	sources := make([]loadedConfigSource, 0, len(ctx.Sources))
	ordered := make([]string, 0, len(ctx.Sources))
	resolved := map[string]any{}
	for _, source := range ctx.Sources {
		if !source.Exists && source.ParseError == nil {
			continue
		}
		sources = append(sources, loadedConfigSource{
			Path:       source.Path,
			Config:     cloneMap(source.Config),
			ParseError: source.ParseError,
			SourceType: source.Type,
			Identifier: source.Identifier,
			SelectedBy: source.SelectedBy,
			Exists:     source.Exists,
		})
		ordered = append(ordered, source.Path)
		if source.ParseError == nil {
			resolved = mergeMaps(resolved, source.Config)
		}
	}

	return sources, resolved, ordered
}

func resolveConfiguration(paths Paths, info ProjectInfo) (map[string]any, []string, error) {
	ctx, err := resolveConfigurationWithContext(paths, info, true)
	if err != nil {
		return nil, nil, err
	}
	resolved := cloneMap(ctx.Resolved)
	sources := []string{}
	for _, source := range ctx.Sources {
		if source.Exists {
			sources = append(sources, source.Path)
		}
	}
	return resolved, sources, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func sortDiagnosticsBySource(findings []ValidationFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		if left.Message != right.Message {
			return left.Message < right.Message
		}
		return left.Severity < right.Severity
	})
}

func sourceLabel(source appliedSource) string {
	if source.Identifier == "" {
		return source.Type
	}
	return source.Type + ":" + source.Identifier
}

func configuredProfilesFromResolved(resolved map[string]any) []string {
	profiles := configuredProfileList(resolved)
	return uniqueStrings(profiles)
}

func fieldPathValue(configuration map[string]any, fieldPath string) (any, bool) {
	parts := strings.Split(fieldPath, ".")
	var current any = configuration
	for _, part := range parts {
		table, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, exists := table[part]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}

func isSensitiveField(fieldPath string) bool {
	lower := strings.ToLower(fieldPath)
	for _, token := range []string{"secret", "token", "password", "authorization", "api_key", "apikey"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func sanitizeOriginValue(fieldPath string, value any) any {
	if isSensitiveField(fieldPath) {
		return "[redacted]"
	}
	text, ok := value.(string)
	if ok && strings.HasPrefix(text, "secret://") {
		return "[secret-reference]"
	}
	return value
}

func validateContextModel(paths Paths, info ProjectInfo) (resolvedContext, error) {
	ctx, err := resolveConfigurationWithContext(paths, info, true)
	if err != nil {
		return resolvedContext{}, registryOrMachineError(contextCodeResolutionFailed, err.Error())
	}
	return ctx, nil
}

func validateMachineIdentifierForFlag(value string) error {
	if normalizeMachineIdentifier(value) == "" {
		return registryOrMachineError(machineCodeInvalidIdentifier, "machine identifier is invalid")
	}
	return nil
}

func validateExplicitProfiles(values []string) error {
	for _, value := range values {
		if err := validateProfileIdentifier(value); err != nil {
			return err
		}
	}
	return nil
}

func sourceByPath(sources []appliedSource, path string) (appliedSource, bool) {
	for _, source := range sources {
		if source.Path == path {
			return source, true
		}
	}
	return appliedSource{}, false
}

func mergeAction(previous any, next any) string {
	if previous == nil {
		return "inherited"
	}
	_, prevMap := previous.(map[string]any)
	_, nextMap := next.(map[string]any)
	if prevMap && nextMap {
		return "merged"
	}
	_, prevArray := previous.([]any)
	_, nextArray := next.([]any)
	if prevArray && nextArray {
		return "merged"
	}
	return "replaced"
}

func ensureNoSourceErrors(ctx resolvedContext) error {
	for _, source := range ctx.Sources {
		if source.ParseError != nil {
			return source.ParseError
		}
	}
	return nil
}
