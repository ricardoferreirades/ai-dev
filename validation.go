package main

import (
	"encoding/json"
	"errors"
	"fmt"
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
	SourceType string
	Identifier string
	SelectedBy string
	Exists     bool
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
			code := validationCodeInvalidValue
			message := "invalid TOML syntax"
			var registryErr registryError
			if errors.As(source.ParseError, &registryErr) {
				code = registryErr.Code
				message = registryErr.Message
			}
			findings = append(findings, ValidationFinding{
				Source:   source.Path,
				Path:     "$",
				Code:     code,
				Severity: "error",
				Message:  message,
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
	findings = append(findings, detectConflictingSecretCommands(sources)...)
	if len(resolved) > 0 {
		findings = append(
			findings,
			validateConfigurationDocumentWithOptions(
				resolvedValidationSource,
				resolved,
				false,
			)...,
		)
		findings = append(findings, validateResolvedSecretReferences(resolved)...)
		findings = append(findings, validatePromptAndRuleRegistries(paths, info, resolved, sources)...)
		findings = append(findings, pluginValidatorFindings(paths, resolved)...)
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
		"profiles":    true,
		"machine":     true,
		"plugins":     true,
		"policy":      true,
		"policies":    true,
		"bundles":     true,
		"environment": true,
		"mcp":         true,
		"prompts":     true,
		"rules":       true,
		"secrets":     true,
		"clients":     true,
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
	if finding, ok := validateOptionalStringValue(source, "profile", configuration["profile"]); ok {
		findings = append(findings, finding)
	} else if configuration["profile"] != nil {
		findings = append(findings, ValidationFinding{
			Source:   source,
			Path:     "profile",
			Code:     validationCodeDeprecatedField,
			Severity: "warning",
			Message:  "profile is deprecated; use profiles instead",
		})
	}
	if profilesValue, exists := configuration["profiles"]; exists {
		if finding, ok := validateStringArrayField(source, "profiles", profilesValue); ok {
			findings = append(findings, finding)
		}
	}
	findings = append(findings, validateMachineField(source, configuration["machine"])...)
	findings = append(findings, validatePluginsField(source, configuration["plugins"])...)
	findings = append(findings, validatePolicyField(source, configuration["policy"])...)
	findings = append(findings, validatePoliciesOverridesField(source, configuration["policies"])...)
	findings = append(findings, validateBundlesField(source, configuration["bundles"])...)

	findings = append(findings, validateEnvironmentField(source, configuration["environment"])...)
	findings = append(findings, validateMCPField(source, configuration["mcp"])...)
	findings = append(findings, validatePromptsField(source, configuration["prompts"])...)
	findings = append(findings, validateRulesField(source, configuration["rules"])...)
	findings = append(findings, validateSecretsField(source, configuration["secrets"])...)
	findings = append(findings, validateClientsField(source, configuration["clients"])...)

	return findings
}

func validatePolicyField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}
	table, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "policy",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "policy must be a table",
		}}
	}
	findings := []ValidationFinding{}
	for _, key := range mapKeys(table) {
		if key != "mode" {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "policy." + key,
				Code:     validationCodeUnknownField,
				Severity: "error",
				Message:  "unknown field in policy table",
			})
		}
	}
	if modeValue, exists := table["mode"]; exists {
		mode, ok := modeValue.(string)
		if !ok {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "policy.mode",
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "policy.mode must be a string",
			})
		} else if !policyModeValid(mode) {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "policy.mode",
				Code:     validationCodeInvalidValue,
				Severity: "error",
				Message:  "policy.mode must be disabled, advisory, or enforced",
			})
		}
	}
	return findings
}

