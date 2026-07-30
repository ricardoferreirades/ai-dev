package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	registryKindPrompt = "prompt"
	registryKindRule   = "rule"
)

const (
	registryCodePromptNotFound        = "prompt_not_found"
	registryCodeRuleNotFound          = "rule_not_found"
	registryCodeDuplicatePrompt       = "duplicate_prompt"
	registryCodeDuplicateRule         = "duplicate_rule"
	registryCodeInvalidPromptMetadata = "invalid_prompt_metadata"
	registryCodeInvalidRuleMetadata   = "invalid_rule_metadata"
	registryCodeInvalidPromptID       = "invalid_prompt_identifier"
	registryCodeInvalidRuleID         = "invalid_rule_identifier"
	registryCodeUnsupportedPromptFmt  = "unsupported_prompt_format"
	registryCodeUnsupportedRuleFmt    = "unsupported_rule_format"
	registryCodeEmptyPrompt           = "empty_prompt"
	registryCodeEmptyRule             = "empty_rule"
)

const (
	registryCodeMalformedFrontMatter = "malformed_front_matter"
	registryCodeRegistryMissing      = "registry_path_missing"
	registryCodeRegistryUnreadable   = "registry_unreadable"
)

type registryError struct {
	Code    string
	Message string
}

func (err registryError) Error() string {
	if err.Code == "" {
		return err.Message
	}
	return fmt.Sprintf("code=%s %s", err.Code, err.Message)
}

type registryMetadata struct {
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Version     string   `json:"version,omitempty"`
	Author      string   `json:"author,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type registryResource struct {
	Kind       string           `json:"kind"`
	Identifier string           `json:"identifier"`
	Source     string           `json:"source"`
	Format     string           `json:"format"`
	Metadata   registryMetadata `json:"metadata"`
	Content    string           `json:"content"`
}

type registryDiagnostic struct {
	Severity   string `json:"severity"`
	Code       string `json:"code"`
	Kind       string `json:"kind"`
	Identifier string `json:"identifier,omitempty"`
	Path       string `json:"path,omitempty"`
	Message    string `json:"message"`
}

type registryIndex struct {
	Kind        string                      `json:"kind"`
	Registry    string                      `json:"registry_path"`
	Resources   map[string]registryResource `json:"resources"`
	Diagnostics []registryDiagnostic        `json:"diagnostics"`
}

type registryListEntry struct {
	Identifier string   `json:"identifier"`
	Title      string   `json:"title,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Source     string   `json:"source"`
}

func promptCommand(paths Paths, arguments []string) error {
	return registryCommand(paths, registryKindPrompt, arguments)
}

func ruleCommand(paths Paths, arguments []string) error {
	return registryCommand(paths, registryKindRule, arguments)
}

func registryCommand(paths Paths, kind string, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: fmt.Sprintf("%s requires a subcommand", kind)}
	}

	switch arguments[0] {
	case "list":
		return registryListCommand(paths, kind, arguments[1:])
	case "show":
		return registryShowCommand(paths, kind, arguments[1:])
	case "resolve":
		return registryResolveCommand(paths, kind, arguments[1:])
	case "search":
		return registrySearchCommand(paths, kind, arguments[1:])
	case "info":
		return registryInfoCommand(paths, kind, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown %s subcommand: %s", kind, arguments[0])}
	}
}

func registryListCommand(paths Paths, kind string, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown %s list option: %s", kind, argument)}
		}
	}

	index, err := resolveRegistryIndexForCurrentProject(paths, kind)
	if err != nil {
		return err
	}

	entries := registryListEntries(index)
	if jsonOutput {
		content, err := json.MarshalIndent(map[string]any{"resources": entries}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s list JSON: %w", kind, err)
		}
		fmt.Println(string(content))
		return nil
	}

	for _, entry := range entries {
		fmt.Printf("identifier=%s title=%s tags=%s source=%s\n", entry.Identifier, entry.Title, strings.Join(entry.Tags, ","), entry.Source)
	}
	return nil
}

