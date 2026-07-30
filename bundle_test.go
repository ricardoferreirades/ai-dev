package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBundleExportVerifyShowAndMetadata(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()
	if err := seedBundleFixture(configHome, dataHome); err != nil {
		t.Fatalf("seed fixture: %v", err)
	}
	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)
	bundlePath := filepath.Join(t.TempDir(), "fixture.aidev")

	export := exec.Command(binary, "export", "--output", bundlePath, "--include-machine", "--include-plugins")
	export.Dir = repo
	export.Env = env
	if output, err := export.CombinedOutput(); err != nil {
		t.Fatalf("export failed: %v\n%s", err, output)
	}
	if !fileExists(bundlePath) {
		t.Fatalf("expected exported bundle at %s", bundlePath)
	}

	verify := exec.Command(binary, "bundle", "verify", bundlePath)
	verify.Dir = repo
	verify.Env = env
	if output, err := verify.CombinedOutput(); err != nil {
		t.Fatalf("bundle verify failed: %v\n%s", err, output)
	}

	show := exec.Command(binary, "bundle", "show", bundlePath, "--json")
	show.Dir = repo
	show.Env = env
	showOutput, err := show.CombinedOutput()
	if err != nil {
		t.Fatalf("bundle show failed: %v\n%s", err, showOutput)
	}
	var showPayload map[string]any
	if err := json.Unmarshal(showOutput, &showPayload); err != nil {
		t.Fatalf("parse bundle show JSON: %v\n%s", err, showOutput)
	}
	manifest, ok := showPayload["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("missing manifest in show payload: %+v", showPayload)
	}
	if schema, _ := manifest["schema"].(string); schema != bundleSchemaV1 {
		t.Fatalf("unexpected manifest schema: %+v", manifest)
	}

	metadata := exec.Command(binary, "bundle", "metadata", bundlePath, "--json")
	metadata.Dir = repo
	metadata.Env = env
	metadataOutput, err := metadata.CombinedOutput()
	if err != nil {
		t.Fatalf("bundle metadata failed: %v\n%s", err, metadataOutput)
	}
	if !strings.Contains(string(metadataOutput), bundleSchemaV1) {
		t.Fatalf("metadata output should contain schema: %s", metadataOutput)
	}

	list := exec.Command(binary, "bundle", "list", filepath.Dir(bundlePath), "--json")
	list.Dir = repo
	list.Env = env
	listOutput, err := list.CombinedOutput()
	if err != nil {
		t.Fatalf("bundle list failed: %v\n%s", err, listOutput)
	}
	if !strings.Contains(string(listOutput), ".aidev") {
		t.Fatalf("bundle list should include aidev file: %s", listOutput)
	}
}

func TestBundleImportDryRunConflictPoliciesAndSyncPreview(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)

	repo := t.TempDir()
	sourceConfig := t.TempDir()
	sourceData := t.TempDir()
	sourceState := t.TempDir()
	if err := seedBundleFixture(sourceConfig, sourceData); err != nil {
		t.Fatalf("seed source fixture: %v", err)
	}

	exportEnv := isolatedValidationEnvironment(sourceConfig, sourceData, sourceState)
	bundlePath := filepath.Join(t.TempDir(), "sync-fixture.aidev")
	export := exec.Command(binary, "export", "--output", bundlePath)
	export.Dir = repo
	export.Env = exportEnv
	if output, err := export.CombinedOutput(); err != nil {
		t.Fatalf("export failed: %v\n%s", err, output)
	}

	targetConfig := t.TempDir()
	targetData := t.TempDir()
	targetState := t.TempDir()
	if err := os.MkdirAll(filepath.Join(targetConfig, "projects"), 0o755); err != nil {
		t.Fatalf("prepare target projects dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetConfig, "global.toml"), []byte("schema = \"v1\"\nname = \"existing\"\n"), 0o600); err != nil {
		t.Fatalf("write existing global config: %v", err)
	}
	importEnv := isolatedValidationEnvironment(targetConfig, targetData, targetState)

	dryRun := exec.Command(binary, "import", bundlePath, "--dry-run", "--json")
	dryRun.Dir = repo
	dryRun.Env = importEnv
	dryRunOutput, err := dryRun.CombinedOutput()
	if err != nil {
		t.Fatalf("import dry-run should succeed: %v\n%s", err, dryRunOutput)
	}
	var report map[string]any
	if err := json.Unmarshal(dryRunOutput, &report); err != nil {
		t.Fatalf("parse dry-run output: %v\n%s", err, dryRunOutput)
	}

	conflictImport := exec.Command(binary, "import", bundlePath)
	conflictImport.Dir = repo
	conflictImport.Env = importEnv
	conflictOutput, err := conflictImport.CombinedOutput()
	if err == nil {
		t.Fatalf("import should fail with default conflict policy")
	}
	if !strings.Contains(string(conflictOutput), bundleCodeConflict) {
		t.Fatalf("expected bundle conflict error, got: %s", conflictOutput)
	}

	overwriteImport := exec.Command(binary, "import", bundlePath, "--overwrite")
	overwriteImport.Dir = repo
	overwriteImport.Env = importEnv
	if output, err := overwriteImport.CombinedOutput(); err != nil {
		t.Fatalf("import overwrite failed: %v\n%s", err, output)
	}

	syncPreview := exec.Command(binary, "sync", "preview", bundlePath, "--json")
	syncPreview.Dir = repo
	syncPreview.Env = importEnv
	previewOutput, err := syncPreview.CombinedOutput()
	if err != nil {
		t.Fatalf("sync preview failed: %v\n%s", err, previewOutput)
	}
	if !strings.Contains(string(previewOutput), "creates") {
		t.Fatalf("sync preview output should include import report fields: %s", previewOutput)
	}
}

func seedBundleFixture(configHome string, dataHome string) error {
	if err := os.MkdirAll(filepath.Join(configHome, "projects"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(configHome, "profiles"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(configHome, "prompts"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(configHome, "rules"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(configHome, "machines"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(configHome, "global.toml"), []byte("schema = \"v1\"\nname = \"bundle\"\n[environment]\nTOKEN = \"secret://env/TOKEN\"\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(configHome, "projects", "filesystem-repo.toml"), []byte("schema = \"v1\"\nprofiles = [\"team\"]\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(configHome, "profiles", "team.toml"), []byte("schema = \"v1\"\nname = \"team\"\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(configHome, "prompts", "intro.md"), []byte("# Prompt\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(configHome, "rules", "safe.md"), []byte("# Rule\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(configHome, "machines", "dev.toml"), []byte("schema = \"v1\"\n[environment]\nMODE = \"dev\"\n"), 0o600); err != nil {
		return err
	}
	pluginManifestDir := filepath.Join(dataHome, "plugins", "fixture")
	if err := os.MkdirAll(pluginManifestDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(pluginManifestDir, "plugin.toml"), []byte("schema = \"v1\"\nid = \"fixture\"\nname = \"Fixture\"\nversion = \"1.0.0\"\nprotocol = \"ai-dev-plugin-v1\"\nexecutable = \"bin/fixture\"\ncapabilities = [\"validator\"]\n"), 0o600); err != nil {
		return err
	}
	return nil
}