func validatePoliciesOverridesField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}
	table, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "policies",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "policies must be a table",
		}}
	}
	findings := []ValidationFinding{}
	for policyID, entry := range table {
		if !policyIDValid(policyID) {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "policies." + policyID,
				Code:     policyCodeInvalidIdentifier,
				Severity: "error",
				Message:  "invalid policy identifier",
			})
			continue
		}
		override, ok := entry.(map[string]any)
		if !ok {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "policies." + policyID,
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "policy override must be a table",
			})
			continue
		}
		for _, key := range mapKeys(override) {
			if key != "enabled" && key != "enforcement" {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "policies." + policyID + "." + key,
					Code:     policyCodeOverrideInvalid,
					Severity: "error",
					Message:  "policy overrides only allow enabled and enforcement",
				})
			}
		}
		if enabledValue, exists := override["enabled"]; exists {
			if _, ok := enabledValue.(bool); !ok {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "policies." + policyID + ".enabled",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "enabled override must be a boolean",
				})
			}
		}
		if enforcementValue, exists := override["enforcement"]; exists {
			enforcement, ok := enforcementValue.(string)
			if !ok {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "policies." + policyID + ".enforcement",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "enforcement override must be a string",
				})
			} else if !policyModeValid(enforcement) {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "policies." + policyID + ".enforcement",
					Code:     validationCodeInvalidValue,
					Severity: "error",
					Message:  "enforcement override must be disabled, advisory, or enforced",
				})
			}
		}
	}
	return findings
}

func validateBundlesField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}
	bundles, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "bundles",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "bundles must be a table",
		}}
	}
	findings := []ValidationFinding{}
	for _, key := range mapKeys(bundles) {
		if key != "security" {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "bundles." + key,
				Code:     validationCodeUnknownField,
				Severity: "error",
				Message:  "unknown field in bundles table",
			})
		}
	}
	securityValue, exists := bundles["security"]
	if !exists {
		return findings
	}
	security, ok := securityValue.(map[string]any)
	if !ok {
		findings = append(findings, ValidationFinding{
			Source:   source,
			Path:     "bundles.security",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "bundles.security must be a table",
		})
		return findings
	}
	allowed := map[string]bool{"import_policy": true, "required_signers": true}
	for _, key := range mapKeys(security) {
		if !allowed[key] {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "bundles.security." + key,
				Code:     validationCodeUnknownField,
				Severity: "error",
				Message:  "unknown field in bundles.security",
			})
		}
	}
	if modeValue, exists := security["import_policy"]; exists {
		mode, ok := modeValue.(string)
		if !ok {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "bundles.security.import_policy",
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "import_policy must be a string",
			})
		} else {
			switch mode {
			case "allow-unsigned", "require-signed", "require-trusted", "require-specific-signers":
			default:
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "bundles.security.import_policy",
					Code:     validationCodeInvalidValue,
					Severity: "error",
					Message:  "unknown bundle security import_policy",
				})
			}
		}
	}
	if signersValue, exists := security["required_signers"]; exists {
		signers, ok := signersValue.([]any)
		if !ok {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "bundles.security.required_signers",
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "required_signers must be an array of key identifiers",
			})
		} else {
			for index, signer := range signers {
				signerID, ok := signer.(string)
				if !ok {
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     fmt.Sprintf("bundles.security.required_signers[%d]", index),
						Code:     validationCodeInvalidType,
						Severity: "error",
						Message:  "required signer must be a string",
					})
					continue
				}
				if err := validateKeyIdentifier(signerID); err != nil {
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     fmt.Sprintf("bundles.security.required_signers[%d]", index),
						Code:     securityCodeInvalidKeyIdentifier,
						Severity: "error",
						Message:  err.Error(),
					})
				}
			}
		}
	}
	return findings
}

func validateMachineField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}
	table, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "machine",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "machine must be a table",
		}}
	}

	findings := []ValidationFinding{}
	allowed := map[string]bool{"id": true}
	for _, key := range mapKeys(table) {
		if !allowed[key] {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "machine." + key,
				Code:     validationCodeUnknownField,
				Severity: "error",
				Message:  "unknown field in machine table",
			})
		}
	}

	if finding, ok := validateOptionalStringValue(source, "machine.id", table["id"]); ok {
		findings = append(findings, finding)
	}

	return findings
}

