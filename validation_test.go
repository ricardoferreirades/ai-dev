package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestValidateConfigurationForProjectWarningsAndStrict(t *testing.T) {
	paths := Paths{
		ConfigHome: t.TempDir(),
		DataHome:   t.TempDir(),
		StateHome:  t.TempDir(),
	}
	if err := os.MkdirAll(filepath.Join(paths.ConfigHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}

	global := filepath.Join(paths.ConfigHome, "global.toml")
	if err := os.WriteFile(global, []byte("[environment]\nFOO = \"bar\"\n"), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	info := ProjectInfo{ProjectID: "example.com/acme/app"}
	report, err := validateConfigurationForProject(paths, info, false)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}

	if !report.Valid {
		t.Fatalf("expected valid report in non-strict mode, got invalid: %+v", report)
	}
	if len(report.Warnings) == 0 {
		t.Fatalf("expected missing schema warning")
	}
	if report.Warnings[0].Code != validationCodeMissingSchema {
		t.Fatalf("expected warning code %s, got %s", validationCodeMissingSchema, report.Warnings[0].Code)
	}

	strictReport, err := validateConfigurationForProject(paths, info, true)
	if err != nil {
		t.Fatalf("validate strict config: %v", err)
	}
	if strictReport.Valid {
		t.Fatalf("expected strict report to fail on warning")
	}
}

func TestValidateConfigurationDetectsUnknownAndTypeErrors(t *testing.T) {
	paths := Paths{
		ConfigHome: t.TempDir(),
		DataHome:   t.TempDir(),
		StateHome:  t.TempDir(),
	}
	if err := os.MkdirAll(filepath.Join(paths.ConfigHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}

	content := strings.Join([]string{
		"schema = \"v1\"",
		"unknown_top = true",
		"[mcp]",
		"servers = [\"ok\", 1]",
		"servres = []",
		"[prompts]",
		"default = 1",
		"extra = \"x\"",
		"[rules]",
		"enabled = \"bad\"",
		"[environment]",
		"INVALID-NAME = \"x\"",
		"GOOD = [1]",
	}, "\n")
	global := filepath.Join(paths.ConfigHome, "global.toml")
	if err := os.WriteFile(global, []byte(content), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	report, err := validateConfigurationForProject(paths, ProjectInfo{ProjectID: "pid"}, false)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}

	if report.Valid {
		t.Fatalf("expected invalid report")
	}

	expectedCodes := map[string]bool{
		validationCodeUnknownField:           false,
		validationCodeInvalidType:            false,
		validationCodeInvalidEnvironmentName: false,
		validationCodeInvalidEnvironmentVal:  false,
	}
	for _, finding := range report.Errors {
		if _, ok := expectedCodes[finding.Code]; ok {
			expectedCodes[finding.Code] = true
		}
	}
	for code, seen := range expectedCodes {
		if !seen {
			t.Fatalf("expected finding code %s in errors: %+v", code, report.Errors)
		}
	}
}

func TestValidateConfigurationClientsNamespace(t *testing.T) {
	paths := Paths{
		ConfigHome: t.TempDir(),
		DataHome:   t.TempDir(),
		StateHome:  t.TempDir(),
	}
	if err := os.MkdirAll(filepath.Join(paths.ConfigHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}

	global := filepath.Join(paths.ConfigHome, "global.toml")
	if err := os.WriteFile(
		global,
		[]byte(strings.Join([]string{
			"schema = \"v1\"",
			"[clients.codex]",
			"enabled = true",
			"[clients.unknown]",
			"enabled = true",
			"[clients.cursor]",
			"enabled = \"yes\"",
			"format = \"json\"",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	report, err := validateConfigurationForProject(paths, ProjectInfo{ProjectID: "pid"}, false)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if report.Valid {
		t.Fatalf("expected clients namespace validation to fail")
	}

	seen := map[string]bool{}
	for _, finding := range report.Errors {
		if finding.Path == "clients.unknown" && finding.Code == validationCodeUnknownField {
			seen["unknown_client"] = true
		}
		if finding.Path == "clients.cursor.enabled" && finding.Code == validationCodeInvalidType {
			seen["enabled_type"] = true
		}
		if finding.Path == "clients.cursor.format" && finding.Code == validationCodeUnknownField {
			seen["unknown_field"] = true
		}
	}
	for _, key := range []string{"unknown_client", "enabled_type", "unknown_field"} {
		if !seen[key] {
			t.Fatalf("missing expected clients validation finding %s: %+v", key, report.Errors)
		}
	}
}

func TestValidateConfigurationRejectsUnsupportedSchema(t *testing.T) {
	paths := Paths{
		ConfigHome: t.TempDir(),
		DataHome:   t.TempDir(),
		StateHome:  t.TempDir(),
	}
	if err := os.MkdirAll(filepath.Join(paths.ConfigHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(paths.ConfigHome, "global.toml"),
		[]byte("schema = \"v2\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	report, err := validateConfigurationForProject(paths, ProjectInfo{ProjectID: "pid"}, false)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}

	if report.Valid {
		t.Fatalf("expected invalid report")
	}

	foundUnsupported := false
	for _, finding := range report.Errors {
		if finding.Code == validationCodeUnsupportedSchema {
			foundUnsupported = true
			break
		}
	}
	if !foundUnsupported {
		t.Fatalf("expected unsupported schema error, got %+v", report.Errors)
	}
}

func TestSchemaV1KnownFieldTypes(t *testing.T) {
	tests := []struct {
		name          string
		configuration map[string]any
		expectedPath  string
	}{
		{
			name:          "schema",
			configuration: map[string]any{"schema": int64(1)},
			expectedPath:  "schema",
		},
		{
			name:          "name",
			configuration: map[string]any{"schema": "v1", "name": true},
			expectedPath:  "name",
		},
		{
			name:          "profile",
			configuration: map[string]any{"schema": "v1", "profile": int64(1)},
			expectedPath:  "profile",
		},
		{
			name:          "environment",
			configuration: map[string]any{"schema": "v1", "environment": "bad"},
			expectedPath:  "environment",
		},
		{
			name:          "mcp",
			configuration: map[string]any{"schema": "v1", "mcp": []any{}},
			expectedPath:  "mcp",
		},
		{
			name:          "prompts",
			configuration: map[string]any{"schema": "v1", "prompts": "bad"},
			expectedPath:  "prompts",
		},
		{
			name: "prompts project",
			configuration: map[string]any{
				"schema":  "v1",
				"prompts": map[string]any{"project": false},
			},
			expectedPath: "prompts.project",
		},
		{
			name:          "rules",
			configuration: map[string]any{"schema": "v1", "rules": "bad"},
			expectedPath:  "rules",
		},
		{
			name:          "plugins",
			configuration: map[string]any{"schema": "v1", "plugins": "bad"},
			expectedPath:  "plugins",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			findings := validateConfigurationDocument("/config.toml", test.configuration)
			found := false
			for _, finding := range findings {
				if finding.Path == test.expectedPath &&
					finding.Code == validationCodeInvalidType {
					found = true
				}
			}
			if !found {
				t.Fatalf(
					"expected invalid_type at %s, got %+v",
					test.expectedPath,
					findings,
				)
			}
		})
	}
}

func TestValidateCLIJSONStrictAndExitCodes(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}

	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()

	if err := os.MkdirAll(filepath.Join(configHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte("[environment]\nFOO = \"bar\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	binary := filepath.Join(t.TempDir(), "ai-dev")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, ".")
	build.Dir = workspace
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, output)
	}

	baseEnv := append(
		os.Environ(),
		"AI_DEV_CONFIG_HOME="+configHome,
		"AI_DEV_DATA_HOME="+dataHome,
		"AI_DEV_STATE_HOME="+stateHome,
	)

	validateJSON := exec.Command(binary, "validate", "--json")
	validateJSON.Dir = repo
	validateJSON.Env = baseEnv
	output, err := validateJSON.CombinedOutput()
	if err != nil {
		t.Fatalf("validate --json should pass in non-strict mode: %v\n%s", err, output)
	}

	var payload ValidationReport
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("parse validate json: %v\n%s", err, output)
	}
	if !payload.Valid {
		t.Fatalf("expected valid=true in non-strict mode, got %+v", payload)
	}
	if len(payload.Warnings) == 0 || payload.Warnings[0].Severity != "warning" {
		t.Fatalf("expected warning findings in json output: %+v", payload.Warnings)
	}

	validateStrict := exec.Command(binary, "validate", "--strict")
	validateStrict.Dir = repo
	validateStrict.Env = baseEnv
	if output, err = validateStrict.CombinedOutput(); err == nil {
		t.Fatalf("validate --strict should fail on warning\n%s", output)
	} else {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
			t.Fatalf("validate --strict should exit 1, got %v\n%s", err, output)
		}
	}

	usage := exec.Command(binary, "validate", "--nope")
	usage.Dir = repo
	usage.Env = baseEnv
	if output, err = usage.CombinedOutput(); err == nil {
		t.Fatalf("validate unknown option should fail\n%s", output)
	} else {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
			t.Fatalf("validate unknown option should exit 2, got %v\n%s", err, output)
		}
	}
}

func TestEnvRefusesInvalidConfiguration(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}

	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()

	if err := os.MkdirAll(filepath.Join(configHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	gitInit := exec.Command("git", "init", "-q")
	gitInit.Dir = repo
	if output, err := gitInit.CombinedOutput(); err != nil {
		t.Fatalf("initialize test repository: %v\n%s", err, output)
	}
	gitRemote := exec.Command(
		"git",
		"remote",
		"add",
		"origin",
		"https://example.com/acme/app.git",
	)
	gitRemote.Dir = repo
	if output, err := gitRemote.CombinedOutput(); err != nil {
		t.Fatalf("configure test remote: %v\n%s", err, output)
	}
	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte("schema = \"v1\"\n[environment]\nGLOBAL_VALUE = \"global\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(configHome, "projects", "example.com-acme-app.toml"),
		[]byte("schema = \"v2\"\n[environment]\nPROJECT_VALUE = \"project\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write invalid project config: %v", err)
	}

	binary := filepath.Join(t.TempDir(), "ai-dev")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, ".")
	build.Dir = workspace
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, output)
	}

	command := exec.Command(binary, "env", "--shell", "sh")
	command.Dir = repo
	command.Env = append(
		os.Environ(),
		"AI_DEV_CONFIG_HOME="+configHome,
		"AI_DEV_DATA_HOME="+dataHome,
		"AI_DEV_STATE_HOME="+stateHome,
	)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("expected env command failure for invalid config; output=%s", output)
	}

	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		t.Fatalf("env should exit 1, got %v", err)
	}

	if strings.Contains(string(output), "export ") {
		t.Fatalf("env command must not print exports on validation failure: %s", output)
	}
}

func TestValidationFindingsOrderingIsDeterministic(t *testing.T) {
	findings := []ValidationFinding{
		{Source: "/b", Path: "z", Code: "unknown_field", Message: "m3", Severity: "error"},
		{Source: "/a", Path: "z", Code: "unknown_field", Message: "m1", Severity: "error"},
		{Source: "/a", Path: "a", Code: "unknown_field", Message: "m2", Severity: "error"},
	}

	sortValidationFindings(findings)

	if !sort.SliceIsSorted(findings, func(i, j int) bool {
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
	}) {
		t.Fatalf("findings are not deterministically sorted: %+v", findings)
	}
}

func TestValidationWithNoConfigurationIsValid(t *testing.T) {
	paths := Paths{
		ConfigHome: t.TempDir(),
		DataHome:   t.TempDir(),
		StateHome:  t.TempDir(),
	}

	report, err := validateConfigurationForProject(
		paths,
		ProjectInfo{ProjectID: "example.com/acme/app"},
		true,
	)
	if err != nil {
		t.Fatalf("validate empty configuration: %v", err)
	}

	if !report.Valid {
		t.Fatalf("empty optional configuration should be valid: %+v", report)
	}
	if len(report.Errors) != 0 || len(report.Warnings) != 0 || len(report.Sources) != 0 {
		t.Fatalf("empty report should contain empty arrays: %+v", report)
	}

	content, err := validationOutputJSON(report)
	if err != nil {
		t.Fatalf("encode empty validation report: %v", err)
	}
	for _, fragment := range []string{
		`"errors": []`,
		`"warnings": []`,
		`"sources": []`,
	} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("expected %s in JSON output:\n%s", fragment, content)
		}
	}
}

