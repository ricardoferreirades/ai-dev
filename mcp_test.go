package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPListShowAndResolve(t *testing.T) {
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

	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte(strings.Join([]string{
			"schema = \"v1\"",
			"[environment]",
			"GLOBAL_VALUE = \"global\"",
			"TOKEN = \"secret://env/MCP_TOKEN\"",
			"[mcp.servers.github]",
			"transport = \"stdio\"",
			"command = \"git\"",
			"args = [\"status\"]",
			"[mcp.servers.github.environment]",
			"LOG_LEVEL = \"debug\"",
			"[mcp.servers.remote]",
			"transport = \"http\"",
			"url = \"https://mcp.example.com\"",
			"enabled = false",
			"[mcp.servers.remote.headers]",
			"Authorization = \"secret://env/MCP_AUTH_TOKEN\"",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	projectID := filesystemProjectID(repo)
	if resolvedRepo, resolveErr := filepath.EvalSymlinks(repo); resolveErr == nil {
		projectID = filesystemProjectID(resolvedRepo)
	}
	projectConfig := filepath.Join(
		configHome,
		"projects",
		safeProjectFilename(projectID)+".toml",
	)
	if err := os.WriteFile(
		projectConfig,
		[]byte(strings.Join([]string{
			"schema = \"v1\"",
			"[mcp.servers.github]",
			"enabled = false",
			"[mcp.servers.postgres]",
			"transport = \"stdio\"",
			"command = \"printf\"",
			"args = [\"postgres\"]",
			"[mcp.servers.postgres.environment]",
			"MODE = \"readonly\"",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	env := append(
		isolatedValidationEnvironment(configHome, dataHome, stateHome),
		"MCP_TOKEN=token-value",
		"MCP_AUTH_TOKEN=auth-value",
	)

	listJSON := exec.Command(binary, "mcp", "list", "--json")
	listJSON.Dir = repo
	listJSON.Env = env
	output, err := listJSON.CombinedOutput()
	if err != nil {
		t.Fatalf("mcp list --json failed: %v\n%s", err, output)
	}

	var listPayload struct {
		Servers []MCPListEntry `json:"servers"`
	}
	if err := json.Unmarshal(output, &listPayload); err != nil {
		t.Fatalf("parse mcp list json: %v\n%s", err, output)
	}
	if len(listPayload.Servers) != 3 {
		t.Fatalf("expected 3 resolved servers, got %d", len(listPayload.Servers))
	}

	listEnabled := exec.Command(binary, "mcp", "list", "--enabled", "--json")
	listEnabled.Dir = repo
	listEnabled.Env = env
	output, err = listEnabled.CombinedOutput()
	if err != nil {
		t.Fatalf("mcp list --enabled --json failed: %v\n%s", err, output)
	}
	listPayload = struct {
		Servers []MCPListEntry `json:"servers"`
	}{}
	if err := json.Unmarshal(output, &listPayload); err != nil {
		t.Fatalf("parse enabled mcp list json: %v\n%s", err, output)
	}
	if len(listPayload.Servers) != 1 || listPayload.Servers[0].Name != "postgres" {
		t.Fatalf("expected only enabled postgres server, got %+v", listPayload.Servers)
	}

	show := exec.Command(binary, "mcp", "show", "postgres", "--json")
	show.Dir = repo
	show.Env = env
	output, err = show.CombinedOutput()
	if err != nil {
		t.Fatalf("mcp show postgres --json failed: %v\n%s", err, output)
	}

	var showPayload MCPShowEntry
	if err := json.Unmarshal(output, &showPayload); err != nil {
		t.Fatalf("parse mcp show json: %v\n%s", err, output)
	}
	if showPayload.Name != "postgres" || showPayload.Transport != "stdio" {
		t.Fatalf("unexpected show payload: %+v", showPayload)
	}
	if showPayload.Environment["TOKEN"] != "secret://env/MCP_TOKEN" {
		t.Fatalf("show output should keep unresolved secret references by default: %+v", showPayload.Environment)
	}

	resolve := exec.Command(binary, "mcp", "resolve")
	resolve.Dir = repo
	resolve.Env = env
	output, err = resolve.CombinedOutput()
	if err != nil {
		t.Fatalf("mcp resolve failed: %v\n%s", err, output)
	}
	var resolvePayload MCPResolveOutput
	if err := json.Unmarshal(output, &resolvePayload); err != nil {
		t.Fatalf("parse mcp resolve json: %v\n%s", err, output)
	}
	if len(resolvePayload.Servers) != 1 {
		t.Fatalf("resolve should include only enabled servers by default: %+v", resolvePayload.Servers)
	}
	if _, exists := resolvePayload.Servers["postgres"]; !exists {
		t.Fatalf("resolve output is missing postgres: %+v", resolvePayload.Servers)
	}

	resolveAll := exec.Command(binary, "mcp", "resolve", "--include-disabled", "--resolve-secrets")
	resolveAll.Dir = repo
	resolveAll.Env = env
	output, err = resolveAll.CombinedOutput()
	if err != nil {
		t.Fatalf("mcp resolve --include-disabled --resolve-secrets failed: %v\n%s", err, output)
	}
	resolvePayload = MCPResolveOutput{}
	if err := json.Unmarshal(output, &resolvePayload); err != nil {
		t.Fatalf("parse resolved secrets json: %v\n%s", err, output)
	}
	if resolvePayload.Servers["postgres"].Environment["TOKEN"] != "token-value" {
		t.Fatalf("expected resolved token in stdio environment: %+v", resolvePayload.Servers["postgres"].Environment)
	}
	if resolvePayload.Servers["remote"].Headers["Authorization"] != "auth-value" {
		t.Fatalf("expected resolved auth header: %+v", resolvePayload.Servers["remote"].Headers)
	}

	resolveFailure := exec.Command(binary, "mcp", "resolve", "--include-disabled", "--resolve-secrets")
	resolveFailure.Dir = repo
	resolveFailure.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	resolveFailure.Stdout = &stdout
	resolveFailure.Stderr = &stderr
	err = resolveFailure.Run()
	if err == nil {
		t.Fatalf("resolve should fail when secrets are missing")
	}
	if stdout.Len() != 0 {
		t.Fatalf("resolve --resolve-secrets must be atomic and print no partial JSON: %s", stdout.String())
	}
}

func TestMCPShowMissingServerFails(t *testing.T) {
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
	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte("schema = \"v1\"\n[mcp.servers.github]\ntransport = \"stdio\"\ncommand = \"git\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	command := exec.Command(binary, "mcp", "show", "does-not-exist")
	command.Dir = repo
	command.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("mcp show should fail for unknown server")
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		t.Fatalf("mcp show should exit 1, got %v", err)
	}
	if !strings.Contains(string(output), mcpCodeServerNotFound) {
		t.Fatalf("missing stable error code in output: %s", output)
	}
}

func TestMCPCheckJSONIsSafe(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()
	fixture := filepath.Join(repo, "ok-mcp")

	if err := os.WriteFile(fixture, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write fixture executable: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(configHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte(strings.Join([]string{
			"schema = \"v1\"",
			"[mcp.servers.ok]",
			"transport = \"stdio\"",
			"command = \"" + fixture + "\"",
			"cwd = \"" + repo + "\"",
			"[mcp.servers.ok.environment]",
			"TOKEN = \"secret://env/CHECK_SECRET\"",
			"[mcp.servers.badcmd]",
			"transport = \"stdio\"",
			"command = \"definitely-not-found-command\"",
			"[mcp.servers.badcwd]",
			"transport = \"stdio\"",
			"command = \"" + fixture + "\"",
			"cwd = \"/path/that/does/not/exist\"",
			"[mcp.servers.http]",
			"transport = \"http\"",
			"url = \"https://example.com/mcp\"",
			"[mcp.servers.http.headers]",
			"Authorization = \"secret://env/MISSING_AUTH\"",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	check := exec.Command(binary, "mcp", "check", "--json")
	check.Dir = repo
	check.Env = append(
		isolatedValidationEnvironment(configHome, dataHome, stateHome),
		"CHECK_SECRET=top-secret-value",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	check.Stdout = &stdout
	check.Stderr = &stderr
	err = check.Run()
	if err == nil {
		t.Fatalf("mcp check should fail when checks fail")
	}

	if strings.Contains(stdout.String(), "top-secret-value") || strings.Contains(stderr.String(), "top-secret-value") {
		t.Fatalf("mcp check output must not expose resolved secrets: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}

	var payload MCPCheckOutput
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse mcp check json: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if payload.Valid {
		t.Fatalf("expected invalid mcp check payload")
	}

	allCodes := map[string]bool{}
	for _, result := range payload.Results {
		for _, issue := range result.Errors {
			allCodes[issue.Code] = true
		}
	}
	for _, expected := range []string{
		mcpCodeCommandNotFound,
		mcpCodeWorkingDirectoryNotFound,
		mcpCodeSecretResolutionFailed,
	} {
		if !allCodes[expected] {
			t.Fatalf("expected check code %s in payload: %+v", expected, payload)
		}
	}
}

func TestValidateMCPTransportAndFieldConflicts(t *testing.T) {
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

	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte(strings.Join([]string{
			"schema = \"v1\"",
			"[mcp.servers.bad_transport]",
			"transport = \"ftp\"",
			"command = \"bad\"",
			"[mcp.servers.bad_stdio]",
			"transport = \"stdio\"",
			"url = \"https://invalid-for-stdio.example.com\"",
			"command = \"\"",
			"[mcp.servers.bad_http]",
			"transport = \"http\"",
			"url = \"ssh://invalid-scheme\"",
			"command = \"not-allowed\"",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
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
		t.Fatalf("validate should fail for invalid mcp config")
	}

	var report ValidationReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("parse validation json: %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	codes := map[string]bool{}
	for _, finding := range report.Errors {
		codes[finding.Code] = true
	}
	for _, expected := range []string{
		mcpCodeUnsupportedTransport,
		mcpCodeConflictingFields,
		mcpCodeInvalidCommand,
		mcpCodeInvalidURL,
	} {
		if !codes[expected] {
			t.Fatalf("expected validation code %s in errors: %+v", expected, report.Errors)
		}
	}
}