func validatePluginsField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}
	table, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "plugins",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "plugins must be a table",
		}}
	}

	findings := []ValidationFinding{}
	if pathsValue, exists := table["paths"]; exists {
		if finding, ok := validateStringArrayField(source, "plugins.paths", pathsValue); ok {
			findings = append(findings, finding)
		}
	}

	for _, key := range mapKeys(table) {
		if key == "paths" {
			continue
		}
		if err := validatePluginIdentifier(key); err != nil {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "plugins." + key,
				Code:     pluginCodeInvalidIdentifier,
				Severity: "error",
				Message:  err.Error(),
			})
			continue
		}

		entry, ok := table[key].(map[string]any)
		if !ok {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "plugins." + key,
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "plugin configuration must be a table",
			})
			continue
		}

		allowed := map[string]bool{
			"enabled":             true,
			"timeout_seconds":     true,
			"working_directory":   true,
			"inherit_environment": true,
			"environment":         true,
			"config":              true,
		}
		for _, entryKey := range mapKeys(entry) {
			if !allowed[entryKey] {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "plugins." + key + "." + entryKey,
					Code:     validationCodeUnknownField,
					Severity: "error",
					Message:  "unknown plugin configuration field",
				})
			}
		}

		if enabledValue, exists := entry["enabled"]; exists {
			if _, ok := enabledValue.(bool); !ok {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "plugins." + key + ".enabled",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "enabled must be a boolean",
				})
			}
		}
		if timeoutValue, exists := entry["timeout_seconds"]; exists {
			switch typed := timeoutValue.(type) {
			case int64:
				if typed <= 0 {
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     "plugins." + key + ".timeout_seconds",
						Code:     pluginCodeConfigurationInvalid,
						Severity: "error",
						Message:  "timeout_seconds must be greater than zero",
					})
				}
			default:
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "plugins." + key + ".timeout_seconds",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "timeout_seconds must be an integer",
				})
			}
		}
		if finding, ok := validateOptionalStringValue(source, "plugins."+key+".working_directory", entry["working_directory"]); ok {
			findings = append(findings, finding)
		}
		if inheritValue, exists := entry["inherit_environment"]; exists {
			if _, ok := inheritValue.(bool); !ok {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "plugins." + key + ".inherit_environment",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "inherit_environment must be a boolean",
				})
			}
		}
		if environmentValue, exists := entry["environment"]; exists {
			environmentTable, ok := environmentValue.(map[string]any)
			if !ok {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "plugins." + key + ".environment",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "environment must be a table",
				})
			} else {
				for _, envKey := range mapKeys(environmentTable) {
					if _, ok := environmentTable[envKey].(string); !ok {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "plugins." + key + ".environment." + envKey,
							Code:     validationCodeInvalidType,
							Severity: "error",
							Message:  "plugin environment value must be a string",
						})
					}
				}
			}
		}
		if configValue, exists := entry["config"]; exists {
			if _, ok := configValue.(map[string]any); !ok {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "plugins." + key + ".config",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "config must be a table",
				})
			}
		}
	}

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

		if stringValue, err := environmentStringValue(key, environment[key]); err != nil {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "environment." + key,
				Code:     validationCodeInvalidEnvironmentVal,
				Severity: "error",
				Message:  "environment variable must be string, boolean, integer, or float",
			})
		} else if strings.HasPrefix(stringValue, "secret://") {
			if finding := validateSecretReference(stringValue, source, "environment."+key); finding != nil {
				findings = append(findings, *finding)
			}
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
	resolvedMode := source == resolvedValidationSource
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
		switch servers := serversValue.(type) {
		case []any:
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "mcp.servers",
				Code:     validationCodeDeprecatedField,
				Severity: "warning",
				Message:  "legacy mcp.servers array syntax is deprecated; use [mcp.servers.<name>] tables",
			})

			seen := map[string]bool{}
			for _, item := range servers {
				name, ok := item.(string)
				if !ok {
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     "mcp.servers",
						Code:     validationCodeInvalidType,
						Severity: "error",
						Message:  "legacy mcp.servers must be an array of strings",
					})
					break
				}

				if err := validateMCPServerName(name); err != nil {
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     "mcp.servers",
						Code:     mcpCodeInvalidServerName,
						Severity: "error",
						Message:  "invalid MCP server name",
					})
				}

				if seen[name] {
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     "mcp.servers",
						Code:     mcpCodeDuplicateServer,
						Severity: "error",
						Message:  "duplicate MCP server name in legacy array",
					})
				}
				seen[name] = true
			}

		case map[string]any:
			for _, name := range mapKeys(servers) {
				if err := validateMCPServerName(name); err != nil {
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     "mcp.servers." + name,
						Code:     mcpCodeInvalidServerName,
						Severity: "error",
						Message:  "invalid MCP server name",
					})
					continue
				}

				definition, ok := servers[name].(map[string]any)
				if !ok {
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     "mcp.servers." + name,
						Code:     validationCodeInvalidType,
						Severity: "error",
						Message:  "MCP server definition must be a table",
					})
					continue
				}

				allowedServerFields := map[string]bool{
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
					if !allowedServerFields[key] {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + "." + key,
							Code:     validationCodeUnknownField,
							Severity: "error",
							Message:  "unknown MCP server field",
						})
					}
				}

				transportValue := ""
				if transportRaw, exists := definition["transport"]; exists {
					transportText, ok := transportRaw.(string)
					if !ok || transportText == "" {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".transport",
							Code:     mcpCodeUnsupportedTransport,
							Severity: "error",
							Message:  "transport must be stdio or http",
						})
						continue
					}
					transportValue = transportText
				} else if resolvedMode {
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     "mcp.servers." + name + ".transport",
						Code:     mcpCodeUnsupportedTransport,
						Severity: "error",
						Message:  "transport must be stdio or http",
					})
					continue
				}

				if enabledValue, exists := definition["enabled"]; exists {
					if _, ok := enabledValue.(bool); !ok {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".enabled",
							Code:     validationCodeInvalidType,
							Severity: "error",
							Message:  "enabled must be a boolean",
						})
					}
				}

				if timeoutValue, exists := definition["timeout_seconds"]; exists {
					timeout, ok := timeoutValue.(int64)
					if !ok || timeout <= 0 {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".timeout_seconds",
							Code:     mcpCodeInvalidTimeout,
							Severity: "error",
							Message:  "timeout_seconds must be a positive integer",
						})
					}
				}

				if inheritValue, exists := definition["inherit_environment"]; exists {
					if _, ok := inheritValue.(bool); !ok {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".inherit_environment",
							Code:     validationCodeInvalidType,
							Severity: "error",
							Message:  "inherit_environment must be a boolean",
						})
					}
				}

				switch transportValue {
				case mcpTransportStdio:
					if _, exists := definition["url"]; exists {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".url",
							Code:     mcpCodeConflictingFields,
							Severity: "error",
							Message:  "url is not allowed for stdio transport",
						})
					}
					if _, exists := definition["headers"]; exists {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".headers",
							Code:     mcpCodeConflictingFields,
							Severity: "error",
							Message:  "headers is not allowed for stdio transport",
						})
					}

					commandValue, exists := definition["command"]
					if !exists && resolvedMode {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".command",
							Code:     mcpCodeMissingCommand,
							Severity: "error",
							Message:  "stdio transport requires command",
						})
					} else if command, ok := commandValue.(string); !ok || strings.TrimSpace(command) == "" {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".command",
							Code:     mcpCodeInvalidCommand,
							Severity: "error",
							Message:  "command must be a non-empty string",
						})
					}

					if argsValue, exists := definition["args"]; exists {
						if finding, ok := validateStringArrayField(
							source,
							"mcp.servers."+name+".args",
							argsValue,
						); ok {
							finding.Code = mcpCodeInvalidArgs
							findings = append(findings, finding)
						}
					}

					if cwdValue, exists := definition["cwd"]; exists {
						if _, ok := cwdValue.(string); !ok {
							findings = append(findings, ValidationFinding{
								Source:   source,
								Path:     "mcp.servers." + name + ".cwd",
								Code:     validationCodeInvalidType,
								Severity: "error",
								Message:  "cwd must be a string",
							})
						}
					}

					if environmentValue, exists := definition["environment"]; exists {
						environmentTable, ok := environmentValue.(map[string]any)
						if !ok {
							findings = append(findings, ValidationFinding{
								Source:   source,
								Path:     "mcp.servers." + name + ".environment",
								Code:     mcpCodeInvalidEnvironment,
								Severity: "error",
								Message:  "environment must be a table",
							})
						} else {
							for _, key := range mapKeys(environmentTable) {
								if err := validateEnvironmentName(key); err != nil {
									findings = append(findings, ValidationFinding{
										Source:   source,
										Path:     "mcp.servers." + name + ".environment." + key,
										Code:     mcpCodeInvalidEnvironment,
										Severity: "error",
										Message:  "invalid environment variable name",
									})
									continue
								}

								stringValue, err := environmentStringValue(key, environmentTable[key])
								if err != nil {
									findings = append(findings, ValidationFinding{
										Source:   source,
										Path:     "mcp.servers." + name + ".environment." + key,
										Code:     mcpCodeInvalidEnvironment,
										Severity: "error",
										Message:  "environment variable must be string, boolean, integer, or float",
									})
									continue
								}

								if strings.HasPrefix(stringValue, "secret://") {
									if finding := validateSecretReference(
										stringValue,
										source,
										"mcp.servers."+name+".environment."+key,
									); finding != nil {
										finding.Code = mcpCodeInvalidEnvironment
										findings = append(findings, *finding)
									}
								}
							}
						}
					}

				case mcpTransportHTTP:
					if _, exists := definition["command"]; exists {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".command",
							Code:     mcpCodeConflictingFields,
							Severity: "error",
							Message:  "command is not allowed for http transport",
						})
					}
					if _, exists := definition["args"]; exists {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".args",
							Code:     mcpCodeConflictingFields,
							Severity: "error",
							Message:  "args is not allowed for http transport",
						})
					}
					if _, exists := definition["cwd"]; exists {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".cwd",
							Code:     mcpCodeConflictingFields,
							Severity: "error",
							Message:  "cwd is not allowed for http transport",
						})
					}
					if _, exists := definition["environment"]; exists {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".environment",
							Code:     mcpCodeConflictingFields,
							Severity: "error",
							Message:  "environment is not allowed for http transport",
						})
					}
					if _, exists := definition["inherit_environment"]; exists {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".inherit_environment",
							Code:     mcpCodeConflictingFields,
							Severity: "error",
							Message:  "inherit_environment is not allowed for http transport",
						})
					}

					urlValue, exists := definition["url"]
					if !exists && resolvedMode {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".url",
							Code:     mcpCodeMissingURL,
							Severity: "error",
							Message:  "http transport requires url",
						})
					} else if urlText, ok := urlValue.(string); !ok || strings.TrimSpace(urlText) == "" {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".url",
							Code:     mcpCodeInvalidURL,
							Severity: "error",
							Message:  "url must be a non-empty absolute HTTP or HTTPS URL",
						})
					} else if err := validateMCPHTTPURL(urlText); err != nil {
						findings = append(findings, ValidationFinding{
							Source:   source,
							Path:     "mcp.servers." + name + ".url",
							Code:     mcpCodeInvalidURL,
							Severity: "error",
							Message:  "url must be a valid absolute HTTP or HTTPS URL",
						})
					}

					if headersValue, exists := definition["headers"]; exists {
						headersTable, ok := headersValue.(map[string]any)
						if !ok {
							findings = append(findings, ValidationFinding{
								Source:   source,
								Path:     "mcp.servers." + name + ".headers",
								Code:     mcpCodeInvalidHeaders,
								Severity: "error",
								Message:  "headers must be a table with string values",
							})
						} else {
							for _, key := range mapKeys(headersTable) {
								headerValue, ok := headersTable[key].(string)
								if !ok {
									findings = append(findings, ValidationFinding{
										Source:   source,
										Path:     "mcp.servers." + name + ".headers." + key,
										Code:     mcpCodeInvalidHeaders,
										Severity: "error",
										Message:  "headers must be a table with string values",
									})
									continue
								}

								if strings.HasPrefix(headerValue, "secret://") {
									if finding := validateSecretReference(
										headerValue,
										source,
										"mcp.servers."+name+".headers."+key,
									); finding != nil {
										finding.Code = mcpCodeInvalidHeaders
										findings = append(findings, *finding)
									}
								}
							}
						}
					}

				case "":
					if commandValue, exists := definition["command"]; exists {
						if command, ok := commandValue.(string); !ok || strings.TrimSpace(command) == "" {
							findings = append(findings, ValidationFinding{
								Source:   source,
								Path:     "mcp.servers." + name + ".command",
								Code:     mcpCodeInvalidCommand,
								Severity: "error",
								Message:  "command must be a non-empty string",
							})
						}
					}
					if argsValue, exists := definition["args"]; exists {
						if finding, ok := validateStringArrayField(
							source,
							"mcp.servers."+name+".args",
							argsValue,
						); ok {
							finding.Code = mcpCodeInvalidArgs
							findings = append(findings, finding)
						}
					}
					if cwdValue, exists := definition["cwd"]; exists {
						if _, ok := cwdValue.(string); !ok {
							findings = append(findings, ValidationFinding{
								Source:   source,
								Path:     "mcp.servers." + name + ".cwd",
								Code:     validationCodeInvalidType,
								Severity: "error",
								Message:  "cwd must be a string",
							})
						}
					}
					if environmentValue, exists := definition["environment"]; exists {
						if _, ok := environmentValue.(map[string]any); !ok {
							findings = append(findings, ValidationFinding{
								Source:   source,
								Path:     "mcp.servers." + name + ".environment",
								Code:     mcpCodeInvalidEnvironment,
								Severity: "error",
								Message:  "environment must be a table",
							})
						}
					}
					if headersValue, exists := definition["headers"]; exists {
						if _, ok := headersValue.(map[string]any); !ok {
							findings = append(findings, ValidationFinding{
								Source:   source,
								Path:     "mcp.servers." + name + ".headers",
								Code:     mcpCodeInvalidHeaders,
								Severity: "error",
								Message:  "headers must be a table with string values",
							})
						}
					}

				default:
					findings = append(findings, ValidationFinding{
						Source:   source,
						Path:     "mcp.servers." + name + ".transport",
						Code:     mcpCodeUnsupportedTransport,
						Severity: "error",
						Message:  "transport must be stdio or http",
					})
				}
			}

		default:
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "mcp.servers",
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "mcp.servers must be a table of server definitions or a legacy array of strings",
			})
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
	allowed := map[string]bool{"default": true, "project": true, "enabled": true, "registry": true}
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

	if enabledValue, exists := prompts["enabled"]; exists {
		if finding, ok := validateStringArrayField(source, "prompts.enabled", enabledValue); ok {
			findings = append(findings, finding)
		}
	}

	if finding, ok := validateOptionalStringValue(
		source,
		"prompts.registry",
		prompts["registry"],
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
	allowed := map[string]bool{"enabled": true, "registry": true}
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

	if finding, ok := validateOptionalStringValue(
		source,
		"rules.registry",
		rules["registry"],
	); ok {
		findings = append(findings, finding)
	}

	return findings
}