func registryShowCommand(paths Paths, kind string, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: fmt.Sprintf("%s show requires an identifier", kind)}
	}
	identifier := arguments[0]
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown %s show option: %s", kind, argument)}
		}
	}

	index, err := resolveRegistryIndexForCurrentProject(paths, kind)
	if err != nil {
		return err
	}
	resource, exists := index.Resources[identifier]
	if !exists {
		return registryMissingIdentifierError(kind, identifier)
	}

	content, err := json.MarshalIndent(resource, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s show output: %w", kind, err)
	}
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}

	fmt.Println(string(content))
	return nil
}

func registryResolveCommand(paths Paths, kind string, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown %s resolve option: %s", kind, argument)}
		}
	}

	model, err := resolveRegistrySourceModel(paths)
	if err != nil {
		return err
	}
	index, err := registryIndexFromModel(paths, model, kind)
	if err != nil {
		return err
	}

	enabled, err := collectEnabledRegistryIdentifiers(kind, model.LoadedSource, model.Resolved)
	if err != nil {
		return err
	}
	content, err := composeRegistryDocument(kind, index, enabled)
	if err != nil {
		return err
	}

	if jsonOutput {
		payload := map[string]any{
			"kind":        kind,
			"identifiers": enabled,
			"content":     content,
		}
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s resolve JSON: %w", kind, err)
		}
		fmt.Println(string(encoded))
		return nil
	}

	fmt.Print(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		fmt.Println()
	}
	return nil
}

func registrySearchCommand(paths Paths, kind string, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: fmt.Sprintf("%s search requires a query", kind)}
	}

	jsonOutput := false
	queryParts := []string{}
	for _, argument := range arguments {
		if argument == "--json" {
			jsonOutput = true
			continue
		}
		queryParts = append(queryParts, argument)
	}
	if len(queryParts) == 0 {
		return UsageError{Message: fmt.Sprintf("%s search requires a query", kind)}
	}
	query := strings.ToLower(strings.Join(queryParts, " "))

	index, err := resolveRegistryIndexForCurrentProject(paths, kind)
	if err != nil {
		return err
	}

	matches := []registryListEntry{}
	for _, identifier := range sortedRegistryIdentifiers(index.Resources) {
		resource := index.Resources[identifier]
		if registrySearchMatch(resource, query) {
			matches = append(matches, registryListEntry{
				Identifier: resource.Identifier,
				Title:      resource.Metadata.Title,
				Tags:       cloneStringSlice(resource.Metadata.Tags),
				Source:     resource.Source,
			})
		}
	}

	if jsonOutput {
		content, err := json.MarshalIndent(map[string]any{"matches": matches}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s search JSON: %w", kind, err)
		}
		fmt.Println(string(content))
		return nil
	}

	for _, match := range matches {
		fmt.Printf("identifier=%s title=%s tags=%s source=%s\n", match.Identifier, match.Title, strings.Join(match.Tags, ","), match.Source)
	}
	return nil
}

func registryInfoCommand(paths Paths, kind string, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown %s info option: %s", kind, argument)}
		}
	}

	model, err := resolveRegistrySourceModel(paths)
	if err != nil {
		return err
	}
	index, err := registryIndexFromModel(paths, model, kind)
	if err != nil {
		return err
	}

	status := map[string]any{
		"kind":          kind,
		"registry_path": index.Registry,
		"count":         len(index.Resources),
		"valid":         !registryHasErrors(index.Diagnostics),
		"diagnostics":   index.Diagnostics,
	}

	if jsonOutput {
		content, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return fmt.Errorf("encode %s info JSON: %w", kind, err)
		}
		fmt.Println(string(content))
		return nil
	}

	fmt.Printf("kind=%s registry_path=%s count=%d valid=%t\n", kind, index.Registry, len(index.Resources), !registryHasErrors(index.Diagnostics))
	for _, finding := range index.Diagnostics {
		fmt.Printf("[%s] code=%s identifier=%s path=%s message=%s\n", finding.Severity, finding.Code, finding.Identifier, finding.Path, finding.Message)
	}
	return nil
}

