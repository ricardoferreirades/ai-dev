package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	policySchemaV1 = "policy-v1"

	policyOutcomePass = "pass"
	policyOutcomeWarn = "warn"
	policyOutcomeFail = "fail"
	policyOutcomeSkip = "skip"

	policySeverityInfo     = "info"
	policySeverityWarning  = "warning"
	policySeverityError    = "error"
	policySeverityCritical = "critical"

	policyModeDisabled = "disabled"
	policyModeAdvisory = "advisory"
	policyModeEnforced = "enforced"
)

const (
	policyCodeNotFound          = "policy_not_found"
	policyCodeDuplicate         = "duplicate_policy"
	policyCodeInvalid           = "invalid_policy"
	policyCodeUnsupportedSchema = "unsupported_policy_schema"
	policyCodeInvalidIdentifier = "invalid_policy_identifier"
	policyCodeEvalFailed        = "policy_evaluation_failed"
	policyCodeConditionInvalid  = "policy_condition_invalid"
	policyCodeScopeInvalid      = "policy_scope_invalid"
	policyCodeEnforcementFailed = "policy_enforcement_failed"
	policyCodeOverrideInvalid   = "policy_override_invalid"
	policyCodeConflict          = "policy_conflict"
	policyCodeOperationBlocked  = "policy_operation_blocked"
	policyCodePluginFailed      = "policy_plugin_failed"
)

type policyError struct {
	Code    string
	Message string
}

func (err policyError) Error() string {
	if err.Code == "" {
		return err.Message
	}
	return fmt.Sprintf("code=%s %s", err.Code, err.Message)
}

type policyDefinition struct {
	Schema       string         `toml:"schema" json:"schema"`
	Identifier   string         `toml:"id" json:"id"`
	Title        string         `toml:"title" json:"title"`
	Description  string         `toml:"description" json:"description"`
	Version      string         `toml:"version" json:"version"`
	Author       string         `toml:"author" json:"author"`
	Tags         []string       `toml:"tags" json:"tags"`
	Severity     string         `toml:"severity" json:"severity"`
	Enabled      *bool          `toml:"enabled" json:"enabled"`
	Enforcement  string         `toml:"enforcement" json:"enforcement"`
	Scopes       []string       `toml:"scopes" json:"scopes"`
	Target       string         `toml:"target" json:"target"`
	Condition    map[string]any `toml:"condition" json:"condition"`
	Message      string         `toml:"message" json:"message"`
	FailureCode  string         `toml:"failure_code" json:"failure_code"`
	FailureState string         `toml:"failure_outcome" json:"failure_outcome"`
	Path         string         `toml:"-" json:"path"`
	Namespace    string         `toml:"-" json:"namespace"`
	Unknown      []string       `toml:"-" json:"unknown_fields,omitempty"`
}

type policyOverride struct {
	Enabled     *bool
	Enforcement string
}

type policyFinding struct {
	PolicyID   string `json:"policy_id"`
	Severity   string `json:"severity"`
	Outcome    string `json:"outcome"`
	Path       string `json:"path"`
	Provenance string `json:"provenance"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

type policyResult struct {
	PolicyID     string          `json:"policy_id"`
	Title        string          `json:"title"`
	Severity     string          `json:"severity"`
	Scope        []string        `json:"scope"`
	Enabled      bool            `json:"enabled"`
	Enforcement  string          `json:"enforcement"`
	Outcome      string          `json:"outcome"`
	Findings     []policyFinding `json:"findings"`
	EvaluatedAt  string          `json:"evaluated_at"`
	Explanation  string          `json:"explanation,omitempty"`
	PolicySource string          `json:"policy_source"`
}

type policySummary struct {
	Passed               int     `json:"passed"`
	Warned               int     `json:"warned"`
	Failed               int     `json:"failed"`
	Skipped              int     `json:"skipped"`
	CompliancePercentage float64 `json:"compliance_percentage"`
}

type policyReport struct {
	Mode         string          `json:"mode"`
	Operation    string          `json:"operation"`
	Results      []policyResult  `json:"results"`
	Summary      policySummary   `json:"summary"`
	PluginIssues []policyFinding `json:"plugin_issues,omitempty"`
}

type policyEvaluationContext struct {
	Resolved        map[string]any
	ResolvedContext resolvedContext
	Operation       string
	BundleMetadata  map[string]any
	Scopes          []string
}

func policyRegistryPath(paths Paths) string {
	return filepath.Join(paths.ConfigHome, "policies")
}

func policyIDValid(id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	pattern := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-/]*$`)
	return pattern.MatchString(id)
}