func validateSecretsField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}

	secrets, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "secrets",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "secrets must be a table",
		}}
	}

	findings := []ValidationFinding{}
	allowed := map[string]bool{"commands": true}
	for _, key := range mapKeys(secrets) {
		if !allowed[key] {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "secrets." + key,
				Code:     validationCodeUnknownField,
				Severity: "error",
				Message:  "unknown field in secrets table",
			})
		}
	}

	commandsValue, exists := secrets["commands"]
	if !exists || commandsValue == nil {
		return findings
	}

	commands, ok := commandsValue.(map[string]any)
	if !ok {
		findings = append(findings, ValidationFinding{
			Source:   source,
			Path:     "secrets.commands",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "secrets.commands must be a table",
		})
		return findings
	}

	for _, name := range mapKeys(commands) {
		definition, ok := commands[name].(map[string]any)
		if !ok {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "secrets.commands." + name,
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "secret command definition must be a table",
			})
			continue
		}

		commandValue, exists := definition["command"]
		if !exists {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "secrets.commands." + name + ".command",
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "secret command definition requires command",
			})
		} else if _, ok := commandValue.(string); !ok {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "secrets.commands." + name + ".command",
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "secret command must be a string",
			})
		}

		if argsValue, exists := definition["args"]; exists {
			if args, ok := argsValue.([]any); !ok {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "secrets.commands." + name + ".args",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "secret command args must be an array of strings",
				})
			} else {
				for range args {
					// Element validation is intentionally generic to avoid echoing values.
				}
				if finding, ok := validateStringArrayField(
					source,
					"secrets.commands."+name+".args",
					argsValue,
				); ok {
					findings = append(findings, finding)
				}
			}
		}
	}

	return findings
}

