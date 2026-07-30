package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestPluginCommandsAndIntegrations(t *testing.T) {
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
		t.Fatalf("write global config: %v", err)
	}

	pluginDir := filepath.Join(dataHome, "plugins", "fixture")
	if err := writeFixturePlugin(t, pluginDir, "fixture", []string{"secret-provider", "prompt-provider", "validator"}, []string{"onepassword"}, []string{"plugin-client"}); err != nil {
		t.Fatalf("write fixture plugin: %v", err)
	}

	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)

	list := exec.Command(binary, "plugin", "list", "--json")
	list.Dir = repo
	list.Env = env
	output, err := list.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin list failed: %v\n%s", err, output)
	}
	var listPayload struct {
		Plugins []pluginListEntry `json:"plugins"`
	}
	if err := json.Unmarshal(output, &listPayload); err != nil {
		t.Fatalf("parse plugin list payload: %v\n%s", err, output)
	}
	if len(listPayload.Plugins) != 1 || listPayload.Plugins[0].ID != "fixture" {
		t.Fatalf("unexpected plugin list payload: %+v", listPayload)
	}

	show := exec.Command(binary, "plugin", "show", "fixture", "--json", "--handshake")
	show.Dir = repo
	show.Env = env
	output, err = show.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin show failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "runtime_capabilities") {
		t.Fatalf("plugin show should include runtime capabilities: %s", output)
	}

	validate := exec.Command(binary, "plugin", "validate", "--json")
	validate.Dir = repo
	validate.Env = env
	output, err = validate.CombinedOutput()
	if err != nil {
		t.Fatalf("plugin validate should pass: %v\n%s", err, output)
	}
	var validatePayload map[string]any
	if err := json.Unmarshal(output, &validatePayload); err != nil {
		t.Fatalf("parse plugin validate payload: %v\n%s", err, output)
	}
	if valid, ok := validatePayload["valid"].(bool); !ok || !valid {
		t.Fatalf("plugin validate expected valid=true: %+v", validatePayload)
	}

	secret := exec.Command(binary, "secret", "resolve", "secret://onepassword/item/path")
	secret.Dir = repo
	secret.Env = env
	output, err = secret.CombinedOutput()
	if err != nil {
		t.Fatalf("secret resolve with plugin provider failed: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != "resolved:item/path" {
		t.Fatalf("unexpected plugin secret resolution output: %s", output)
	}

	promptList := exec.Command(binary, "prompt", "list", "--json")
	promptList.Dir = repo
	promptList.Env = env
	output, err = promptList.CombinedOutput()
	if err != nil {
		t.Fatalf("prompt list should include plugin resources: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "plugin/greeting") {
		t.Fatalf("expected plugin-provided prompt in list output: %s", output)
	}

	unsupported := exec.Command(binary, "plugin", "run", "fixture", "nope", "--json")
	unsupported.Dir = repo
	unsupported.Env = env
	output, err = unsupported.CombinedOutput()
	if err == nil {
		t.Fatalf("plugin run should fail for unsupported operations")
	}
	if !strings.Contains(string(output), pluginCodeOperationUnsupported) {
		t.Fatalf("expected unsupported operation code, got: %s", output)
	}
}

func TestPluginDuplicateIdentifierValidation(t *testing.T) {
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
		t.Fatalf("write global config: %v", err)
	}

	pluginPathOne := filepath.Join(t.TempDir(), "plugins-one")
	pluginPathTwo := filepath.Join(t.TempDir(), "plugins-two")
	if err := writeFixturePlugin(t, filepath.Join(pluginPathOne, "dup-a"), "dup", []string{"validator"}, nil, nil); err != nil {
		t.Fatalf("write duplicate plugin A: %v", err)
	}
	if err := writeFixturePlugin(t, filepath.Join(pluginPathTwo, "dup-b"), "dup", []string{"validator"}, nil, nil); err != nil {
		t.Fatalf("write duplicate plugin B: %v", err)
	}

	env := append(
		isolatedValidationEnvironment(configHome, dataHome, stateHome),
		"AI_DEV_PLUGIN_PATH="+pluginPathOne+string(os.PathListSeparator)+pluginPathTwo,
	)

	validate := exec.Command(binary, "plugin", "validate", "--json")
	validate.Dir = repo
	validate.Env = env
	output, err := validate.CombinedOutput()
	if err == nil {
		t.Fatalf("plugin validate should fail on duplicate IDs")
	}
	if !strings.Contains(string(output), pluginCodeDuplicateIdentifier) {
		t.Fatalf("expected duplicate plugin identifier code, got: %s", output)
	}
}

