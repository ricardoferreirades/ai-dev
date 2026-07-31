package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIProbeRemoteModelUsesConfiguredProviderAndStrictLayout(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing provider authorization")
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := body["plugins"]; !ok {
			t.Fatalf("OpenRouter request must enable official-doc web lookup")
		}
		layout := `{"status":"updated","client":"copilot","target":"user","root":"~/.copilot","instruction_files":["copilot-instructions.md"],"prompt_files":["prompts/ai-dev.prompt.md"],"rule_files":[],"agent_files":["agents/ai-dev.agent.md"],"skill_files":["skills/ai-dev/SKILL.md"],"mcp_files":["mcp-config.json"],"official_docs":["https://docs.github.com/"],"note":"updated from official docs"}`
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": layout}}},
		})
	}))
	defer server.Close()

	paths := Paths{ConfigHome: t.TempDir()}
	env := strings.Join([]string{
		"AI_DEV_AI_PROVIDER=openrouter",
		"OPENROUTER_API_KEY=test-key",
		"OPENROUTER_BASE_URL=" + server.URL,
		"OPENROUTER_MODEL=test-model",
	}, "\n")
	if err := os.WriteFile(aiConfigEnvPath(paths, false), []byte(env), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	result, err := aiProbeRemoteModel(paths, "copilot", "user")
	if err != nil {
		t.Fatalf("probe remote model: %v", err)
	}
	if !called || result.Provider != "openrouter" || result.Model != "test-model" || result.Status != "updated" {
		t.Fatalf("unexpected probe result: %+v", result)
	}
	instructions := result.Parsed["instruction_files"].([]any)
	if len(instructions) != 2 || instructions[1] != "instructions/ai-dev.instructions.md" {
		t.Fatalf("Copilot additional instructions directory was not added: %+v", instructions)
	}
}

func TestAIApplyRemoteConfigUpdateWritesTOMLAndRegistersArbitraryClient(t *testing.T) {
	paths := Paths{ConfigHome: t.TempDir()}
	if err := os.WriteFile(filepath.Join(paths.ConfigHome, "global.toml"), []byte("schema = \"v1\"\n"), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	parsed := map[string]any{
		"status":            "updated",
		"client":            "copilot",
		"target":            "user",
		"root":              "~/.copilot",
		"instruction_files": []any{"copilot-instructions.md"},
		"prompt_files":      []any{"prompts/ai-dev.prompt.md"},
		"rule_files":        []any{},
		"agent_files":       []any{"agents/ai-dev.agent.md"},
		"skill_files":       []any{"skills/ai-dev/SKILL.md"},
		"mcp_files":         []any{"mcp-config.json"},
		"official_docs":     []any{"https://docs.github.com/"},
		"note":              "current layout",
	}
	remote := aiRemoteProbeResult{Status: "updated", Provider: "openrouter", Model: "test", Mode: "ai", Parsed: parsed}
	discovery := aiClientDiscoveryReport{Client: "copilot", Target: "user", Source: "ai", Status: "updated", Mismatch: true}
	if err := aiApplyRemoteConfigUpdate(paths, "copilot", remote, discovery); err != nil {
		t.Fatalf("apply remote config: %v", err)
	}

	definitionPath := filepath.Join(paths.ConfigHome, "clients", "copilot", "client.toml")
	definition, err := readTOML(definitionPath)
	if err != nil {
		t.Fatalf("definition is not valid TOML: %v", err)
	}
	if definition["provider"] != "openrouter" {
		t.Fatalf("unexpected definition: %+v", definition)
	}
	global, err := readTOML(filepath.Join(paths.ConfigHome, "global.toml"))
	if err != nil {
		t.Fatalf("read global config: %v", err)
	}
	clients := global["clients"].(map[string]any)
	copilot := clients["copilot"].(map[string]any)
	if copilot["definition"] != definitionPath || copilot["enabled"] != true {
		t.Fatalf("client was not registered globally: %+v", copilot)
	}
	if findings := validateClientsField("global", global["clients"]); len(findings) != 0 {
		t.Fatalf("registered arbitrary client must validate: %+v", findings)
	}
}

func TestAILayoutPathRejectsTraversal(t *testing.T) {
	if _, err := aiLayoutPath(t.TempDir(), "../outside.md"); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestAINormalizeRemoteDefinitionConvertsConventionsToManagedFiles(t *testing.T) {
	payload := map[string]any{
		"agent_files":  []any{"agents/*.agent.md"},
		"prompt_files": []any{"instructions/*.prompt.md"},
		"skill_files":  []any{"skills/*.SKILL.md"},
	}
	aiNormalizeRemoteDefinition(payload)
	if got := payload["agent_files"].([]any)[0]; got != "agents/ai-dev.agent.md" {
		t.Fatalf("unexpected agent path: %v", got)
	}
	if got := payload["prompt_files"].([]any)[0]; got != "instructions/ai-dev.prompt.md" {
		t.Fatalf("unexpected prompt path: %v", got)
	}
	if got := payload["skill_files"].([]any)[0]; got != "skills/ai-dev/SKILL.md" {
		t.Fatalf("unexpected skill path: %v", got)
	}
}

func TestAIBuildClientBundleIncludesSnapshotFile(t *testing.T) {
	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()

	if err := os.MkdirAll(filepath.Join(configHome, "projects"), 0o755); err != nil {
		t.Fatalf("create projects dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configHome, "prompts"), 0o755); err != nil {
		t.Fatalf("create prompts dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configHome, "rules"), 0o755); err != nil {
		t.Fatalf("create rules dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "prompts", "intro.md"), []byte("# Prompt\n"), 0o600); err != nil {
		t.Fatalf("seed prompts registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "rules", "safe.md"), []byte("# Rule\n"), 0o600); err != nil {
		t.Fatalf("seed rules registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "global.toml"), []byte("schema = \"v1\"\n"), 0o600); err != nil {
		t.Fatalf("write global config: %v", err)
	}
	if err := exec.Command("git", "init", repo).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# repo\n"), 0o600); err != nil {
		t.Fatalf("seed repo readme: %v", err)
	}
	if err := exec.Command("git", "-C", repo, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := exec.Command("git", "-C", repo, "commit", "-m", "init", "--allow-empty").Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	paths := Paths{ConfigHome: configHome, DataHome: dataHome, StateHome: stateHome}
	info, err := resolveProjectInfo(paths)
	if err != nil {
		t.Fatalf("resolve project info: %v", err)
	}
	if info.ProjectRoot == "" {
		t.Fatalf("expected a project root")
	}
	plan, err := aiBuildClientBundle(paths, clientNameCodex, "user")
	if err != nil {
		t.Fatalf("build client bundle: %v", err)
	}
	found := false
	for _, path := range plan.Paths {
		if strings.HasSuffix(path, filepath.Join("library", "default", "ai-client-structure.snapshot.md")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("snapshot file was not included in bundle paths: %+v", plan.Paths)
	}
}

func TestAIExpandNativeFilesConvertsCodexDirectories(t *testing.T) {
	files := aiExpandNativeFiles([]string{"prompts"}, "prompts", "ai-dev.prompt.md")
	if len(files) != 1 || files[0] != "prompts/ai-dev.prompt.md" {
		t.Fatalf("unexpected Codex prompt path: %+v", files)
	}
	layout := map[string]any{"prompt_files": []string{"prompts"}}
	paths := aiLayoutStringSlice(layout, "prompt_files")
	if len(paths) != 1 || paths[0] != "prompts" {
		t.Fatalf("TOML string arrays were not read: %+v", paths)
	}
}