func policyModeValid(mode string) bool {
	switch mode {
	case policyModeDisabled, policyModeAdvisory, policyModeEnforced:
		return true
	default:
		return false
	}
}

func defaultPolicyEnabled(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func defaultPolicyEnforcement(value string) string {
	if value == "" {
		return policyModeEnforced
	}
	return value
}

func discoverPolicies(paths Paths) ([]policyDefinition, []ValidationFinding, error) {
	root := policyRegistryPath(paths)
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []policyDefinition{}, []ValidationFinding{}, nil
		}
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, policyError{Code: policyCodeInvalid, Message: "policy registry path is not a directory"}
	}

	files := []string{}
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, entryErr error) error {
		if entryErr != nil {
			return entryErr
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".toml" {
			files = append(files, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}
	sort.Strings(files)

	policies := []policyDefinition{}
	warnings := []ValidationFinding{}
	seen := map[string]string{}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, nil, policyError{Code: policyCodeInvalid, Message: fmt.Sprintf("read policy file failed: %s", file)}
		}
		raw := map[string]any{}
		if err := toml.Unmarshal(content, &raw); err != nil {
			return nil, nil, policyError{Code: policyCodeInvalid, Message: fmt.Sprintf("parse policy file failed: %s", file)}
		}
		policy := policyDefinition{}
		if err := toml.Unmarshal(content, &policy); err != nil {
			return nil, nil, policyError{Code: policyCodeInvalid, Message: fmt.Sprintf("decode policy file failed: %s", file)}
		}
		policy.Path = file
		relative, _ := filepath.Rel(root, file)
		policy.Namespace = filepath.ToSlash(strings.TrimSuffix(relative, filepath.Ext(relative)))
		unknownFields := unknownPolicyFields(raw)
		for _, field := range unknownFields {
			warnings = append(warnings, ValidationFinding{
				Source:   file,
				Path:     field,
				Code:     validationCodeUnknownField,
				Severity: "warning",
				Message:  "unknown policy metadata field",
			})
		}
		if policy.Schema != policySchemaV1 {
			return nil, nil, policyError{Code: policyCodeUnsupportedSchema, Message: fmt.Sprintf("unsupported policy schema %q in %s", policy.Schema, file)}
		}
		if !policyIDValid(policy.Identifier) {
			return nil, nil, policyError{Code: policyCodeInvalidIdentifier, Message: fmt.Sprintf("invalid policy identifier in %s", file)}
		}
		if prior, exists := seen[policy.Identifier]; exists {
			return nil, nil, policyError{Code: policyCodeDuplicate, Message: fmt.Sprintf("duplicate policy identifier %q in %s and %s", policy.Identifier, prior, file)}
		}
		seen[policy.Identifier] = file
		if policy.Severity == "" {
			policy.Severity = policySeverityWarning
		}
		if !policySeverityValid(policy.Severity) {
			return nil, nil, policyError{Code: policyCodeInvalid, Message: fmt.Sprintf("invalid policy severity %q in %s", policy.Severity, file)}
		}
		if len(policy.Scopes) == 0 {
			policy.Scopes = []string{"global"}
		}
		for _, scope := range policy.Scopes {
			if !policyScopeValid(scope) {
				return nil, nil, policyError{Code: policyCodeScopeInvalid, Message: fmt.Sprintf("invalid policy scope %q in %s", scope, file)}
			}
		}
		if policy.Condition == nil {
			policy.Condition = map[string]any{"op": "exists", "path": "schema"}
		}
		if policy.FailureState == "" {
			policy.FailureState = policyOutcomeFail
		}
		if !policyOutcomeValid(policy.FailureState) {
			return nil, nil, policyError{Code: policyCodeInvalid, Message: fmt.Sprintf("invalid failure outcome %q in %s", policy.FailureState, file)}
		}
		policy.Enforcement = defaultPolicyEnforcement(policy.Enforcement)
		if !policyModeValid(policy.Enforcement) {
			return nil, nil, policyError{Code: policyCodeInvalid, Message: fmt.Sprintf("invalid policy enforcement %q in %s", policy.Enforcement, file)}
		}
		policies = append(policies, policy)
	}
	return policies, warnings, nil
}

