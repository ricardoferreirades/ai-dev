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

func TestClientListShowPathAndCompareJSON(t *testing.T) {
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
		[]byte("schema = \"v1\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)

	list := exec.Command(binary, "client", "list", "--json")
	list.Dir = repo
	list.Env = env
	output, err := list.CombinedOutput()
	if err != nil {
		t.Fatalf("client list --json failed: %v\n%s", err, output)
	}

	var listPayload struct {
		Clients []ClientListEntry `json:"clients"`
	}
	if err := json.Unmarshal(output, &listPayload); err != nil {
		t.Fatalf("parse client list payload: %v\n%s", err, output)
	}
	if len(listPayload.Clients) != 4 {
		t.Fatalf("expected 4 clients, got %d", len(listPayload.Clients))
	}

	show := exec.Command(binary, "client", "show", "codex", "--json")
	show.Dir = repo
	show.Env = env
	output, err = show.CombinedOutput()
	if err != nil {
		t.Fatalf("client show codex --json failed: %v\n%s", err, output)
	}

	var showPayload ClientShowResult
	if err := json.Unmarshal(output, &showPayload); err != nil {
		t.Fatalf("parse client show payload: %v\n%s", err, output)
	}
	if showPayload.Name != clientNameCodex {
		t.Fatalf("unexpected client show name: %+v", showPayload)
	}
	if showPayload.DefaultFormat != clientFormatJSON {
		t.Fatalf("expected default JSON format for codex: %+v", showPayload)
	}

	path := exec.Command(binary, "client", "path", "vscode", "--json")
	path.Dir = repo
	path.Env = env
	output, err = path.CombinedOutput()
	if err != nil {
		t.Fatalf("client path vscode --json failed: %v\n%s", err, output)
	}

	var pathPayload ClientPathResult
	if err := json.Unmarshal(output, &pathPayload); err != nil {
		t.Fatalf("parse client path payload: %v\n%s", err, output)
	}
	if !pathPayload.Ambiguous {
		t.Fatalf("expected vscode paths to be ambiguous: %+v", pathPayload)
	}

	compare := exec.Command(binary, "client", "compare", "--json")
	compare.Dir = repo
	compare.Env = env
	output, err = compare.CombinedOutput()
	if err != nil {
		t.Fatalf("client compare --json failed: %v\n%s", err, output)
	}

	var comparePayload map[string]any
	if err := json.Unmarshal(output, &comparePayload); err != nil {
		t.Fatalf("parse client compare payload: %v\n%s", err, output)
	}
	clients, ok := comparePayload["clients"].([]any)
	if !ok || len(clients) != 4 {
		t.Fatalf("expected compare payload for 4 clients: %+v", comparePayload)
	}
}

func TestClientGenerateAndValidateSafetyModes(t *testing.T) {
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
			"[mcp.servers.alpha]",
			"transport = \"stdio\"",
			"command = \"printf\"",
			"args = [\"alpha\"]",
			"timeout_seconds = 30",
			"[mcp.servers.alpha.environment]",
			"TOKEN = \"secret://env/ALPHA_TOKEN\"",
			"[mcp.servers.beta]",
			"transport = \"http\"",
			"url = \"https://example.com/mcp\"",
			"enabled = false",
			"[mcp.servers.beta.headers]",
			"Authorization = \"secret://env/BETA_TOKEN\"",
			"[clients.cursor]",
			"enabled = true",
			"[clients.codex]",
			"enabled = true",
			"[clients.claude]",
			"enabled = true",
			"[clients.vscode]",
			"enabled = true",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	baseEnv := append(
		isolatedValidationEnvironment(configHome, dataHome, stateHome),
		"ALPHA_TOKEN=alpha-secret",
		"BETA_TOKEN=beta-secret",
	)

	generateCodex := exec.Command(binary, "client", "generate", "codex", "--json")
	generateCodex.Dir = repo
	generateCodex.Env = baseEnv
	output, err := generateCodex.CombinedOutput()
	if err != nil {
		t.Fatalf("client generate codex --json failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "secret://env/ALPHA_TOKEN") {
		t.Fatalf("codex generation should keep unresolved references by default: %s", output)
	}
	if strings.Contains(string(output), "beta") {
		t.Fatalf("disabled servers must be excluded by default: %s", output)
	}

	generateCodexAll := exec.Command(binary, "client", "generate", "codex", "--json", "--include-disabled")
	generateCodexAll.Dir = repo
	generateCodexAll.Env = baseEnv
	output, err = generateCodexAll.CombinedOutput()
	if err != nil {
		t.Fatalf("client generate codex include-disabled failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "\"beta\"") {
		t.Fatalf("expected disabled server with include-disabled: %s", output)
	}

	generateClaude := exec.Command(binary, "client", "generate", "claude")
	generateClaude.Dir = repo
	generateClaude.Env = baseEnv
	output, err = generateClaude.CombinedOutput()
	if err == nil {
		t.Fatalf("claude generation without secret resolution should fail")
	}
	if !strings.Contains(string(output), clientCodeClientConfigurationMismatch) {
		t.Fatalf("expected unresolved-secret error code, got: %s", output)
	}

	generateClaudeResolved := exec.Command(binary, "client", "generate", "claude", "--resolve-secrets", "--format", "yaml")
	generateClaudeResolved.Dir = repo
	generateClaudeResolved.Env = baseEnv
	output, err = generateClaudeResolved.CombinedOutput()
	if err != nil {
		t.Fatalf("claude generation with resolved secrets should succeed: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "secret://") {
		t.Fatalf("resolved generation should not emit secret references for claude: %s", output)
	}

	validateCursorStrict := exec.Command(binary, "client", "validate", "cursor", "--strict", "--json")
	validateCursorStrict.Dir = repo
	validateCursorStrict.Env = baseEnv
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	validateCursorStrict.Stdout = &stdout
	validateCursorStrict.Stderr = &stderr
	err = validateCursorStrict.Run()
	if err == nil {
		t.Fatalf("strict validation should fail when adapter has warnings")
	}

	var validatePayload ClientValidationResult
	if jsonErr := json.Unmarshal(stdout.Bytes(), &validatePayload); jsonErr != nil {
		t.Fatalf("parse cursor strict validation payload: %v\nstdout=%s\nstderr=%s", jsonErr, stdout.String(), stderr.String())
	}
	if validatePayload.Valid {
		t.Fatalf("expected strict validation failure: %+v", validatePayload)
	}
}

func TestClientSnapshotCommand(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(configHome, "global.toml"), []byte("schema = \"v1\"\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(binary, "client", "snapshot")
	cmd.Dir = repo
	cmd.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("client snapshot failed: %v\n%s", err, output)
	}
	text := string(output)
	if !strings.Contains(text, "# AI client structure snapshot") {
		t.Fatalf("snapshot header missing: %s", text)
	}
	if !strings.Contains(text, ".codex/config/ai-client-structure.snapshot.md") {
		t.Fatalf("library default snapshot path missing: %s", text)
	}
	if !strings.Contains(text, "### codex") || !strings.Contains(text, ".claude/rules.md") {
		t.Fatalf("client hierarchy markers missing: %s", text)
	}
}

func TestClientGenerateOutputFileSafety(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()
	outDir := filepath.Join(repo, "generated")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(configHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(configHome, "global.toml"),
		[]byte(strings.Join([]string{
			"schema = \"v1\"",
			"[mcp.servers.alpha]",
			"transport = \"stdio\"",
			"command = \"printf\"",
			"args = [\"alpha\"]",
			"[mcp.servers.alpha.environment]",
			"TOKEN = \"secret://env/ALPHA_TOKEN\"",
		}, "\n")),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	env := append(
		isolatedValidationEnvironment(configHome, dataHome, stateHome),
		"ALPHA_TOKEN=alpha-secret",
	)

	outputPath := filepath.Join("generated", "codex.json")
	generate := exec.Command(binary, "client", "generate", "codex", "--json", "--resolve-secrets", "--output", outputPath)
	generate.Dir = repo
	generate.Env = env
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("first output generation failed: %v\n%s", err, output)
	}

	absoluteOutput := filepath.Join(repo, outputPath)
	stat, err := os.Stat(absoluteOutput)
	if err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	if stat.Mode().Perm() != 0o600 {
		t.Fatalf("resolved-secret output file must be 0600, got %o", stat.Mode().Perm())
	}

	generateAgain := exec.Command(binary, "client", "generate", "codex", "--json", "--output", outputPath)
	generateAgain.Dir = repo
	generateAgain.Env = env
	output, err := generateAgain.CombinedOutput()
	if err == nil {
		t.Fatalf("generation must fail on existing output without --force")
	}
	if !strings.Contains(string(output), clientCodeClientOutputExists) {
		t.Fatalf("expected output-exists code, got: %s", output)
	}

	force := exec.Command(binary, "client", "generate", "codex", "--json", "--output", outputPath, "--force")
	force.Dir = repo
	force.Env = env
	if output, err := force.CombinedOutput(); err != nil {
		t.Fatalf("force output generation failed: %v\n%s", err, output)
	}

	outside := exec.Command(binary, "client", "generate", "codex", "--json", "--output", "../outside.json")
	outside.Dir = repo
	outside.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	outside.Stdout = &stdout
	outside.Stderr = &stderr
	err = outside.Run()
	if err == nil {
		t.Fatalf("generation must reject non-repository-local output path")
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		t.Fatalf("expected exit code 1 for output path violation, got %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("failed generation must not emit partial stdout output: %s", stdout.String())
	}
}
