package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptAndRuleRegistryCommands(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()

	mustMkdirAll(t, filepath.Join(configHome, "projects"))
	mustMkdirAll(t, filepath.Join(configHome, "prompts", "backend"))
	mustMkdirAll(t, filepath.Join(configHome, "rules", "go"))

	mustWriteFile(t, filepath.Join(configHome, "prompts", "backend", "reviewer.md"), strings.Join([]string{
		"---",
		"title: Backend Reviewer",
		"description: Review backend changes",
		"tags: [backend, review]",
		"---",
		"## Backend Review Checklist",
		"- Validate boundaries",
	}, "\n"))
	mustWriteFile(t, filepath.Join(configHome, "prompts", "security.txt"), "Security prompt content")

	mustWriteFile(t, filepath.Join(configHome, "rules", "go", "reviewer.md"), strings.Join([]string{
		"---",
		"title: Go Rules",
		"tags: [go, lint]",
		"---",
		"Use gofmt and go vet.",
	}, "\n"))
	mustWriteFile(t, filepath.Join(configHome, "rules", "security.txt"), "Never log secrets.")

	mustWriteFile(t, filepath.Join(configHome, "global.toml"), strings.Join([]string{
		"schema = \"v1\"",
		"[prompts]",
		"enabled = [\"backend/reviewer\", \"security\"]",
		"[rules]",
		"enabled = [\"go/reviewer\", \"security\"]",
	}, "\n"))

	projectID := filesystemProjectID(repo)
	if resolvedRepo, resolveErr := filepath.EvalSymlinks(repo); resolveErr == nil {
		projectID = filesystemProjectID(resolvedRepo)
	}
	mustWriteFile(t, filepath.Join(configHome, "projects", safeProjectFilename(projectID)+".toml"), strings.Join([]string{
		"schema = \"v1\"",
		"[prompts]",
		"enabled = [\"security\", \"backend/reviewer\"]",
		"[rules]",
		"enabled = [\"security\", \"go/reviewer\"]",
	}, "\n"))

	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)

	listOutput := runOK(t, repo, env, binary, "prompt", "list", "--json")
	var promptList struct {
		Resources []registryListEntry `json:"resources"`
	}
	mustUnmarshal(t, listOutput, &promptList)
	if len(promptList.Resources) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(promptList.Resources))
	}
	if promptList.Resources[0].Identifier != "backend/reviewer" || promptList.Resources[1].Identifier != "security" {
		t.Fatalf("unexpected prompt list ordering: %+v", promptList.Resources)
	}

	showOutput := runOK(t, repo, env, binary, "prompt", "show", "backend/reviewer", "--json")
	var shown registryResource
	mustUnmarshal(t, showOutput, &shown)
	if shown.Metadata.Title != "Backend Reviewer" {
		t.Fatalf("expected metadata title in show output: %+v", shown)
	}
	if !strings.Contains(shown.Content, "Backend Review Checklist") {
		t.Fatalf("expected preserved markdown content in show output: %s", shown.Content)
	}

	searchOutput := runOK(t, repo, env, binary, "prompt", "search", "review", "--json")
	var searchPayload map[string]any
	mustUnmarshal(t, searchOutput, &searchPayload)
	matches, ok := searchPayload["matches"].([]any)
	if !ok || len(matches) == 0 {
		t.Fatalf("expected search matches: %+v", searchPayload)
	}

	resolveOutput := runOK(t, repo, env, binary, "prompt", "resolve")
	resolvedText := string(resolveOutput)
	if strings.Count(resolvedText, "Security prompt content") != 1 {
		t.Fatalf("expected duplicate prompt references to be eliminated: %s", resolvedText)
	}
	if strings.Index(resolvedText, "Backend Review Checklist") > strings.Index(resolvedText, "Security prompt content") {
		t.Fatalf("expected global prompt order to win deterministically: %s", resolvedText)
	}

	ruleResolveOutput := runOK(t, repo, env, binary, "rule", "resolve", "--json")
	var ruleResolved map[string]any
	mustUnmarshal(t, ruleResolveOutput, &ruleResolved)
	ids, ok := ruleResolved["identifiers"].([]any)
	if !ok || len(ids) != 2 {
		t.Fatalf("expected 2 rule identifiers in resolved output: %+v", ruleResolved)
	}
}