func validateClientsField(source string, value any) []ValidationFinding {
	if value == nil {
		return nil
	}

	clients, ok := value.(map[string]any)
	if !ok {
		return []ValidationFinding{{
			Source:   source,
			Path:     "clients",
			Code:     validationCodeInvalidType,
			Severity: "error",
			Message:  "clients must be a table",
		}}
	}

	findings := []ValidationFinding{}
	for _, name := range mapKeys(clients) {
		if strings.TrimSpace(name) == "" {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "clients." + name,
				Code:     validationCodeInvalidValue,
				Severity: "error",
				Message:  "client name must not be empty",
			})
			continue
		}

		table, ok := clients[name].(map[string]any)
		if !ok {
			findings = append(findings, ValidationFinding{
				Source:   source,
				Path:     "clients." + name,
				Code:     validationCodeInvalidType,
				Severity: "error",
				Message:  "client override must be a table",
			})
			continue
		}

		allowedFields := map[string]bool{
			"enabled":    true,
			"definition": true,
		}
		for _, key := range mapKeys(table) {
			if !allowedFields[key] {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "clients." + name + "." + key,
					Code:     validationCodeUnknownField,
					Severity: "error",
					Message:  "unknown field in client override",
				})
			}
		}

		if enabledValue, exists := table["enabled"]; exists {
			if _, ok := enabledValue.(bool); !ok {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "clients." + name + ".enabled",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "enabled must be a boolean",
				})
			}
		}
		if definitionValue, exists := table["definition"]; exists {
			if definition, ok := definitionValue.(string); !ok || strings.TrimSpace(definition) == "" {
				findings = append(findings, ValidationFinding{
					Source:   source,
					Path:     "clients." + name + ".definition",
					Code:     validationCodeInvalidType,
					Severity: "error",
					Message:  "definition must be a non-empty path string",
				})
			}
		}
	}

	return findings
}