func resolveRegistrySourceModel(paths Paths) (ClientSourceModel, error) {
	return resolveClientSourceModel(paths)
}

func resolveRegistryIndexForCurrentProject(paths Paths, kind string) (registryIndex, error) {
	model, err := resolveRegistrySourceModel(paths)
	if err != nil {
		return registryIndex{}, err
	}
	return registryIndexFromModel(paths, model, kind)
}

func registryIndexFromModel(paths Paths, model ClientSourceModel, kind string) (registryIndex, error) {
	registryPath := resolveRegistryPath(paths, model.Info, model.Resolved, kind)
	index := discoverRegistry(kind, registryPath)
	index = mergePluginRegistryResources(paths, kind, index)
	if len(index.Resources) > 0 {
		filtered := make([]registryDiagnostic, 0, len(index.Diagnostics))
		for _, finding := range index.Diagnostics {
			if finding.Code == registryCodeRegistryMissing {
				continue
			}
			filtered = append(filtered, finding)
		}
		index.Diagnostics = filtered
	}
	if registryHasErrors(index.Diagnostics) {
		first := firstRegistryError(index.Diagnostics)
		return registryIndex{}, registryError{Code: first.Code, Message: first.Message}
	}
	return index, nil
}

func mergePluginRegistryResources(paths Paths, kind string, index registryIndex) registryIndex {
	resources, pluginFindings := pluginProvidedRegistryResources(paths, kind)
	for _, finding := range pluginFindings {
		severity := "error"
		if finding.Severity == "warning" {
			severity = "warning"
		}
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity: severity,
			Code:     finding.Code,
			Kind:     kind,
			Path:     finding.Path,
			Message:  finding.Message,
		})
	}

	for _, resource := range resources {
		if err := validateRegistryIdentifier(resource.Identifier); err != nil {
			index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
				Severity:   "error",
				Code:       registryInvalidIDCode(kind),
				Kind:       kind,
				Identifier: resource.Identifier,
				Path:       resource.Source,
				Message:    "invalid registry identifier",
			})
			continue
		}
		if _, exists := index.Resources[resource.Identifier]; exists {
			index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
				Severity:   "error",
				Code:       registryDuplicateCode(kind),
				Kind:       kind,
				Identifier: resource.Identifier,
				Path:       resource.Source,
				Message:    "duplicate registry identifier",
			})
			continue
		}
		index.Resources[resource.Identifier] = resource
	}

	sortRegistryDiagnostics(index.Diagnostics)
	return index
}

func resolveRegistryPath(paths Paths, info ProjectInfo, configuration map[string]any, kind string) string {
	defaultPath := filepath.Join(paths.ConfigHome, kind+"s")

	section, ok := configuration[kind+"s"].(map[string]any)
	if !ok {
		return defaultPath
	}
	value, ok := section["registry"].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return defaultPath
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	if strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Clean(filepath.Join(home, strings.TrimPrefix(value, "~/")))
		}
	}

	return filepath.Clean(filepath.Join(info.ProjectRoot, value))
}

func discoverRegistry(kind, registryPath string) registryIndex {
	index := registryIndex{
		Kind:        kind,
		Registry:    registryPath,
		Resources:   map[string]registryResource{},
		Diagnostics: []registryDiagnostic{},
	}

	info, err := os.Stat(registryPath)
	if err != nil {
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity: "error",
			Code:     registryCodeRegistryMissing,
			Kind:     kind,
			Path:     registryPath,
			Message:  "registry path does not exist",
		})
		return index
	}
	if !info.IsDir() {
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity: "error",
			Code:     registryCodeRegistryUnreadable,
			Kind:     kind,
			Path:     registryPath,
			Message:  "registry path is not a directory",
		})
		return index
	}

	files := []string{}
	walkErr := filepath.WalkDir(registryPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
				Severity: "error",
				Code:     registryCodeRegistryUnreadable,
				Kind:     kind,
				Path:     path,
				Message:  "registry path cannot be traversed",
			})
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity: "error",
			Code:     registryCodeRegistryUnreadable,
			Kind:     kind,
			Path:     registryPath,
			Message:  "registry discovery failed",
		})
		return index
	}

	sort.Strings(files)
	for _, filePath := range files {
		index.parseRegistryFile(filePath)
	}

	sortRegistryDiagnostics(index.Diagnostics)
	return index
}

