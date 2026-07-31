package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportSourceDirectoryPreservesAndRegistersAIResources(t *testing.T) {
	source := t.TempDir()
	paths := Paths{
		ConfigHome: t.TempDir(),
		DataHome:   t.TempDir(),
		StateHome:  t.TempDir(),
	}

	files := map[string]string{
		"rules/frontend.md":                     "Use modular components.\n",
		"prompts/page.md":                       "Create the page.\n",
		"prompts/cli.prompt.md":                 "Use the CLI prompt.\n",
		"instructions/frontend.instructions.md": "Use the frontend conventions.\n",
		"agents/frontend.agent.md":              "You are the frontend agent.\n",
		"skills/next/SKILL.md":                  "Use Next.js best practices.\n",
		"mcp/servers.json":                      "{}\n",
		".claude/settings.json":                 "{}\n",
		"AGENTS.md":                             "Follow the project agents.\n",
	}
	for relative, content := range files {
		path := filepath.Join(source, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := importSourceCommand(paths, []string{source, "--name", "team"}); err != nil {
		t.Fatalf("import source failed: %v", err)
	}

	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", "rules", "frontend.md"), files["rules/frontend.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", "prompts", "page.md"), files["prompts/page.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", "prompts", "cli.md"), files["prompts/cli.prompt.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", "instructions", "frontend.instructions.md"), files["instructions/frontend.instructions.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", "agents", "frontend.agent.md"), files["agents/frontend.agent.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", "skills", "next", "SKILL.md"), files["skills/next/SKILL.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", ".claude", "settings.json"), files[".claude/settings.json"])

	if _, err := os.Stat(filepath.Join(paths.ConfigHome, "global.toml")); !os.IsNotExist(err) {
		t.Fatalf("source import should not create or modify global.toml")
	}

	previousImportName := activeImportName
	activeImportName = "team"
	defer func() { activeImportName = previousImportName }()
	model, err := resolveRegistrySourceModel(paths)
	if err != nil {
		t.Fatalf("resolve imported registries: %v", err)
	}
	rules, err := registryIndexFromModel(paths, model, registryKindRule)
	if err != nil {
		t.Fatalf("discover imported rules: %v", err)
	}
	if _, ok := rules.Resources["frontend"]; !ok {
		t.Fatalf("imported rule was not discoverable: %+v", rules.Resources)
	}
	resources, err := loadImportedAIResources(paths)
	if err != nil {
		t.Fatalf("load imported resource catalog: %v", err)
	}
	for _, category := range []string{"instructions", "agents", "skills", "mcp", "client"} {
		if len(resources[category]) == 0 {
			t.Fatalf("expected imported %s resources to remain distinct: %+v", category, resources)
		}
	}
}

func TestImportSourceRequiresForceForExistingFiles(t *testing.T) {
	source := t.TempDir()
	paths := Paths{ConfigHome: t.TempDir(), DataHome: t.TempDir(), StateHome: t.TempDir()}
	path := filepath.Join(source, "rules", "safe.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("safe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := importSourceCommand(paths, []string{source, "--name", "team"}); err != nil {
		t.Fatalf("initial import failed: %v", err)
	}
	if err := importSourceCommand(paths, []string{source, "--name", "team"}); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected force conflict, got %v", err)
	}
	if err := importSourceCommand(paths, []string{source, "--name", "team", "--force"}); err != nil {
		t.Fatalf("forced import failed: %v", err)
	}
}

func TestImportSourceIgnoresSelectedCategories(t *testing.T) {
	source := t.TempDir()
	paths := Paths{ConfigHome: t.TempDir(), DataHome: t.TempDir(), StateHome: t.TempDir()}
	files := map[string]string{
		"prompts/ignored.md":      "prompt\n",
		"rules/kept.md":           "rule\n",
		"agents/ignored.agent.md": "agent\n",
		"mcp/ignored.json":        "{}\n",
	}
	for relative, content := range files {
		path := filepath.Join(source, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := importSourceCommand(paths, []string{source, "--name", "selective", "--ignore", "prompts", "--ignore=agents", "--ignore", "mcps"}); err != nil {
		t.Fatalf("selective import failed: %v", err)
	}
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "selective", "rules", "kept.md"), files["rules/kept.md"])
	for _, path := range []string{
		filepath.Join(paths.ConfigHome, "imports", "selective", "prompts", "ignored.md"),
		filepath.Join(paths.ConfigHome, "imports", "selective", "agents", "ignored.agent.md"),
		filepath.Join(paths.ConfigHome, "imports", "selective", "mcp", "ignored.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("ignored resource was imported: %s", path)
		}
	}
	manifestData, err := os.ReadFile(filepath.Join(paths.ConfigHome, "imports", "selective", "import.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	ignored, ok := manifest["ignored"].([]any)
	if !ok || len(ignored) != 3 {
		t.Fatalf("manifest did not record ignored categories: %+v", manifest["ignored"])
	}
}

func assertImportedFile(t *testing.T, path, expected string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read imported file %s: %v", path, err)
	}
	if string(content) != expected {
		t.Fatalf("unexpected content in %s: %q", path, string(content))
	}
}