func validateSecretReference(raw, source, path string) *ValidationFinding {
	reference, err := parseSecretReference(raw)
	if err != nil {
		return &ValidationFinding{
			Source:   source,
			Path:     path,
			Code:     secretFindingCode(err),
			Severity: "error",
			Message:  secretFindingMessage(err),
		}
	}

	switch reference.Provider {
	case secretProviderEnv, secretProviderCommand:
		return nil
	default:
		return &ValidationFinding{
			Source:   source,
			Path:     path,
			Code:     secretCodeUnknownProvider,
			Severity: "error",
			Message:  fmt.Sprintf("unknown secret provider %q", reference.Provider),
		}
	}
}

func validateResolvedSecretReferences(configuration map[string]any) []ValidationFinding {
	definitions := loadSecretCommandDefinitions(configuration)
	findings := []ValidationFinding{}

	environmentValue, exists := configuration["environment"]
	if exists {
		environment, ok := environmentValue.(map[string]any)
		if ok {
			for _, key := range mapKeys(environment) {
				value, ok := environment[key].(string)
				if !ok || !strings.HasPrefix(value, "secret://") {
					continue
				}

				reference, err := parseSecretReference(value)
				if err != nil {
					findings = append(findings, ValidationFinding{
						Source:   resolvedValidationSource,
						Path:     "environment." + key,
						Code:     secretFindingCode(err),
						Severity: "error",
						Message:  secretFindingMessage(err),
					})
					continue
				}

				switch reference.Provider {
				case secretProviderEnv:
				case secretProviderCommand:
					if _, exists := definitions[reference.Reference]; !exists {
						findings = append(findings, ValidationFinding{
							Source:   resolvedValidationSource,
							Path:     "environment." + key,
							Code:     secretCodeMissingCommand,
							Severity: "error",
							Message:  fmt.Sprintf("secret command %q is not configured", reference.Reference),
						})
					}
				default:
					findings = append(findings, ValidationFinding{
						Source:   resolvedValidationSource,
						Path:     "environment." + key,
						Code:     secretCodeUnknownProvider,
						Severity: "error",
						Message:  fmt.Sprintf("unknown secret provider %q", reference.Provider),
					})
				}
			}
		}
	}

	mcpValue, exists := configuration["mcp"]
	if !exists {
		return findings
	}
	mcp, ok := mcpValue.(map[string]any)
	if !ok {
		return findings
	}

	serversValue, exists := mcp["servers"]
	if !exists {
		return findings
	}

	servers, ok := serversValue.(map[string]any)
	if !ok {
		return findings
	}

	for _, name := range mapKeys(servers) {
		definition, ok := servers[name].(map[string]any)
		if !ok {
			continue
		}

		if environmentValue, exists := definition["environment"]; exists {
			environmentTable, ok := environmentValue.(map[string]any)
			if ok {
				for _, key := range mapKeys(environmentTable) {
					value, ok := environmentTable[key].(string)
					if !ok || !strings.HasPrefix(value, "secret://") {
						continue
					}

					reference, err := parseSecretReference(value)
					if err != nil {
						findings = append(findings, ValidationFinding{
							Source:   resolvedValidationSource,
							Path:     "mcp.servers." + name + ".environment." + key,
							Code:     secretFindingCode(err),
							Severity: "error",
							Message:  secretFindingMessage(err),
						})
						continue
					}

					if reference.Provider == secretProviderCommand {
						if _, exists := definitions[reference.Reference]; !exists {
							findings = append(findings, ValidationFinding{
								Source:   resolvedValidationSource,
								Path:     "mcp.servers." + name + ".environment." + key,
								Code:     secretCodeMissingCommand,
								Severity: "error",
								Message:  fmt.Sprintf("secret command %q is not configured", reference.Reference),
							})
						}
					} else if reference.Provider != secretProviderEnv {
						findings = append(findings, ValidationFinding{
							Source:   resolvedValidationSource,
							Path:     "mcp.servers." + name + ".environment." + key,
							Code:     secretCodeUnknownProvider,
							Severity: "error",
							Message:  fmt.Sprintf("unknown secret provider %q", reference.Provider),
						})
					}
				}
			}
		}

		if headersValue, exists := definition["headers"]; exists {
			headersTable, ok := headersValue.(map[string]any)
			if ok {
				for _, key := range mapKeys(headersTable) {
					value, ok := headersTable[key].(string)
					if !ok || !strings.HasPrefix(value, "secret://") {
						continue
					}

					reference, err := parseSecretReference(value)
					if err != nil {
						findings = append(findings, ValidationFinding{
							Source:   resolvedValidationSource,
							Path:     "mcp.servers." + name + ".headers." + key,
							Code:     secretFindingCode(err),
							Severity: "error",
							Message:  secretFindingMessage(err),
						})
						continue
					}

					if reference.Provider == secretProviderCommand {
						if _, exists := definitions[reference.Reference]; !exists {
							findings = append(findings, ValidationFinding{
								Source:   resolvedValidationSource,
								Path:     "mcp.servers." + name + ".headers." + key,
								Code:     secretCodeMissingCommand,
								Severity: "error",
								Message:  fmt.Sprintf("secret command %q is not configured", reference.Reference),
							})
						}
					} else if reference.Provider != secretProviderEnv {
						findings = append(findings, ValidationFinding{
							Source:   resolvedValidationSource,
							Path:     "mcp.servers." + name + ".headers." + key,
							Code:     secretCodeUnknownProvider,
							Severity: "error",
							Message:  fmt.Sprintf("unknown secret provider %q", reference.Provider),
						})
					}
				}
			}
		}
	}

	return findings
}