func (index *registryIndex) parseRegistryFile(filePath string) {
	kind := index.Kind
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".md" && ext != ".txt" {
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity: "error",
			Code:     registryUnsupportedFormatCode(kind),
			Kind:     kind,
			Path:     filePath,
			Message:  "only .md and .txt registry files are supported",
		})
		return
	}

	relative, err := filepath.Rel(index.Registry, filePath)
	if err != nil {
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity: "error",
			Code:     registryInvalidIDCode(kind),
			Kind:     kind,
			Path:     filePath,
			Message:  "failed to compute resource identifier",
		})
		return
	}

	identifier := strings.TrimSuffix(filepath.ToSlash(relative), ext)
	if err := validateRegistryIdentifier(identifier); err != nil {
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity:   "error",
			Code:       registryInvalidIDCode(kind),
			Kind:       kind,
			Identifier: identifier,
			Path:       filePath,
			Message:    "invalid registry identifier",
		})
		return
	}

	if _, exists := index.Resources[identifier]; exists {
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity:   "error",
			Code:       registryDuplicateCode(kind),
			Kind:       kind,
			Identifier: identifier,
			Path:       filePath,
			Message:    "duplicate registry identifier",
		})
		return
	}

	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity:   "error",
			Code:       registryMissingCode(kind),
			Kind:       kind,
			Identifier: identifier,
			Path:       filePath,
			Message:    "registry resource file is unreadable",
		})
		return
	}

	metadata, body, findings := parseRegistryFrontMatter(kind, identifier, filePath, string(contentBytes))
	index.Diagnostics = append(index.Diagnostics, findings...)
	if strings.TrimSpace(body) == "" {
		index.Diagnostics = append(index.Diagnostics, registryDiagnostic{
			Severity:   "error",
			Code:       registryEmptyCode(kind),
			Kind:       kind,
			Identifier: identifier,
			Path:       filePath,
			Message:    "registry resource content is empty",
		})
		return
	}

	index.Resources[identifier] = registryResource{
		Kind:       kind,
		Identifier: identifier,
		Source:     filePath,
		Format:     strings.TrimPrefix(ext, "."),
		Metadata:   metadata,
		Content:    body,
	}
}

