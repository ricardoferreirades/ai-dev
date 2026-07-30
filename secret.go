package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

const (
	secretProviderEnv     = "env"
	secretProviderCommand = "command"
)

const (
	secretCodeInvalidReference   = "invalid_secret_reference"
	secretCodeUnknownProvider    = "unknown_secret_provider"
	secretCodeMissingValue       = "missing_secret_value"
	secretCodeEmptyValue         = "empty_secret_value"
	secretCodeMissingCommand     = "missing_secret_command"
	secretCodeCommandFailed      = "secret_command_failed"
	secretCodeCommandEmptyOutput = "secret_command_empty_output"
	secretCodeInvalidCommand     = "invalid_secret_command"
	secretCodeResolutionFailed   = "secret_resolution_failed"
)

type SecretReference struct {
	Provider  string
	Reference string
}

type SecretCommandDefinition struct {
	Command string
	Args    []string
}

type SecretResolutionResult struct {
	Provider  string `json:"provider"`
	Reference string `json:"reference"`
	Resolved  bool   `json:"resolved"`
	Error     string `json:"error"`
}

type secretProvider interface {
	Resolve(context.Context, SecretReference) (string, error)
}

type secretResolver struct {
	providers map[string]secretProvider
	cache     map[string]string
}

func newSecretResolver(definitions map[string]SecretCommandDefinition) *secretResolver {
	return &secretResolver{
		providers: map[string]secretProvider{
			secretProviderEnv:     envSecretProvider{},
			secretProviderCommand: commandSecretProvider{definitions: definitions},
		},
		cache: map[string]string{},
	}
}

func (resolver *secretResolver) Resolve(
	ctx context.Context,
	reference SecretReference,
) (string, error) {
	cacheKey := reference.Provider + "\x00" + reference.Reference
	if value, ok := resolver.cache[cacheKey]; ok {
		return value, nil
	}

	provider, ok := resolver.providers[reference.Provider]
	if !ok {
		return "", secretError{
			Code:      secretCodeUnknownProvider,
			Provider:  reference.Provider,
			Reference: reference.Reference,
			Message:   fmt.Sprintf("unknown secret provider %q", reference.Provider),
		}
	}

	value, err := provider.Resolve(ctx, reference)
	if err != nil {
		return "", err
	}

	resolver.cache[cacheKey] = value
	return value, nil
}

func (resolver *secretResolver) RegisterProvider(name string, provider secretProvider) error {
	if strings.TrimSpace(name) == "" {
		return secretError{Code: secretCodeUnknownProvider, Message: "secret provider name is empty"}
	}
	if provider == nil {
		return secretError{Code: secretCodeUnknownProvider, Message: "secret provider is nil"}
	}
	if _, exists := resolver.providers[name]; exists {
		return secretError{Code: secretCodeUnknownProvider, Provider: name, Message: fmt.Sprintf("secret provider %q is already registered", name)}
	}
	resolver.providers[name] = provider
	return nil
}

func newProjectSecretResolver(paths Paths, definitions map[string]SecretCommandDefinition) *secretResolver {
	resolver := newSecretResolver(definitions)
	_ = registerPluginSecretProviders(paths, resolver)
	return resolver
}

func parseSecretReference(raw string) (SecretReference, error) {
	if !strings.HasPrefix(raw, "secret://") {
		return SecretReference{}, secretError{
			Code:    secretCodeInvalidReference,
			Message: "secret reference must start with secret://",
		}
	}

	remainder := strings.TrimPrefix(raw, "secret://")
	if remainder == "" {
		return SecretReference{}, secretError{
			Code:    secretCodeInvalidReference,
			Message: "secret reference is missing a provider",
		}
	}

	parts := strings.SplitN(remainder, "/", 2)
	provider := parts[0]
	if provider == "" {
		return SecretReference{}, secretError{
			Code:    secretCodeInvalidReference,
			Message: "secret reference is missing a provider",
		}
	}

	if len(parts) < 2 || parts[1] == "" {
		return SecretReference{}, secretError{
			Code:     secretCodeInvalidReference,
			Provider: provider,
			Message:  "secret reference is missing a reference path",
		}
	}

	return SecretReference{
		Provider:  provider,
		Reference: parts[1],
	}, nil
}

func collectSecretReferences(configuration map[string]any) []SecretReference {
	environmentValue, exists := configuration["environment"]
	if !exists {
		return nil
	}

	environment, ok := environmentValue.(map[string]any)
	if !ok {
		return nil
	}

	references := map[string]SecretReference{}
	for _, value := range environment {
		valueString, ok := value.(string)
		if !ok || !strings.HasPrefix(valueString, "secret://") {
			continue
		}

		reference, err := parseSecretReference(valueString)
		if err != nil {
			continue
		}
		references[reference.Provider+"\x00"+reference.Reference] = reference
	}

	result := make([]SecretReference, 0, len(references))
	keys := make([]string, 0, len(references))
	for key := range references {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, references[key])
	}

	return result
}

func loadSecretCommandDefinitions(configuration map[string]any) map[string]SecretCommandDefinition {
	definitions := map[string]SecretCommandDefinition{}

	secretsValue, exists := configuration["secrets"]
	if !exists {
		return definitions
	}

	secrets, ok := secretsValue.(map[string]any)
	if !ok {
		return definitions
	}

	commandsValue, exists := secrets["commands"]
	if !exists {
		return definitions
	}

	commands, ok := commandsValue.(map[string]any)
	if !ok {
		return definitions
	}

	for name, value := range commands {
		commandTable, ok := value.(map[string]any)
		if !ok {
			continue
		}

		commandName, _ := commandTable["command"].(string)
		args := []string{}
		if rawArgs, ok := commandTable["args"].([]any); ok {
			for _, item := range rawArgs {
				if arg, ok := item.(string); ok {
					args = append(args, arg)
				}
			}
		}

		definitions[name] = SecretCommandDefinition{
			Command: commandName,
			Args:    args,
		}
	}

	return definitions
}