func unknownPolicyFields(raw map[string]any) []string {
	allowed := map[string]bool{
		"schema":          true,
		"id":              true,
		"title":           true,
		"description":     true,
		"version":         true,
		"author":          true,
		"tags":            true,
		"severity":        true,
		"enabled":         true,
		"enforcement":     true,
		"scopes":          true,
		"target":          true,
		"condition":       true,
		"message":         true,
		"failure_code":    true,
		"failure_outcome": true,
	}
	keys := mapKeys(raw)
	unknown := []string{}
	for _, key := range keys {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

func policySeverityValid(severity string) bool {
	switch severity {
	case policySeverityInfo, policySeverityWarning, policySeverityError, policySeverityCritical:
		return true
	default:
		return false
	}
}

func policyScopeValid(scope string) bool {
	switch scope {
	case "global", "project", "profile", "machine", "bundle", "client":
		return true
	default:
		return false
	}
}

func policyOutcomeValid(outcome string) bool {
	switch outcome {
	case policyOutcomePass, policyOutcomeWarn, policyOutcomeFail, policyOutcomeSkip:
		return true
	default:
		return false
	}
}

func loadPolicyModeFromConfiguration(resolved map[string]any) string {
	policyValue, ok := resolved["policy"].(map[string]any)
	if !ok {
		return policyModeEnforced
	}
	mode, ok := policyValue["mode"].(string)
	if !ok || mode == "" {
		return policyModeEnforced
	}
	if !policyModeValid(mode) {
		return policyModeEnforced
	}
	return mode
}

func loadPolicyOverrideFromSource(source appliedSource, id string) policyOverride {
	result := policyOverride{}
	policiesValue, ok := source.Config["policies"].(map[string]any)
	if !ok {
		return result
	}
	overrideValue, ok := policiesValue[id].(map[string]any)
	if !ok {
		return result
	}
	if enabledValue, exists := overrideValue["enabled"]; exists {
		enabled, ok := enabledValue.(bool)
		if ok {
			result.Enabled = &enabled
		}
	}
	if enforcementValue, exists := overrideValue["enforcement"]; exists {
		enforcement, ok := enforcementValue.(string)
		if ok {
			result.Enforcement = enforcement
		}
	}
	return result
}

func applyPolicyOverrides(policy policyDefinition, context resolvedContext) (bool, string, error) {
	enabled := defaultPolicyEnabled(policy.Enabled)
	enforcement := defaultPolicyEnforcement(policy.Enforcement)
	orderedTypes := []string{sourceTypeGlobal, sourceTypeProfile, sourceTypeCLIProfile, sourceTypeMachine, sourceTypeProject}
	for _, sourceType := range orderedTypes {
		for _, source := range context.Sources {
			if source.Type != sourceType || source.ParseError != nil {
				continue
			}
			override := loadPolicyOverrideFromSource(source, policy.Identifier)
			if override.Enabled != nil {
				enabled = *override.Enabled
			}
			if override.Enforcement != "" {
				if !policyModeValid(override.Enforcement) {
					return false, "", policyError{Code: policyCodeOverrideInvalid, Message: fmt.Sprintf("invalid enforcement override for policy %s", policy.Identifier)}
				}
				enforcement = override.Enforcement
			}
		}
	}
	return enabled, enforcement, nil
}

func resolvePathValue(model map[string]any, path string) (any, bool) {
	if path == "" {
		return model, true
	}
	segments := strings.Split(path, ".")
	var current any = model
	for _, segment := range segments {
		if segment == "" {
			return nil, false
		}
		switch typed := current.(type) {
		case map[string]any:
			next, exists := typed[segment]
			if !exists {
				return nil, false
			}
			current = next
		default:
			return nil, false
		}
	}
	return current, true
}

func anyToFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func evaluatePolicyCondition(condition map[string]any, model map[string]any) (bool, error) {
	opRaw, exists := condition["op"]
	if !exists {
		return false, policyError{Code: policyCodeConditionInvalid, Message: "condition op is required"}
	}
	op, ok := opRaw.(string)
	if !ok || op == "" {
		return false, policyError{Code: policyCodeConditionInvalid, Message: "condition op must be a string"}
	}

	switch op {
	case "and", "or":
		entriesRaw, ok := condition["conditions"].([]any)
		if !ok || len(entriesRaw) == 0 {
			return false, policyError{Code: policyCodeConditionInvalid, Message: "logical condition requires conditions array"}
		}
		results := make([]bool, 0, len(entriesRaw))
		for _, entry := range entriesRaw {
			entryCondition, ok := entry.(map[string]any)
			if !ok {
				return false, policyError{Code: policyCodeConditionInvalid, Message: "condition entry must be an object"}
			}
			result, err := evaluatePolicyCondition(entryCondition, model)
			if err != nil {
				return false, err
			}
			results = append(results, result)
		}
		if op == "and" {
			for _, result := range results {
				if !result {
					return false, nil
				}
			}
			return true, nil
		}
		for _, result := range results {
			if result {
				return true, nil
			}
		}
		return false, nil
	case "not":
		nested, ok := condition["condition"].(map[string]any)
		if !ok {
			return false, policyError{Code: policyCodeConditionInvalid, Message: "not condition requires condition object"}
		}
		result, err := evaluatePolicyCondition(nested, model)
		if err != nil {
			return false, err
		}
		return !result, nil
	}

	pathRaw, ok := condition["path"].(string)
	if !ok {
		return false, policyError{Code: policyCodeConditionInvalid, Message: "condition path must be a string"}
	}
	left, exists := resolvePathValue(model, pathRaw)
	right := condition["value"]

	switch op {
	case "exists":
		return exists, nil
	case "missing":
		return !exists, nil
	case "equals":
		if !exists {
			return false, nil
		}
		return jsonValueEqual(left, right), nil
	case "not_equals":
		if !exists {
			return true, nil
		}
		return !jsonValueEqual(left, right), nil
	case "contains":
		if !exists {
			return false, nil
		}
		return valueContains(left, right), nil
	case "not_contains":
		if !exists {
			return true, nil
		}
		return !valueContains(left, right), nil
	case "greater_than":
		if !exists {
			return false, nil
		}
		leftNumber, leftOK := anyToFloat(left)
		rightNumber, rightOK := anyToFloat(right)
		if !leftOK || !rightOK {
			return false, policyError{Code: policyCodeConditionInvalid, Message: "greater_than requires numeric values"}
		}
		return leftNumber > rightNumber, nil
	case "less_than":
		if !exists {
			return false, nil
		}
		leftNumber, leftOK := anyToFloat(left)
		rightNumber, rightOK := anyToFloat(right)
		if !leftOK || !rightOK {
			return false, policyError{Code: policyCodeConditionInvalid, Message: "less_than requires numeric values"}
		}
		return leftNumber < rightNumber, nil
	case "regex_match":
		if !exists {
			return false, nil
		}
		text, ok := left.(string)
		if !ok {
			return false, policyError{Code: policyCodeConditionInvalid, Message: "regex_match requires string left-hand value"}
		}
		pattern, ok := right.(string)
		if !ok {
			return false, policyError{Code: policyCodeConditionInvalid, Message: "regex_match requires string pattern"}
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return false, policyError{Code: policyCodeConditionInvalid, Message: "regex pattern is invalid"}
		}
		return compiled.MatchString(text), nil
	default:
		return false, policyError{Code: policyCodeConditionInvalid, Message: fmt.Sprintf("unsupported condition operator %q", op)}
	}
}

func jsonValueEqual(left any, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(leftJSON) == string(rightJSON)
}

func valueContains(left any, right any) bool {
	switch typed := left.(type) {
	case string:
		needle, ok := right.(string)
		if !ok {
			return false
		}
		return strings.Contains(typed, needle)
	case []any:
		for _, entry := range typed {
			if jsonValueEqual(entry, right) {
				return true
			}
		}
		return false
	case map[string]any:
		key, ok := right.(string)
		if !ok {
			return false
		}
		_, exists := typed[key]
		return exists
	default:
		return false
	}
}

func policyModel(resolved map[string]any, context resolvedContext, operation string, bundleMetadata map[string]any) map[string]any {
	model := map[string]any{}
	for key, value := range resolved {
		model[key] = value
	}
	model["operation"] = operation
	model["machine"] = map[string]any{"id": context.MachineIdentifier, "path": context.MachineOverlayPath, "exists": context.MachineOverlayExists}
	activeProfiles := []string{}
	for _, profile := range context.ActiveProfiles {
		activeProfiles = append(activeProfiles, profile.Identifier)
	}
	sort.Strings(activeProfiles)
	model["profiles"] = activeProfiles
	if bundleMetadata != nil {
		model["bundle"] = bundleMetadata
	}
	if _, exists := model["plugins"]; !exists {
		model["plugins"] = map[string]any{}
	}
	return model
}

func policyAppliesToOperation(scopes []string, operation string) bool {
	if len(scopes) == 0 {
		return true
	}
	opScope := map[string]string{
		"validate":        "global",
		"doctor":          "global",
		"export":          "bundle",
		"import":          "bundle",
		"bundle_verify":   "bundle",
		"bundle_decrypt":  "bundle",
		"client_generate": "client",
		"client_validate": "client",
		"env":             "global",
		"mcp_resolve":     "global",
	}
	required := opScope[operation]
	for _, scope := range scopes {
		if scope == required || scope == "global" {
			return true
		}
	}
	return false
}

func evaluatePolicies(paths Paths, evalContext policyEvaluationContext, selectedID string, explicitMode string) (policyReport, error) {
	mode := explicitMode
	if mode == "" {
		mode = loadPolicyModeFromConfiguration(evalContext.Resolved)
	}
	if !policyModeValid(mode) {
		return policyReport{}, policyError{Code: policyCodeInvalid, Message: fmt.Sprintf("invalid policy mode %q", mode)}
	}
	report := policyReport{Mode: mode, Operation: evalContext.Operation, Results: []policyResult{}, Summary: policySummary{}}
	if mode == policyModeDisabled {
		report.Summary.Skipped = 0
		return report, nil
	}

	policies, _, err := discoverPolicies(paths)
	if err != nil {
		return report, err
	}
	if selectedID != "" {
		filtered := []policyDefinition{}
		for _, policy := range policies {
			if policy.Identifier == selectedID {
				filtered = append(filtered, policy)
			}
		}
		if len(filtered) == 0 {
			return report, policyError{Code: policyCodeNotFound, Message: fmt.Sprintf("policy %q not found", selectedID)}
		}
		policies = filtered
	}

	model := policyModel(evalContext.Resolved, evalContext.ResolvedContext, evalContext.Operation, evalContext.BundleMetadata)
	for _, policy := range policies {
		enabled, enforcement, err := applyPolicyOverrides(policy, evalContext.ResolvedContext)
		if err != nil {
			return report, err
		}
		result := policyResult{
			PolicyID:     policy.Identifier,
			Title:        policy.Title,
			Severity:     policy.Severity,
			Scope:        append([]string{}, policy.Scopes...),
			Enabled:      enabled,
			Enforcement:  enforcement,
			Outcome:      policyOutcomeSkip,
			Findings:     []policyFinding{},
			EvaluatedAt:  time.Now().UTC().Format(time.RFC3339),
			Explanation:  policy.Description,
			PolicySource: policy.Path,
		}
		if !enabled {
			result.Outcome = policyOutcomeSkip
			report.Results = append(report.Results, result)
			continue
		}
		if !policyAppliesToOperation(policy.Scopes, evalContext.Operation) {
			result.Outcome = policyOutcomeSkip
			report.Results = append(report.Results, result)
			continue
		}
		match, err := evaluatePolicyCondition(policy.Condition, model)
		if err != nil {
			result.Outcome = policyOutcomeFail
			conditionPath := toConditionPath(policy.Condition)
			result.Findings = append(result.Findings, policyFinding{
				PolicyID:   policy.Identifier,
				Severity:   policy.Severity,
				Outcome:    policyOutcomeFail,
				Path:       conditionPath,
				Provenance: policyFieldProvenance(evalContext.ResolvedContext, conditionPath),
				Code:       policyCodeConditionInvalid,
				Message:    err.Error(),
			})
			report.Results = append(report.Results, result)
			continue
		}
		if match {
			result.Outcome = policyOutcomePass
			report.Results = append(report.Results, result)
			continue
		}
		outcome := policy.FailureState
		if outcome == "" {
			outcome = policyOutcomeFail
		}
		result.Outcome = outcome
		failureCode := policy.FailureCode
		if failureCode == "" {
			failureCode = policyCodeEnforcementFailed
		}
		message := policy.Message
		if message == "" {
			message = "policy condition was not satisfied"
		}
		conditionPath := toConditionPath(policy.Condition)
		result.Findings = append(result.Findings, policyFinding{
			PolicyID:   policy.Identifier,
			Severity:   policy.Severity,
			Outcome:    outcome,
			Path:       conditionPath,
			Provenance: policyFieldProvenance(evalContext.ResolvedContext, conditionPath),
			Code:       failureCode,
			Message:    message,
		})
		report.Results = append(report.Results, result)
	}

	sort.Slice(report.Results, func(i, j int) bool { return report.Results[i].PolicyID < report.Results[j].PolicyID })
	report.Summary = buildPolicySummary(report.Results)
	if mode == policyModeEnforced {
		for _, result := range report.Results {
			if result.Outcome != policyOutcomeFail {
				continue
			}
			if result.Enforcement != policyModeEnforced {
				continue
			}
			return report, policyError{Code: policyCodeOperationBlocked, Message: fmt.Sprintf("policy %s blocked operation %s", result.PolicyID, evalContext.Operation)}
		}
	}
	return report, nil
}

func toConditionPath(condition map[string]any) string {
	path, ok := condition["path"].(string)
	if !ok {
		return "$"
	}
	if path == "" {
		return "$"
	}
	return path
}

func policyFieldProvenance(ctx resolvedContext, fieldPath string) string {
	if fieldPath == "" || fieldPath == "$" {
		return "resolved"
	}
	for index := len(ctx.Sources) - 1; index >= 0; index-- {
		source := ctx.Sources[index]
		if source.ParseError != nil {
			continue
		}
		if _, exists := fieldPathValue(source.Config, fieldPath); !exists {
			continue
		}
		return fmt.Sprintf("%s:%s:%s", source.Type, source.Identifier, source.Path)
	}
	return "resolved"
}

func buildPolicySummary(results []policyResult) policySummary {
	summary := policySummary{}
	for _, result := range results {
		switch result.Outcome {
		case policyOutcomePass:
			summary.Passed++
		case policyOutcomeWarn:
			summary.Warned++
		case policyOutcomeFail:
			summary.Failed++
		case policyOutcomeSkip:
			summary.Skipped++
		}
	}
	total := summary.Passed + summary.Warned + summary.Failed
	if total > 0 {
		summary.CompliancePercentage = float64(summary.Passed+summary.Warned) * 100.0 / float64(total)
	}
	return summary
}

func persistPolicyReport(paths Paths, report policyReport) {
	statePath := filepath.Join(paths.StateHome, "policy-last-report.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return
	}
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(statePath, content, 0o600)
}

func readLastPolicyReport(paths Paths) (policyReport, error) {
	statePath := filepath.Join(paths.StateHome, "policy-last-report.json")
	if !fileExists(statePath) {
		return policyReport{}, policyError{Code: policyCodeNotFound, Message: "policy report not found"}
	}
	content, err := os.ReadFile(statePath)
	if err != nil {
		return policyReport{}, err
	}
	report := policyReport{}
	if err := json.Unmarshal(content, &report); err != nil {
		return policyReport{}, err
	}
	return report, nil
}

func printPolicyReport(report policyReport, jsonOutput bool) error {
	if jsonOutput {
		content, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(content))
		return nil
	}
	fmt.Printf("operation=%s mode=%s\n", report.Operation, report.Mode)
	for _, result := range report.Results {
		fmt.Printf("policy=%s enabled=%t enforcement=%s outcome=%s severity=%s\n", result.PolicyID, result.Enabled, result.Enforcement, result.Outcome, result.Severity)
		for _, finding := range result.Findings {
			fmt.Printf("  finding code=%s path=%s provenance=%s message=%s\n", finding.Code, finding.Path, finding.Provenance, finding.Message)
		}
	}
	fmt.Printf("summary passed=%d warned=%d failed=%d skipped=%d compliance=%.2f\n", report.Summary.Passed, report.Summary.Warned, report.Summary.Failed, report.Summary.Skipped, report.Summary.CompliancePercentage)
	return nil
}

func policyCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "policy requires a subcommand"}
	}
	switch arguments[0] {
	case "list":
		return policyListCommand(paths, arguments[1:])
	case "show":
		return policyShowCommand(paths, arguments[1:])
	case "explain":
		return policyExplainCommand(paths, arguments[1:])
	case "evaluate":
		return policyEvaluateCommand(paths, arguments[1:])
	case "report":
		return policyReportCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown policy subcommand: %s", arguments[0])}
	}
}

func policyListCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown policy list option: %s", argument)}
		}
	}
	policies, _, err := discoverPolicies(paths)
	if err != nil {
		return err
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].Identifier < policies[j].Identifier })
	if jsonOutput {
		content, err := json.MarshalIndent(map[string]any{"policies": policies}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(content))
		return nil
	}
	for _, policy := range policies {
		fmt.Printf("id=%s title=%s enabled=%t enforcement=%s severity=%s scope=%s\n", policy.Identifier, policy.Title, defaultPolicyEnabled(policy.Enabled), defaultPolicyEnforcement(policy.Enforcement), policy.Severity, strings.Join(policy.Scopes, ","))
	}
	return nil
}

func policyByID(paths Paths, id string) (policyDefinition, error) {
	policies, _, err := discoverPolicies(paths)
	if err != nil {
		return policyDefinition{}, err
	}
	for _, policy := range policies {
		if policy.Identifier == id {
			return policy, nil
		}
	}
	return policyDefinition{}, policyError{Code: policyCodeNotFound, Message: fmt.Sprintf("policy %q not found", id)}
}

func policyShowCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "policy show requires a policy identifier"}
	}
	policyID := arguments[0]
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown policy show option: %s", argument)}
		}
	}
	policy, err := policyByID(paths, policyID)
	if err != nil {
		return err
	}
	content, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func policyExplainCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "policy explain requires a policy identifier"}
	}
	policy, err := policyByID(paths, arguments[0])
	if err != nil {
		return err
	}
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	resolvedCtx, err := resolveConfigurationWithContext(paths, info, true)
	if err != nil {
		return err
	}
	report, evalErr := evaluatePolicies(paths, policyEvaluationContext{Resolved: resolvedCtx.Resolved, ResolvedContext: resolvedCtx, Operation: "validate"}, policy.Identifier, "")
	if evalErr != nil {
		var blocked policyError
		if !errors.As(evalErr, &blocked) || blocked.Code != policyCodeOperationBlocked {
			return evalErr
		}
	}
	result := map[string]any{
		"policy":            policy,
		"evaluation_logic":  policy.Condition,
		"applicable_scopes": policy.Scopes,
		"recent_evaluation": report.Results,
		"recent_compliance": report.Summary,
	}
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(content))
	return nil
}