func writeFixturePlugin(t *testing.T, pluginDir string, pluginID string, capabilities []string, providers []string, clients []string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
		return err
	}

	manifest := strings.Builder{}
	manifest.WriteString("schema = \"v1\"\n")
	manifest.WriteString("id = \"" + pluginID + "\"\n")
	manifest.WriteString("name = \"Fixture Plugin\"\n")
	manifest.WriteString("version = \"1.0.0\"\n")
	manifest.WriteString("protocol = \"" + pluginProtocolV1 + "\"\n")
	manifest.WriteString("executable = \"bin/fixture-plugin\"\n")
	manifest.WriteString("capabilities = [")
	for index, capability := range capabilities {
		if index > 0 {
			manifest.WriteString(", ")
		}
		manifest.WriteString("\"" + capability + "\"")
	}
	manifest.WriteString("]\n")
	manifest.WriteString("platforms = [\"" + runtime.GOOS + "\"]\n")
	manifest.WriteString("architectures = [\"" + runtime.GOARCH + "\"]\n")
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte(manifest.String()), 0o600); err != nil {
		return err
	}

	pluginSource := fixturePluginSource(capabilities, providers, clients)
	sourcePath := filepath.Join(pluginDir, "fixture_plugin_main.go")
	if err := os.WriteFile(sourcePath, []byte(pluginSource), 0o600); err != nil {
		return err
	}

	binaryPath := filepath.Join(pluginDir, "bin", "fixture-plugin")
	build := exec.Command("go", "build", "-trimpath", "-o", binaryPath, sourcePath)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build fixture plugin: %v\n%s", err, output)
	}
	if err := os.Chmod(binaryPath, 0o755); err != nil {
		return err
	}
	return nil
}