func resolveEnvironmentValues(
	ctx context.Context,
	environment map[string]any,
	resolver *secretResolver,
) (map[string]string, error) {
	values := make(map[string]string, len(environment))
	keys := make([]string, 0, len(environment))
	for key := range environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value, err := resolveEnvironmentValue(ctx, key, environment[key], resolver)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}

	return values, nil
}

func resolveEnvironmentValue(
	ctx context.Context,
	name string,
	value any,
	resolver *secretResolver,
) (string, error) {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "secret://") {
			reference, err := parseSecretReference(typed)
			if err != nil {
				return "", err
			}
			return resolver.Resolve(ctx, reference)
		}
		return typed, nil

	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil

	case int64:
		return fmt.Sprintf("%d", typed), nil

	case float64:
		return fmt.Sprintf("%v", typed), nil
	}

	return "", secretError{
		Code:      secretCodeResolutionFailed,
		Reference: name,
		Message:   fmt.Sprintf("environment variable %s cannot be resolved", name),
	}
}

func secretCheckResults(
	ctx context.Context,
	configuration map[string]any,
	resolver *secretResolver,
) ([]SecretResolutionResult, error) {
	references := collectSecretReferences(configuration)
	results := make([]SecretResolutionResult, 0, len(references))

	for _, reference := range references {
		_, err := resolver.Resolve(ctx, reference)
		if err != nil {
			results = append(results, SecretResolutionResult{
				Provider:  reference.Provider,
				Reference: reference.Reference,
				Resolved:  false,
				Error:     secretErrorMessage(err),
			})
			continue
		}

		results = append(results, SecretResolutionResult{
			Provider:  reference.Provider,
			Reference: reference.Reference,
			Resolved:  true,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		left := results[i]
		right := results[j]
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		if left.Reference != right.Reference {
			return left.Reference < right.Reference
		}
		if left.Error != right.Error {
			return left.Error < right.Error
		}
		return left.Resolved && !right.Resolved
	})

	return results, nil
}

func secretErrorMessage(err error) string {
	var typed secretError
	if errors.As(err, &typed) {
		return typed.Message
	}
	return "secret resolution failed"
}

type secretError struct {
	Code      string
	Provider  string
	Reference string
	Message   string
}

func (error secretError) Error() string {
	return error.Message
}

func (error secretError) SecretCode() string {
	return error.Code
}

type envSecretProvider struct{}

func (envSecretProvider) Resolve(_ context.Context, reference SecretReference) (string, error) {
	value := os.Getenv(reference.Reference)
	if value == "" {
		if _, exists := os.LookupEnv(reference.Reference); exists {
			return "", secretError{
				Code:      secretCodeEmptyValue,
				Provider:  reference.Provider,
				Reference: reference.Reference,
				Message:   fmt.Sprintf("environment variable %s is empty", reference.Reference),
			}
		}
		return "", secretError{
			Code:      secretCodeMissingValue,
			Provider:  reference.Provider,
			Reference: reference.Reference,
			Message:   fmt.Sprintf("environment variable %s is missing", reference.Reference),
		}
	}

	return value, nil
}

type commandSecretProvider struct {
	definitions map[string]SecretCommandDefinition
}

func (provider commandSecretProvider) Resolve(ctx context.Context, reference SecretReference) (string, error) {
	definition, exists := provider.definitions[reference.Reference]
	if !exists {
		return "", secretError{
			Code:      secretCodeMissingCommand,
			Provider:  reference.Provider,
			Reference: reference.Reference,
			Message:   fmt.Sprintf("secret command %q is not configured", reference.Reference),
		}
	}

	if definition.Command == "" {
		return "", secretError{
			Code:      secretCodeInvalidCommand,
			Provider:  reference.Provider,
			Reference: reference.Reference,
			Message:   fmt.Sprintf("secret command %q is missing command", reference.Reference),
		}
	}

	command := exec.CommandContext(ctx, definition.Command, definition.Args...)
	var standardError bytes.Buffer
	command.Stderr = &standardError
	output, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return "", secretError{
				Code:      secretCodeCommandFailed,
				Provider:  reference.Provider,
				Reference: reference.Reference,
				Message:   fmt.Sprintf("secret command %q failed", reference.Reference),
			}
		}

		return "", secretError{
			Code:      secretCodeResolutionFailed,
			Provider:  reference.Provider,
			Reference: reference.Reference,
			Message:   fmt.Sprintf("secret command %q could not be executed", reference.Reference),
		}
	}

	value := strings.TrimRight(string(output), "\r\n")
	if value == "" {
		return "", secretError{
			Code:      secretCodeCommandEmptyOutput,
			Provider:  reference.Provider,
			Reference: reference.Reference,
			Message:   fmt.Sprintf("secret command %q returned empty output", reference.Reference),
		}
	}

	return value, nil
}

func secretErrorCode(err error) string {
	var typed secretError
	if errors.As(err, &typed) {
		return typed.Code
	}
	return secretCodeResolutionFailed
}

func secretCommandDefinitionsFromResolved(configuration map[string]any) map[string]SecretCommandDefinition {
	return loadSecretCommandDefinitions(configuration)
}

func secretResolutionJSON(results []SecretResolutionResult, valid bool) (string, error) {
	payload := map[string]any{
		"valid":   valid,
		"results": results,
	}

	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}

	return string(content), nil
}