func policyEvaluateCommand(paths Paths, arguments []string) error {
	policyID := ""
	jsonOutput := false
	explicitMode := ""
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--json":
			jsonOutput = true
		case "--policy-mode":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--policy-mode requires a value"}
			}
			index++
			explicitMode = arguments[index]
		default:
			if strings.HasPrefix(argument, "--") {
				return UsageError{Message: fmt.Sprintf("unknown policy evaluate option: %s", argument)}
			}
			if policyID != "" {
				return UsageError{Message: "policy evaluate accepts at most one policy identifier"}
			}
			policyID = argument
		}
	}
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	resolvedCtx, err := resolveConfigurationWithContext(paths, info, true)
	if err != nil {
		return err
	}
	report, evalErr := evaluatePolicies(paths, policyEvaluationContext{Resolved: resolvedCtx.Resolved, ResolvedContext: resolvedCtx, Operation: "validate"}, policyID, explicitMode)
	persistPolicyReport(paths, report)
	if err := printPolicyReport(report, jsonOutput); err != nil {
		return err
	}
	if evalErr != nil {
		return evalErr
	}
	return nil
}

func policyReportCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown policy report option: %s", argument)}
		}
	}
	report, err := readLastPolicyReport(paths)
	if err != nil {
		return err
	}
	return printPolicyReport(report, jsonOutput)
}