func TestValidationTracksSourcePrecedenceAndValidatesProjectIndependently(t *testing.T) {
	paths := Paths{
		ConfigHome: t.TempDir(),
		DataHome:   t.TempDir(),
		StateHome:  t.TempDir(),
	}
	info := ProjectInfo{ProjectID: "example.com/acme/app"}
	projectPath := projectConfigPath(paths, info.ProjectID)
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatalf("create projects directory: %v", err)
	}

	globalPath := filepath.Join(paths.ConfigHome, "global.toml")
	if err := os.WriteFile(
		globalPath,
		[]byte("schema = \"v1\"\n[environment]\nSHARED = \"global\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	if err := os.WriteFile(
		projectPath,
		[]byte("schema = \"v1\"\n[environment]\nSHARED = \"project\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	report, err := validateConfigurationForProject(paths, info, false)
	if err != nil {
		t.Fatalf("validate layered configuration: %v", err)
	}
	if !report.Valid || len(report.Errors) != 0 || len(report.Warnings) != 0 {
		t.Fatalf("expected valid layered configuration: %+v", report)
	}

	expectedSources := []string{globalPath, projectPath}
	if !reflect.DeepEqual(report.Sources, expectedSources) {
		t.Fatalf("expected source precedence %v, got %v", expectedSources, report.Sources)
	}

	if err := os.WriteFile(
		projectPath,
		[]byte("schema = \"v1\"\n[mcp]\nservres = []\n"),
		0o600,
	); err != nil {
		t.Fatalf("write invalid project config: %v", err)
	}

	report, err = validateConfigurationForProject(paths, info, false)
	if err != nil {
		t.Fatalf("validate invalid project config: %v", err)
	}

	foundProjectError := false
	for _, finding := range report.Errors {
		if finding.Source == projectPath &&
			finding.Path == "mcp.servres" &&
			finding.Code == validationCodeUnknownField {
			foundProjectError = true
		}
	}
	if !foundProjectError {
		t.Fatalf("expected independent project-source finding: %+v", report.Errors)
	}
}

func TestValidationDetectsResolvedShapeAndSchemaConflicts(t *testing.T) {
	paths := Paths{
		ConfigHome: t.TempDir(),
		DataHome:   t.TempDir(),
		StateHome:  t.TempDir(),
	}
	info := ProjectInfo{ProjectID: "example.com/acme/app"}
	projectPath := projectConfigPath(paths, info.ProjectID)
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o755); err != nil {
		t.Fatalf("create projects directory: %v", err)
	}

	globalPath := filepath.Join(paths.ConfigHome, "global.toml")
	if err := os.WriteFile(
		globalPath,
		[]byte("schema = \"v1\"\n[environment]\nFOO = \"bar\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	if err := os.WriteFile(
		projectPath,
		[]byte("schema = \"v2\"\nenvironment = \"not-a-table\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	report, err := validateConfigurationForProject(paths, info, false)
	if err != nil {
		t.Fatalf("validate conflicting config: %v", err)
	}

	foundConflict := false
	foundUnsupported := false
	for _, finding := range report.Errors {
		if finding.Source == resolvedValidationSource &&
			finding.Code == validationCodeConflictingValue {
			foundConflict = true
		}
		if finding.Source == projectPath &&
			finding.Code == validationCodeUnsupportedSchema {
			foundUnsupported = true
		}
	}
	if !foundConflict || !foundUnsupported {
		t.Fatalf(
			"expected resolved conflict and project unsupported schema: %+v",
			report.Errors,
		)
	}
}

func TestValidationParseErrorsDoNotExposeConfigurationValues(t *testing.T) {
	paths := Paths{
		ConfigHome: t.TempDir(),
		DataHome:   t.TempDir(),
		StateHome:  t.TempDir(),
	}
	globalPath := filepath.Join(paths.ConfigHome, "global.toml")
	const sensitiveValue = "DO_NOT_EXPOSE_THIS_VALUE"
	if err := os.WriteFile(
		globalPath,
		[]byte("schema = \"v1\"\ncredential = \""+sensitiveValue+"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write malformed config: %v", err)
	}

	report, err := validateConfigurationForProject(
		paths,
		ProjectInfo{ProjectID: "pid"},
		false,
	)
	if err != nil {
		t.Fatalf("validate malformed config: %v", err)
	}
	if report.Valid || len(report.Errors) == 0 {
		t.Fatalf("malformed TOML should fail validation: %+v", report)
	}

	content, err := validationOutputJSON(report)
	if err != nil {
		t.Fatalf("encode malformed report: %v", err)
	}
	if strings.Contains(content, sensitiveValue) {
		t.Fatalf("validation output exposed configuration value: %s", content)
	}
	if report.Errors[0].Source != globalPath ||
		report.Errors[0].Path != "$" ||
		report.Errors[0].Code != validationCodeInvalidValue {
		t.Fatalf("unexpected parse finding: %+v", report.Errors[0])
	}
}

func TestDoctorValidatesGlobalConfigurationWithoutProjectOverlay(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte("schema = \"v2\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write unsupported global config: %v", err)
	}

	command := exec.Command(binary, "doctor")
	command.Dir = repo
	command.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("doctor should fail unsupported global schema:\n%s", output)
	}
	if !strings.Contains(string(output), "unsupported schema version") {
		t.Fatalf("doctor did not classify unsupported schema:\n%s", output)
	}

	if err := os.Remove(filepath.Join(configHome, "global.toml")); err != nil {
		t.Fatalf("remove global config: %v", err)
	}
	command = exec.Command(binary, "doctor")
	command.Dir = repo
	command.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	if output, err = command.CombinedOutput(); err != nil {
		t.Fatalf("doctor should accept missing optional configs: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "configuration validation: valid") {
		t.Fatalf("doctor did not report valid empty configuration:\n%s", output)
	}
}

func TestValidateInvalidJSONOutputAndTopLevelUsageExit(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()
	repo := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte("schema = \"v1\"\nunknown = true\n"),
		0o600,
	); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	command := exec.Command(binary, "validate", "--json")
	command.Dir = repo
	command.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	if err == nil {
		t.Fatalf("validate --json should fail invalid configuration")
	}

	var report ValidationReport
	if unmarshalErr := json.Unmarshal(stdout.Bytes(), &report); unmarshalErr != nil {
		t.Fatalf("invalid JSON output: %v\nstdout=%s\nstderr=%s", unmarshalErr, stdout.String(), stderr.String())
	}
	if report.Valid || len(report.Errors) == 0 {
		t.Fatalf("expected invalid JSON report: %+v", report)
	}

	usage := exec.Command(binary, "not-a-command")
	usage.Dir = repo
	usage.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	if output, usageErr := usage.CombinedOutput(); usageErr == nil {
		t.Fatalf("unknown command should fail:\n%s", output)
	} else {
		var exitError *exec.ExitError
		if !errors.As(usageErr, &exitError) || exitError.ExitCode() != 2 {
			t.Fatalf("unknown command should exit 2, got %v\n%s", usageErr, output)
		}
	}
}

func TestLegacyEnvOutputIsUnchangedAndWarns(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()
	repo := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte("[environment]\nFOO = \"bar\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	command := exec.Command(binary, "env", "--shell", "sh")
	command.Dir = repo
	command.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("legacy env command failed: %v\n%s", err, stderr.String())
	}
	if stdout.String() != "export FOO='bar'\n" {
		t.Fatalf("legacy env stdout changed: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), validationCodeMissingSchema) {
		t.Fatalf("legacy env did not emit schema deprecation: %s", stderr.String())
	}
}

func TestSecretEnvAndCommandResolution(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}

	binary := buildValidationTestBinary(t, workspace)
	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()

	if err := os.MkdirAll(filepath.Join(configHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}

	commandFixture := filepath.Join(repo, "resolve-secret")
	if err := os.WriteFile(
		commandFixture,
		[]byte("#!/bin/sh\nprintf '%s\\n' 'postgres://user:pass@localhost/app'\n"),
		0o700,
	); err != nil {
		t.Fatalf("write command fixture: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte(strings.Join([]string{
			"schema = \"v1\"",
			"[environment]",
			"APP_ENV = \"development\"",
			"OPENAI_API_KEY = \"secret://env/OPENAI_API_KEY\"",
			"[secrets.commands.database-url]",
			"command = \"" + commandFixture + "\"",
			"args = []",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	envCommand := exec.Command(binary, "env", "--shell", "sh")
	envCommand.Dir = repo
	envCommand.Env = append(
		isolatedValidationEnvironment(configHome, dataHome, stateHome),
		"OPENAI_API_KEY=secret-value",
	)
	output, err := envCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("env with secrets should pass: %v\n%s", err, output)
	}
	text := string(output)
	for _, fragment := range []string{
		"export APP_ENV='development'",
		"export OPENAI_API_KEY='secret-value'",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("expected %s in env output:\n%s", fragment, text)
		}
	}

	resolveEnv := exec.Command(binary, "secret", "resolve", "secret://env/OPENAI_API_KEY")
	resolveEnv.Dir = repo
	resolveEnv.Env = append(
		isolatedValidationEnvironment(configHome, dataHome, stateHome),
		"OPENAI_API_KEY=secret-value",
	)
	output, err = resolveEnv.CombinedOutput()
	if err != nil {
		t.Fatalf("secret resolve env should pass: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "secret-value" {
		t.Fatalf("unexpected env secret resolution: %s", output)
	}

	resolveCommand := exec.Command(binary, "secret", "resolve", "secret://command/database-url")
	resolveCommand.Dir = repo
	resolveCommand.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	output, err = resolveCommand.CombinedOutput()
	if err != nil {
		t.Fatalf("secret resolve command should pass: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "postgres://user:pass@localhost/app" {
		t.Fatalf("unexpected command secret resolution: %s", output)
	}
}

func TestSecretCheckJSONAndDuplicateResolution(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}

	binary := buildValidationTestBinary(t, workspace)
	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()

	if err := os.MkdirAll(filepath.Join(configHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}

	counter := filepath.Join(repo, "counter")
	counterFile := filepath.Join(repo, "count.txt")
	if err := os.WriteFile(
		counter,
		[]byte("#!/bin/sh\ncount_file=$1\ncount=0\nif [ -f \"$count_file\" ]; then count=$(cat \"$count_file\"); fi\ncount=$((count + 1))\nprintf '%s' \"$count\" > \"$count_file\"\nprintf '%s\\n' \"resolved-value\"\n"),
		0o700,
	); err != nil {
		t.Fatalf("write counter fixture: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte(strings.Join([]string{
			"schema = \"v1\"",
			"[environment]",
			"FIRST = \"secret://command/counter\"",
			"SECOND = \"secret://command/counter\"",
			"[secrets.commands.counter]",
			"command = \"" + counter + "\"",
			"args = [\"" + counterFile + "\"]",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	check := exec.Command(binary, "secret", "check", "--json")
	check.Dir = repo
	check.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	output, err := check.CombinedOutput()
	if err != nil {
		t.Fatalf("secret check should pass: %v\n%s", err, output)
	}

	var payload map[string]any
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("parse secret check json: %v\n%s", err, output)
	}
	if valid, ok := payload["valid"].(bool); !ok || !valid {
		t.Fatalf("expected valid secret check: %+v", payload)
	}
	results, ok := payload["results"].([]any)
	if !ok || len(results) != 1 {
		t.Fatalf("expected duplicate references to be deduplicated: %+v", payload)
	}

	countBytes, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("read counter output: %v", err)
	}
	if strings.TrimSpace(string(countBytes)) != "1" {
		t.Fatalf("expected command provider to run once, got %s", countBytes)
	}
}

func buildValidationTestBinary(t *testing.T, workspace string) string {
	t.Helper()

	binary := filepath.Join(t.TempDir(), "ai-dev")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, ".")
	build.Dir = workspace
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, output)
	}
	return binary
}

func isolatedValidationEnvironment(
	configHome string,
	dataHome string,
	stateHome string,
) []string {
	return append(
		os.Environ(),
		"AI_DEV_CONFIG_HOME="+configHome,
		"AI_DEV_DATA_HOME="+dataHome,
		"AI_DEV_STATE_HOME="+stateHome,
	)
}