func TestPromptAndRuleValidationMissingReferences(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()

	mustMkdirAll(t, filepath.Join(configHome, "projects"))
	mustMkdirAll(t, filepath.Join(configHome, "prompts"))
	mustMkdirAll(t, filepath.Join(configHome, "rules"))

	mustWriteFile(t, filepath.Join(configHome, "global.toml"), strings.Join([]string{
		"schema = \"v1\"",
		"[prompts]",
		"enabled = [\"missing-prompt\"]",
		"[rules]",
		"enabled = [\"missing-rule\"]",
	}, "\n"))

	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)
	validate := exec.Command(binary, "validate", "--json")
	validate.Dir = repo
	validate.Env = env
	var stdout strings.Builder
	var stderr strings.Builder
	validate.Stdout = &stdout
	validate.Stderr = &stderr
	err = validate.Run()
	if err == nil {
		t.Fatalf("expected validate to fail on missing prompt/rule references")
	}

	var report ValidationReport
	mustUnmarshal(t, []byte(stdout.String()), &report)
	codes := map[string]bool{}
	for _, finding := range report.Errors {
		codes[finding.Code] = true
	}
	if !codes[registryCodePromptNotFound] || !codes[registryCodeRuleNotFound] {
		t.Fatalf("expected missing reference codes, got errors: %+v", report.Errors)
	}
}

func TestPromptAndRuleRegistryFileValidation(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()

	mustMkdirAll(t, filepath.Join(configHome, "projects"))
	mustMkdirAll(t, filepath.Join(configHome, "prompts", "dup"))
	mustMkdirAll(t, filepath.Join(configHome, "rules"))

	mustWriteFile(t, filepath.Join(configHome, "prompts", "dup", "item.md"), "content-a")
	mustWriteFile(t, filepath.Join(configHome, "prompts", "dup", "item.txt"), "content-b")
	mustWriteFile(t, filepath.Join(configHome, "prompts", "bad.json"), "{}")
	mustWriteFile(t, filepath.Join(configHome, "rules", "empty.txt"), "\n")
	mustWriteFile(t, filepath.Join(configHome, "rules", "badmeta.md"), strings.Join([]string{
		"---",
		"title: [1,2]",
		"---",
		"rule content",
	}, "\n"))

	mustWriteFile(t, filepath.Join(configHome, "global.toml"), strings.Join([]string{
		"schema = \"v1\"",
	}, "\n"))

	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)
	validate := exec.Command(binary, "validate", "--json")
	validate.Dir = repo
	validate.Env = env
	var stdout strings.Builder
	var stderr strings.Builder
	validate.Stdout = &stdout
	validate.Stderr = &stderr
	err = validate.Run()
	if err == nil {
		t.Fatalf("expected validate to fail on invalid registry files")
	}

	var report ValidationReport
	mustUnmarshal(t, []byte(stdout.String()), &report)
	codes := map[string]bool{}
	for _, finding := range report.Errors {
		codes[finding.Code] = true
	}
	for _, expected := range []string{
		registryCodeDuplicatePrompt,
		registryCodeUnsupportedPromptFmt,
		registryCodeEmptyRule,
		registryCodeInvalidRuleMetadata,
	} {
		if !codes[expected] {
			t.Fatalf("expected validation code %s, got %+v", expected, report.Errors)
		}
	}
}

func TestDoctorReportsPromptAndRuleRegistry(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()

	mustMkdirAll(t, filepath.Join(configHome, "projects"))
	mustMkdirAll(t, filepath.Join(configHome, "prompts"))
	mustMkdirAll(t, filepath.Join(configHome, "rules"))
	mustWriteFile(t, filepath.Join(configHome, "prompts", "one.md"), "one")
	mustWriteFile(t, filepath.Join(configHome, "rules", "one.md"), "one")
	mustWriteFile(t, filepath.Join(configHome, "global.toml"), "schema = \"v1\"\n")

	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)
	output := runOK(t, repo, env, binary, "doctor")
	text := string(output)
	if !strings.Contains(text, "prompt registry") || !strings.Contains(text, "rule registry") {
		t.Fatalf("expected doctor registry lines, got:\n%s", text)
	}
}

func runOK(t *testing.T, dir string, env []string, binary string, args ...string) []byte {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = dir
	command.Env = env
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("command %v failed: %v\n%s", args, err, output)
	}
	return output
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustUnmarshal(t *testing.T, payload []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("unmarshal JSON: %v\n%s", err, payload)
	}
}