func parsePolicyModeFlag(arguments []string) (string, []string, error) {
	cleaned := []string{}
	mode := ""
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument != "--policy-mode" {
			cleaned = append(cleaned, argument)
			continue
		}
		if index+1 >= len(arguments) {
			return "", nil, UsageError{Message: "--policy-mode requires a value"}
		}
		index++
		mode = arguments[index]
		if !policyModeValid(mode) {
			return "", nil, UsageError{Message: fmt.Sprintf("invalid policy mode: %s", mode)}
		}
	}
	return mode, cleaned, nil
}

func operationUsesPolicy(command string, arguments []string) (string, bool) {
	switch command {
	case "validate":
		return "validate", true
	case "doctor":
		return "doctor", true
	case "export":
		return "export", true
	case "import":
		return "import", true
	case "env":
		return "env", true
	case "mcp":
		if len(arguments) > 0 && arguments[0] == "resolve" {
			return "mcp_resolve", true
		}
	case "client":
		if len(arguments) > 0 && arguments[0] == "generate" {
			return "client_generate", true
		}
		if len(arguments) > 0 && arguments[0] == "validate" {
			return "client_validate", true
		}
	case "bundle":
		if len(arguments) > 0 && arguments[0] == "verify" {
			return "bundle_verify", true
		}
		if len(arguments) > 0 && arguments[0] == "decrypt" {
			return "bundle_decrypt", true
		}
	}
	return "", false
}