func fixturePluginSource(capabilities []string, providers []string, clients []string) string {
	capabilitiesJSON, _ := json.Marshal(capabilities)
	providersJSON, _ := json.Marshal(providers)
	clientsJSON, _ := json.Marshal(clients)
	capabilitiesLiteral := strconv.Quote(string(capabilitiesJSON))
	providersLiteral := strconv.Quote(string(providersJSON))
	clientsLiteral := strconv.Quote(string(clientsJSON))
	return "package main\n" +
		"import (\n" +
		"  \"bufio\"\n" +
		"  \"encoding/json\"\n" +
		"  \"fmt\"\n" +
		"  \"os\"\n" +
		"  \"strings\"\n" +
		")\n" +
		"const capabilitiesJSON = " + capabilitiesLiteral + "\n" +
		"const providersJSON = " + providersLiteral + "\n" +
		"const clientsJSON = " + clientsLiteral + "\n" +
		"func main() {\n" +
		"  scanner := bufio.NewScanner(os.Stdin)\n" +
		"  for scanner.Scan() {\n" +
		"    line := strings.TrimSpace(scanner.Text())\n" +
		"    if line == \"\" {\n" +
		"      continue\n" +
		"    }\n" +
		"    request := map[string]any{}\n" +
		"    if err := json.Unmarshal([]byte(line), &request); err != nil {\n" +
		"      write(map[string]any{\"protocol\": \"" + pluginProtocolV1 + "\", \"ok\": false, \"code\": \"" + pluginCodeOutputInvalid + "\", \"message\": \"invalid request\"})\n" +
		"      continue\n" +
		"    }\n" +
		"    typ, _ := request[\"type\"].(string)\n" +
		"    switch typ {\n" +
		"    case \"handshake\":\n" +
		"      write(map[string]any{\"protocol\": \"" + pluginProtocolV1 + "\", \"ok\": true, \"plugin_id\": os.Getenv(\"AI_DEV_PLUGIN_ID\"), \"plugin_version\": \"1.0.0\"})\n" +
		"    case \"capabilities\":\n" +
		"      caps := []map[string]any{}\n" +
		"      declared := map[string]bool{}\n" +
		"      for _, capability := range mustStrings(capabilitiesJSON) {\n" +
		"        if declared[capability] {\n" +
		"          continue\n" +
		"        }\n" +
		"        declared[capability] = true\n" +
		"        metadata := map[string]any{}\n" +
		"        operations := []string{}\n" +
		"        switch capability {\n" +
		"        case \"secret-provider\":\n" +
		"          operations = []string{\"resolve\"}\n" +
		"          metadata[\"providers\"] = mustStrings(providersJSON)\n" +
		"        case \"prompt-provider\":\n" +
		"          operations = []string{\"list\"}\n" +
		"        case \"rule-provider\":\n" +
		"          operations = []string{\"list\"}\n" +
		"        case \"validator\":\n" +
		"          operations = []string{\"validate\"}\n" +
		"        case \"client-adapter\":\n" +
		"          operations = []string{\"generate\"}\n" +
		"          metadata[\"clients\"] = mustStrings(clientsJSON)\n" +
		"        case \"mcp-transform\":\n" +
		"          operations = []string{\"transform\"}\n" +
		"        }\n" +
		"        caps = append(caps, map[string]any{\"name\": capability, \"operations\": operations, \"input_schema_version\": \"v1\", \"output_schema_version\": \"v1\", \"metadata\": metadata})\n" +
		"      }\n" +
		"      write(map[string]any{\"protocol\": \"" + pluginProtocolV1 + "\", \"ok\": true, \"capabilities\": caps})\n" +
		"    case \"run\":\n" +
		"      capability, _ := request[\"capability\"].(string)\n" +
		"      operation, _ := request[\"operation\"].(string)\n" +
		"      input, _ := request[\"input\"].(map[string]any)\n" +
		"      switch capability + \"/\" + operation {\n" +
		"      case \"secret-provider/resolve\":\n" +
		"        reference, _ := input[\"reference\"].(string)\n" +
		"        write(map[string]any{\"protocol\": \"" + pluginProtocolV1 + "\", \"ok\": true, \"output\": map[string]any{\"value\": \"resolved:\" + reference}})\n" +
		"      case \"prompt-provider/list\":\n" +
		"        write(map[string]any{\"protocol\": \"" + pluginProtocolV1 + "\", \"ok\": true, \"resources\": []map[string]any{{\"identifier\": \"plugin/greeting\", \"format\": \"md\", \"content\": \"# Hello from plugin\\n\", \"metadata\": map[string]any{\"title\": \"Plugin Greeting\"}}}})\n" +
		"      case \"validator/validate\":\n" +
		"        write(map[string]any{\"protocol\": \"" + pluginProtocolV1 + "\", \"ok\": true, \"findings\": []map[string]any{}})\n" +
		"      default:\n" +
		"        write(map[string]any{\"protocol\": \"" + pluginProtocolV1 + "\", \"ok\": false, \"code\": \"" + pluginCodeOperationUnsupported + "\", \"message\": \"unsupported operation\"})\n" +
		"      }\n" +
		"    default:\n" +
		"      write(map[string]any{\"protocol\": \"" + pluginProtocolV1 + "\", \"ok\": false, \"code\": \"" + pluginCodeOutputInvalid + "\", \"message\": \"unknown request type\"})\n" +
		"    }\n" +
		"  }\n" +
		"}\n" +
		"func write(payload map[string]any) {\n" +
		"  encoded, _ := json.Marshal(payload)\n" +
		"  fmt.Println(string(encoded))\n" +
		"}\n" +
		"func mustStrings(raw string) []string {\n" +
		"  values := []string{}\n" +
		"  _ = json.Unmarshal([]byte(raw), &values)\n" +
		"  return values\n" +
		"}\n"
}
