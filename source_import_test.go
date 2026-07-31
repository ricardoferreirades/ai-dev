package main

import (
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

	assertImportedFile(t, filepath.Join(paths.ConfigHome, "rules", "imports", "team", "frontend.md"), files["rules/frontend.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "prompts", "imports", "team", "page.md"), files["prompts/page.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", "instructions", "frontend.instructions.md"), files["instructions/frontend.instructions.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", "agents", "frontend.agent.md"), files["agents/frontend.agent.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", "skills", "next", "SKILL.md"), files["skills/next/SKILL.md"])
	assertImportedFile(t, filepath.Join(paths.ConfigHome, "imports", "team", ".claude", "settings.json"), files[".claude/settings.json"])

	global, err := os.ReadFile(filepath.Join(paths.ConfigHome, "global.toml"))
	if err != nil {
		t.Fatalf("read generated global config: %v", err)
	}
	globalText := string(global)
	if !strings.Contains(globalText, "imports/team/frontend") || !strings.Contains(globalText, "imports/team/page") {
		t.Fatalf("imported registries were not enabled:\n%s", globalText)
	}

	model, err := resolveRegistrySourceModel(paths)
	if err != nil {
		t.Fatalf("resolve imported registries: %v", err)
	}
	rules, err := registryIndexFromModel(paths, model, registryKindRule)
	if err != nil {
		t.Fatalf("discover imported rules: %v", err)
	}
	if _, ok := rules.Resources["imports/team/frontend"]; !ok {
		t.Fatalf("imported rule was not discoverable: %+v", rules.Resources)
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