func evaluatePoliciesForOperation(paths Paths, operation string, mode string, command string, commandArgs []string) (policyReport, error) {
	if mode == policyModeDisabled {
		return policyReport{Mode: mode, Operation: operation, Results: []policyResult{}, Summary: policySummary{}}, nil
	}
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return policyReport{}, err
	}
	resolvedCtx, err := resolveConfigurationWithContext(paths, info, true)
	if err != nil {
		return policyReport{}, err
	}
	bundleMeta := map[string]any{}
	if command == "import" || command == "bundle" {
		path := ""
		if command == "import" && len(commandArgs) > 0 {
			path = commandArgs[0]
		}
		if command == "bundle" && len(commandArgs) > 1 {
			subcommand := commandArgs[0]
			if subcommand == "verify" || subcommand == "decrypt" {
				path = commandArgs[1]
			}
		}
		if path != "" {
			archive, readErr := readBundleArchive(path)
			if readErr == nil {
				bundleMeta["path"] = path
				bundleMeta["encrypted"] = archive.Security != nil && archive.Security.Encrypted
				if archive.Security != nil {
					bundleMeta["security_version"] = archive.Security.Version
					bundleMeta["signature_count"] = len(archive.Security.Signatures)
				}
				if archive.Manifest.Schema != "" {
					bundleMeta["schema"] = archive.Manifest.Schema
				}
			}
		}
	}
	report, evalErr := evaluatePolicies(paths, policyEvaluationContext{Resolved: resolvedCtx.Resolved, ResolvedContext: resolvedCtx, Operation: operation, BundleMetadata: bundleMeta}, "", mode)

	pluginFindings := pluginValidatorFindings(paths, resolvedCtx.Resolved)
	for _, finding := range pluginFindings {
		report.PluginIssues = append(report.PluginIssues, policyFinding{
			PolicyID:   "plugin/" + finding.Path,
			Severity:   finding.Severity,
			Outcome:    policyOutcomeWarn,
			Path:       finding.Path,
			Provenance: finding.Source,
			Code:       policyCodePluginFailed,
			Message:    finding.Message,
		})
	}
	persistPolicyReport(paths, report)
	if evalErr != nil {
		return report, evalErr
	}
	return report, nil
}

func policyDoctorLines(paths Paths) []string {
	lines := []string{}
	policies, warnings, err := discoverPolicies(paths)
	if err != nil {
		lines = append(lines, fmt.Sprintf("[error] policies: %v", err))
		return lines
	}
	lines = append(lines, fmt.Sprintf("[ok] policy registry: path=%s policies=%d", policyRegistryPath(paths), len(policies)))
	for _, warning := range warnings {
		lines = append(lines, fmt.Sprintf("[notice] policy metadata warning: source=%s path=%s code=%s", warning.Source, warning.Path, warning.Code))
	}
	report, err := readLastPolicyReport(paths)
	if err == nil {
		lines = append(lines, fmt.Sprintf("[ok] policy last_report: mode=%s operation=%s failed=%d warned=%d passed=%d skipped=%d compliance=%.2f", report.Mode, report.Operation, report.Summary.Failed, report.Summary.Warned, report.Summary.Passed, report.Summary.Skipped, report.Summary.CompliancePercentage))
	}
	return lines
}