func detectConflictingSecretCommands(sources []loadedConfigSource) []ValidationFinding {
	byName := map[string]map[string]string{}

	for _, source := range sources {
		if source.ParseError != nil {
			continue
		}

		secretsValue, exists := source.Config["secrets"]
		if !exists {
			continue
		}

		secrets, ok := secretsValue.(map[string]any)
		if !ok {
			continue
		}

		commandsValue, exists := secrets["commands"]
		if !exists {
			continue
		}

		commands, ok := commandsValue.(map[string]any)
		if !ok {
			continue
		}

		for name, value := range commands {
			if _, exists := byName[name]; !exists {
				byName[name] = map[string]string{}
			}
			byName[name][source.Path] = normalizedSecretCommandDefinition(value)
		}
	}

	findings := []ValidationFinding{}
	for name, definitions := range byName {
		if len(definitions) <= 1 {
			continue
		}

		unique := map[string]bool{}
		for _, definition := range definitions {
			unique[definition] = true
		}
		if len(unique) <= 1 {
			continue
		}

		findings = append(findings, ValidationFinding{
			Source:   resolvedValidationSource,
			Path:     "secrets.commands." + name,
			Code:     validationCodeConflictingValue,
			Severity: "error",
			Message:  "conflicting secret command definitions across sources",
		})
	}

	return findings
}

func normalizedSecretCommandDefinition(value any) string {
	definition, ok := value.(map[string]any)
	if !ok {
		return fmt.Sprintf("%T", value)
	}

	command := ""
	if rawCommand, ok := definition["command"].(string); ok {
		command = rawCommand
	}

	args := []string{}
	if rawArgs, ok := definition["args"].([]any); ok {
		for _, item := range rawArgs {
			if arg, ok := item.(string); ok {
				args = append(args, arg)
			}
		}
	}

	return command + "\x00" + strings.Join(args, "\x00")
}

func secretFindingCode(err error) string {
	return secretErrorCode(err)
}

func secretFindingMessage(err error) string {
	var typed secretError
	if errors.As(err, &typed) {
		return typed.Message
	}
	if err == nil {
		return "secret resolution failed"
	}
	return err.Error()
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
