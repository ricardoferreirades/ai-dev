package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

const (
	validationCodeMissingSchema          = "missing_schema"
	validationCodeUnsupportedSchema      = "unsupported_schema"
	validationCodeUnknownField           = "unknown_field"
	validationCodeInvalidType            = "invalid_type"
	validationCodeInvalidValue           = "invalid_value"
	validationCodeDeprecatedField        = "deprecated_field"
	validationCodeConflictingValue       = "conflicting_value"
	validationCodeInvalidEnvironmentName = "invalid_environment_name"
	validationCodeInvalidEnvironmentVal  = "invalid_environment_value"
)

const resolvedValidationSource = "(resolved)"

var schemaValidators = map[string]func(string, map[string]any) []ValidationFinding{
	"v1": validateSchemaV1Fields,
}

type ValidationFinding struct {
	Source   string `json:"source"`
	Path     string `json:"path"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type ValidationReport struct {
	Valid    bool                `json:"valid"`
	Errors   []ValidationFinding `json:"errors"`
	Warnings []ValidationFinding `json:"warnings"`
	Sources  []string            `json:"sources"`
}

type loadedConfigSource struct {
	Path       string
	Config     map[string]any
	ParseError error
}

func validateConfigurationForCurrentProject(
	paths Paths,
	strict bool,
) (ValidationReport, error) {
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return ValidationReport{}, err
	}

	return validateConfigurationForProject(paths, info, strict)
}

func validateConfigurationForProject(
	paths Paths,
	info ProjectInfo,
	strict bool,
) (ValidationReport, error) {
	sources, resolved, orderedSources := loadConfigurationSources(paths, info)
	findings := make([]ValidationFinding, 0)

	for _, source := range sources {
		if source.ParseError != nil {
			findings = append(findings, ValidationFinding{
				Source:   source.Path,
				Path:     "$",
				Code:     validationCodeInvalidValue,
				Severity: "error",
				Message:  "invalid TOML syntax",
			})
			continue
		}

		findings = append(
			findings,
			validateConfigurationDocument(source.Path, source.Config)...,
		)
	}

	findings = append(findings, detectConflictingTopLevelShapes(sources)...)
	findings = append(findings, detectConflictingSchemaVersions(sources)...)
	if len(resolved) > 0 {
		findings = append(
			findings,
			validateConfigurationDocumentWithOptions(
				resolvedValidationSource,
				resolved,
				false,
			)...,
		)
	}

	sortValidationFindings(findings)

	report := ValidationReport{
		Errors:   []ValidationFinding{},
		Warnings: []ValidationFinding{},
		Sources:  orderedSources,
	}

	for _, finding := range findings {
		if finding.Severity == "warning" {
			report.Warnings = append(report.Warnings, finding)
		} else {
			report.Errors = append(report.Errors, finding)
		}
	}

	report.Valid = len(report.Errors) == 0 && (!strict || len(report.Warnings) == 0)
	return report, nil
}

func loadConfigurationSources(
	paths Paths,
	info ProjectInfo,
) ([]loadedConfigSource, map[string]any, []string) {
	globalPath := filepath.Join(paths.ConfigHome, "global.toml")
	projectPath := projectConfigPath(paths, info.ProjectID)
	candidates := []string{globalPath, projectPath}

	sources := make([]loadedConfigSource, 0, len(candidates))
	resolved := map[string]any{}
	orderedSources := make([]string, 0, len(candidates))

	for _, candidate := range candidates {
		if !fileExists(candidate) {
			continue
		}

		orderedSources = append(orderedSources, candidate)

		config, err := readTOML(candidate)
		source := loadedConfigSource{
			Path:       candidate,
			Config:     config,
			ParseError: err,
		}
		sources = append(sources, source)

		if err == nil {
			resolved = mergeMaps(resolved, config)
		}
	}

	return sources, resolved, orderedSources
}

func validateConfigurationDocument(
	source string,
	configuration map[string]any,
) []ValidationFinding {
	return validateConfigurationDocumentWithOptions(source, configuration, true)
}

func validateConfigurationDocumentWithOptions(
	source string,
	configuration map[string]any,
	warnMissingSchema bool,
) []ValidationFinding {
	findings := []ValidationFinding{}
	version := "v1"

	schemaValue, schemaExists := configuration["schema"]
	if !schemaExists {
		if warnMissingSchema {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "schema",
				Code:     validationCodeMissingSchema,
				Severity: "warning",
				Message:  "schema is missing; legacy v1 compatibility is deprecated",
			})
		}
	} else {
		schemaText, ok := schemaValue.(string)
		if !ok {
			return append(findings, ValidationFinding{
				Source:   source,
				Path:     "schema",
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "schema must be a string",
			})
		}

		version = schemaText
		validator, supported := schemaValidators[version]
		if !supported {
			supportedVersions := supportedSchemaVersionNames()
			return append(findings, ValidationFinding{
				Source:   source,
				Path:     "schema",
				Code:     validationCodeUnsupportedSchema,
				Severity: "error",
				Message: fmt.Sprintf(
					"unsupported schema version %q; supported versions: %s",
					version,
					strings.Join(supportedVersions, ", "),
				),
			})
		}

		return append(findings, validator(source, configuration)...)
	}

	validator := schemaValidators[version]
	if validator == nil {
		return findings
	}

	return append(findings, validator(source, configuration)...)
}

func validateSchemaV1Fields(
	source string,
	configuration map[string]any,
) []ValidationFinding {
	findings := []ValidationFinding{}
	topLevelAllowed := map[string]bool{
		"schema":      true,
		"name":        true,
		"profile":     true,
		"environment": true,
		"mcp":         true,
		"prompts":     true,
		"rules":       true,
	}

	topLevelKeys := mapKeys(configuration)
	for _, key := range topLevelKeys {
		if !topLevelAllowed[key] {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     key,
				Code:     validationCodeUnknownField,
				Severity: "error",
				Message:  "unknown top-level field",
			})
		}
	}

	findings = append(findings, validateStringField(source, "name", configuration["name"])...)
	findings = append(findings, validateStringField(source, "profile", configuration["profile"])...)

	findings = append(findings, validateEnvironmentField(source, configuration["environment"])...)
	findings = append(findings, validateMCPField(source, configuration["mcp"])...)
	findings = append(findings, validatePromptsField(source, configuration["prompts"])...)
	findings = append(findings, validateRulesField(source, configuration["rules"])...)

	return findings
}

func validateStringField(source, path string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}

	if _, ok := value.(string); ok {
		return nil
	}

	return []ValidationFinding{{
		Source:   source,
		Path:     path,
		Code:     validationCodeInvalidType,
		Severity: "error",
		Message:  fmt.Sprintf("%s must be a string", path),
	}}
}

func validateEnvironmentField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}

	environment, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "environment",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "environment must be a table",
		}}
	}

	keys := mapKeys(environment)
	findings := []ValidationFinding{}

	for _, key := range keys {
		if err := validateEnvironmentName(key); err != nil {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "environment." + key,
				Code:     validationCodeInvalidEnvironmentName,
				Severity: "error",
				Message:  "invalid environment variable name",
			})
			continue
		}

		if _, err := environmentStringValue(key, environment[key]); err != nil {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "environment." + key,
				Code:     validationCodeInvalidEnvironmentVal,
				Severity: "error",
				Message:  "environment variable must be string, boolean, integer, or float",
			})
		}
	}

	return findings
}

func validateMCPField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}

	mcp, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "mcp",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "mcp must be a table",
		}}
	}

	findings := []ValidationFinding{}
	allowed := map[string]bool{"servers": true}
	for _, key := range mapKeys(mcp) {
		if !allowed[key] {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "mcp." + key,
				Code:     validationCodeUnknownField,
				Severity: "error",
				Message:  "unknown field in mcp table",
			})
		}
	}

	if serversValue, exists := mcp["servers"]; exists {
		if finding, ok := validateStringArrayField(source, "mcp.servers", serversValue); ok {
			findings = append(findings, finding)
		}
	}

	return findings
}

func validatePromptsField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}

	prompts, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "prompts",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "prompts must be a table",
		}}
	}

	findings := []ValidationFinding{}
	allowed := map[string]bool{"default": true, "project": true}
	for _, key := range mapKeys(prompts) {
		if !allowed[key] {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "prompts." + key,
				Code:     validationCodeUnknownField,
				Severity: "error",
				Message:  "unknown field in prompts table",
			})
		}
	}

	if finding, ok := validateOptionalStringValue(
		source,
		"prompts.default",
		prompts["default"],
	); ok {
		findings = append(findings, finding)
	}

	if finding, ok := validateOptionalStringValue(
		source,
		"prompts.project",
		prompts["project"],
	); ok {
		findings = append(findings, finding)
	}

	return findings
}

func validateRulesField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}

	rules, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "rules",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "rules must be a table",
		}}
	}

	findings := []ValidationFinding{}
	allowed := map[string]bool{"enabled": true}
	for _, key := range mapKeys(rules) {
		if !allowed[key] {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "rules." + key,
				Code:     validationCodeUnknownField,
				Severity: "error",
				Message:  "unknown field in rules table",
			})
		}
	}

	if enabledValue, exists := rules["enabled"]; exists {
		if finding, ok := validateStringArrayField(source, "rules.enabled", enabledValue); ok {
			findings = append(findings, finding)
		}
	}

	return findings
}

func validateOptionalStringValue(
	source, path string,
	value any,
) (ValidationFinding, bool) {
	if value == nil {
		return ValidationFinding{}, false
	}

	if _, ok := value.(string); ok {
		return ValidationFinding{}, false
	}

	return ValidationFinding{
		Source:   source,
		Path:     path,
		Code:     validationCodeInvalidType,
		Severity: "error",
		Message:  fmt.Sprintf("%s must be a string", path),
	}, true
}

func validateStringArrayField(
	source, path string,
	value any,
) (ValidationFinding, bool) {
	values, ok := value.([]any)
	if !ok {
		return ValidationFinding{
			Source:   source,
			Path:     path,
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  fmt.Sprintf("%s must be an array of strings", path),
		}, true
	}

	for _, item := range values {
		if _, ok := item.(string); !ok {
			return ValidationFinding{
				Source:   source,
				Path:     path,
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  fmt.Sprintf("%s must be an array of strings", path),
			}, true
		}
	}

	return ValidationFinding{}, false
}

func detectConflictingTopLevelShapes(sources []loadedConfigSource) []ValidationFinding {
	findings := []ValidationFinding{}
	if len(sources) < 2 {
		return findings
	}

	topLevelFieldTypes := map[string]map[string]string{}
	for _, source := range sources {
		if source.ParseError != nil {
			continue
		}

		for _, key := range mapKeys(source.Config) {
			if _, exists := topLevelFieldTypes[key]; !exists {
				topLevelFieldTypes[key] = map[string]string{}
			}
			topLevelFieldTypes[key][source.Path] = valueTypeLabel(source.Config[key])
		}
	}

	for field, typedSources := range topLevelFieldTypes {
		typeNames := map[string]bool{}
		for _, typeName := range typedSources {
			typeNames[typeName] = true
		}

		if len(typeNames) <= 1 {
			continue
		}

		sourceDetails := make([]string, 0, len(typedSources))
		sourcePaths := mapKeysStringMap(typedSources)
		for _, sourcePath := range sourcePaths {
			sourceDetails = append(
				sourceDetails,
				fmt.Sprintf("%s=%s", sourcePath, typedSources[sourcePath]),
			)
		}

		findings = append(findings, ValidationFinding{
			Source:   resolvedValidationSource,
			Path:     field,
			Code:     validationCodeConflictingValue,
			Severity: "error",
			Message:  "conflicting field shapes across sources: " + strings.Join(sourceDetails, ", "),
		})
	}

	return findings
}

func detectConflictingSchemaVersions(sources []loadedConfigSource) []ValidationFinding {
	versions := map[string][]string{}

	for _, source := range sources {
		if source.ParseError != nil {
			continue
		}

		value, exists := source.Config["schema"]
		if !exists {
			continue
		}

		version, ok := value.(string)
		if !ok {
			continue
		}

		versions[version] = append(versions[version], source.Path)
	}

	if len(versions) <= 1 {
		return nil
	}

	return []ValidationFinding{{
		Source:   resolvedValidationSource,
		Path:     "schema",
		Code:     validationCodeConflictingValue,
		Severity: "error",
		Message:  "configuration sources declare conflicting schema versions",
	}}
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func mapKeysStringMap(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func valueTypeLabel(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case int64:
		return "integer"
	case float64:
		return "float"
	case []any:
		return "array"
	case map[string]any:
		return "table"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func supportedSchemaVersionNames() []string {
	versions := make([]string, 0, len(schemaValidators))
	for version := range schemaValidators {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	return versions
}

func sortValidationFindings(findings []ValidationFinding) {
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

func validationOutputJSON(report ValidationReport) (string, error) {
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode validation JSON: %w", err)
	}
	return string(content), nil
}