func parseRegistryFrontMatter(kind, identifier, sourcePath, content string) (registryMetadata, string, []registryDiagnostic) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return registryMetadata{}, content, nil
	}

	separator := "\n---\n"
	separatorLength := len(separator)
	startOffset := 4
	if strings.HasPrefix(content, "---\r\n") {
		separator = "\r\n---\r\n"
		separatorLength = len(separator)
		startOffset = 5
	}

	endIndex := strings.Index(content[startOffset:], separator)
	if endIndex < 0 {
		return registryMetadata{}, content, []registryDiagnostic{{
			Severity:   "error",
			Code:       registryInvalidMetadataCode(kind),
			Kind:       kind,
			Identifier: identifier,
			Path:       sourcePath,
			Message:    "malformed front matter",
		}}
	}

	endIndex += startOffset
	frontMatter := content[startOffset:endIndex]
	body := content[endIndex+separatorLength:]

	decoded := map[string]any{}
	if err := yaml.Unmarshal([]byte(frontMatter), &decoded); err != nil {
		return registryMetadata{}, content, []registryDiagnostic{{
			Severity:   "error",
			Code:       registryInvalidMetadataCode(kind),
			Kind:       kind,
			Identifier: identifier,
			Path:       sourcePath,
			Message:    "malformed front matter",
		}}
	}

	metadata := registryMetadata{}
	findings := []registryDiagnostic{}
	allowed := map[string]bool{
		"title":       true,
		"description": true,
		"version":     true,
		"author":      true,
		"tags":        true,
	}

	for _, key := range mapKeys(decoded) {
		if !allowed[key] {
			findings = append(findings, registryDiagnostic{
				Severity:   "warning",
				Code:       registryInvalidMetadataCode(kind),
				Kind:       kind,
				Identifier: identifier,
				Path:       sourcePath,
				Message:    "unknown metadata field " + key,
			})
		}
	}

	if value, exists := decoded["title"]; exists {
		text, ok := value.(string)
		if !ok {
			findings = append(findings, registryDiagnostic{Severity: "error", Code: registryInvalidMetadataCode(kind), Kind: kind, Identifier: identifier, Path: sourcePath, Message: "title metadata must be a string"})
		} else {
			metadata.Title = text
		}
	}
	if value, exists := decoded["description"]; exists {
		text, ok := value.(string)
		if !ok {
			findings = append(findings, registryDiagnostic{Severity: "error", Code: registryInvalidMetadataCode(kind), Kind: kind, Identifier: identifier, Path: sourcePath, Message: "description metadata must be a string"})
		} else {
			metadata.Description = text
		}
	}
	if value, exists := decoded["version"]; exists {
		text, ok := value.(string)
		if !ok {
			findings = append(findings, registryDiagnostic{Severity: "error", Code: registryInvalidMetadataCode(kind), Kind: kind, Identifier: identifier, Path: sourcePath, Message: "version metadata must be a string"})
		} else {
			metadata.Version = text
		}
	}
	if value, exists := decoded["author"]; exists {
		text, ok := value.(string)
		if !ok {
			findings = append(findings, registryDiagnostic{Severity: "error", Code: registryInvalidMetadataCode(kind), Kind: kind, Identifier: identifier, Path: sourcePath, Message: "author metadata must be a string"})
		} else {
			metadata.Author = text
		}
	}
	if value, exists := decoded["tags"]; exists {
		raw, ok := value.([]any)
		if !ok {
			findings = append(findings, registryDiagnostic{Severity: "error", Code: registryInvalidMetadataCode(kind), Kind: kind, Identifier: identifier, Path: sourcePath, Message: "tags metadata must be an array of strings"})
		} else {
			tags := make([]string, 0, len(raw))
			valid := true
			for _, item := range raw {
				text, ok := item.(string)
				if !ok {
					valid = false
					break
				}
				tags = append(tags, text)
			}
			if !valid {
				findings = append(findings, registryDiagnostic{Severity: "error", Code: registryInvalidMetadataCode(kind), Kind: kind, Identifier: identifier, Path: sourcePath, Message: "tags metadata must be an array of strings"})
			} else {
				metadata.Tags = tags
			}
		}
	}

	return metadata, body, findings
}

func collectEnabledRegistryIdentifiers(kind string, sources []loadedConfigSource, resolved map[string]any) ([]string, error) {
	ordered := []string{}
	for _, source := range sources {
		if source.ParseError != nil {
			continue
		}
		ordered = append(ordered, enabledIdentifiersFromConfig(kind, source.Config)...)
	}
	if len(ordered) == 0 {
		ordered = enabledIdentifiersFromConfig(kind, resolved)
	}

	seen := map[string]bool{}
	result := []string{}
	for _, identifier := range ordered {
		if seen[identifier] {
			continue
		}
		seen[identifier] = true
		result = append(result, identifier)
	}
	return result, nil
}

func enabledIdentifiersFromConfig(kind string, config map[string]any) []string {
	section, ok := config[kind+"s"].(map[string]any)
	if !ok {
		return nil
	}
	value, exists := section["enabled"]
	if !exists {
		return nil
	}
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
		result = append(result, text)
	}
	return result
}

func composeRegistryDocument(kind string, index registryIndex, identifiers []string) (string, error) {
	parts := []string{}
	for _, identifier := range identifiers {
		resource, exists := index.Resources[identifier]
		if !exists {
			return "", registryMissingIdentifierError(kind, identifier)
		}
		parts = append(parts, normalizeRegistryContent(resource.Content))
	}
	return strings.Join(parts, "\n"), nil
}

func normalizeRegistryContent(content string) string {
	if content == "" {
		return ""
	}
	if strings.HasSuffix(content, "\n") {
		return content
	}
	return content + "\n"
}

func registrySearchMatch(resource registryResource, query string) bool {
	if strings.Contains(strings.ToLower(resource.Identifier), query) {
		return true
	}
	if strings.Contains(strings.ToLower(resource.Metadata.Title), query) {
		return true
	}
	if strings.Contains(strings.ToLower(resource.Metadata.Description), query) {
		return true
	}
	for _, tag := range resource.Metadata.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

func registryListEntries(index registryIndex) []registryListEntry {
	entries := make([]registryListEntry, 0, len(index.Resources))
	for _, identifier := range sortedRegistryIdentifiers(index.Resources) {
		resource := index.Resources[identifier]
		entries = append(entries, registryListEntry{
			Identifier: resource.Identifier,
			Title:      resource.Metadata.Title,
			Tags:       cloneStringSlice(resource.Metadata.Tags),
			Source:     resource.Source,
		})
	}
	return entries
}

func sortedRegistryIdentifiers(resources map[string]registryResource) []string {
	identifiers := make([]string, 0, len(resources))
	for identifier := range resources {
		identifiers = append(identifiers, identifier)
	}
	sort.Strings(identifiers)
	return identifiers
}

func validateRegistryIdentifier(identifier string) error {
	if identifier == "" {
		return errors.New("empty identifier")
	}
	for index, character := range identifier {
		isLetter := character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		isHyphen := character == '-'
		isUnderscore := character == '_'
		isSlash := character == '/'

		if index == 0 {
			if !isLetter && !isDigit {
				return errors.New("identifier must start with a letter or digit")
			}
			continue
		}

		if !isLetter && !isDigit && !isHyphen && !isUnderscore && !isSlash {
			return errors.New("identifier has an invalid character")
		}
	}
	if strings.Contains(identifier, "//") || strings.HasSuffix(identifier, "/") {
		return errors.New("identifier has invalid namespace separator")
	}
	return nil
}

func registryMissingIdentifierError(kind, identifier string) error {
	if kind == registryKindPrompt {
		return registryError{Code: registryCodePromptNotFound, Message: fmt.Sprintf("prompt %q does not exist", identifier)}
	}
	return registryError{Code: registryCodeRuleNotFound, Message: fmt.Sprintf("rule %q does not exist", identifier)}
}

func registryHasErrors(findings []registryDiagnostic) bool {
	for _, finding := range findings {
		if finding.Severity == "error" {
			return true
		}
	}
	return false
}

func firstRegistryError(findings []registryDiagnostic) registryDiagnostic {
	for _, finding := range findings {
		if finding.Severity == "error" {
			return finding
		}
	}
	return registryDiagnostic{Severity: "error", Code: "registry_error", Message: "registry validation failed"}
}

func sortRegistryDiagnostics(findings []registryDiagnostic) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i]
		right := findings[j]
		if left.Severity != right.Severity {
			return left.Severity < right.Severity
		}
		if left.Identifier != right.Identifier {
			return left.Identifier < right.Identifier
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

func registryUnsupportedFormatCode(kind string) string {
	if kind == registryKindPrompt {
		return registryCodeUnsupportedPromptFmt
	}
	return registryCodeUnsupportedRuleFmt
}

func registryDuplicateCode(kind string) string {
	if kind == registryKindPrompt {
		return registryCodeDuplicatePrompt
	}
	return registryCodeDuplicateRule
}

func registryInvalidMetadataCode(kind string) string {
	if kind == registryKindPrompt {
		return registryCodeInvalidPromptMetadata
	}
	return registryCodeInvalidRuleMetadata
}

func registryInvalidIDCode(kind string) string {
	if kind == registryKindPrompt {
		return registryCodeInvalidPromptID
	}
	return registryCodeInvalidRuleID
}

func registryEmptyCode(kind string) string {
	if kind == registryKindPrompt {
		return registryCodeEmptyPrompt
	}
	return registryCodeEmptyRule
}

func registryMissingCode(kind string) string {
	if kind == registryKindPrompt {
		return registryCodePromptNotFound
	}
	return registryCodeRuleNotFound
}

func validatePromptAndRuleRegistries(paths Paths, info ProjectInfo, resolved map[string]any, sources []loadedConfigSource) []ValidationFinding {
	findings := []ValidationFinding{}

	for _, kind := range []string{registryKindPrompt, registryKindRule} {
		registryPath := resolveRegistryPath(paths, info, resolved, kind)
		index := discoverRegistry(kind, registryPath)
		index = mergePluginRegistryResources(paths, kind, index)
		enabled, _ := collectEnabledRegistryIdentifiers(kind, sources, resolved)
		requireRegistry := len(enabled) > 0
		for _, diagnostic := range index.Diagnostics {
			if diagnostic.Code == registryCodeRegistryMissing && !requireRegistry {
				continue
			}
			severity := "error"
			if diagnostic.Severity == "warning" {
				severity = "warning"
			}
			findings = append(findings, ValidationFinding{
				Source:   resolvedValidationSource,
				Path:     kind + "s",
				Code:     diagnostic.Code,
				Severity: severity,
				Message:  diagnostic.Message,
			})
		}

		for _, identifier := range enabled {
			if _, exists := index.Resources[identifier]; !exists {
				code := registryCodeRuleNotFound
				message := fmt.Sprintf("rule %q is referenced but missing", identifier)
				if kind == registryKindPrompt {
					code = registryCodePromptNotFound
					message = fmt.Sprintf("prompt %q is referenced but missing", identifier)
				}
				findings = append(findings, ValidationFinding{
					Source:   resolvedValidationSource,
					Path:     kind + "s.enabled",
					Code:     code,
					Severity: "error",
					Message:  message,
				})
			}
		}
	}

	return findings
}

func registryDoctorLines(paths Paths, info ProjectInfo, resolved map[string]any, sources []loadedConfigSource) []string {
	lines := []string{}
	for _, kind := range []string{registryKindPrompt, registryKindRule} {
		registryPath := resolveRegistryPath(paths, info, resolved, kind)
		index := discoverRegistry(kind, registryPath)
		index = mergePluginRegistryResources(paths, kind, index)
		enabled, _ := collectEnabledRegistryIdentifiers(kind, sources, resolved)
		requireRegistry := len(enabled) > 0
		missing := 0
		for _, identifier := range enabled {
			if _, exists := index.Resources[identifier]; !exists {
				missing++
			}
		}
		duplicates := 0
		invalidMetadata := 0
		for _, diagnostic := range index.Diagnostics {
			switch diagnostic.Code {
			case registryDuplicateCode(kind):
				duplicates++
			case registryInvalidMetadataCode(kind):
				invalidMetadata++
			}
		}

		status := "ok"
		hasBlockingDiagnostics := false
		for _, diagnostic := range index.Diagnostics {
			if diagnostic.Severity != "error" {
				continue
			}
			if diagnostic.Code == registryCodeRegistryMissing && !requireRegistry {
				continue
			}
			hasBlockingDiagnostics = true
		}
		if hasBlockingDiagnostics || missing > 0 {
			status = "error"
		} else if len(index.Diagnostics) > 0 {
			status = "notice"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s registry: path=%s resources=%d missing_references=%d duplicate_identifiers=%d invalid_metadata=%d", status, kind, index.Registry, len(index.Resources), missing, duplicates, invalidMetadata))
	}
	return lines
}
