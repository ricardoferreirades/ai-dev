package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPolicyCommandLifecycleAndJSONDeterminism(t *testing.T) {
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
	if err := seedPolicyFixtures(configHome); err != nil {
		t.Fatalf("seed policy fixtures: %v", err)
	}
	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)

	run := func(expectSuccess bool, args ...string) []byte {
		command := exec.Command(binary, args...)
		command.Dir = repo
		command.Env = env
		output, err := command.CombinedOutput()
		if expectSuccess && err != nil {
			t.Fatalf("command failed: %v\nargs=%v\n%s", err, args, output)
		}
		if !expectSuccess && err == nil {
			t.Fatalf("command unexpectedly succeeded: args=%v\n%s", args, output)
		}
		return output
	}

	listOutput := run(true, "policy", "list", "--json")
	if !strings.Contains(string(listOutput), "require-schema") {
		t.Fatalf("policy list missing expected policy: %s", listOutput)
	}

	showOutput := run(true, "policy", "show", "require-schema", "--json")
	if !strings.Contains(string(showOutput), "policy-v1") {
		t.Fatalf("policy show missing schema: %s", showOutput)
	}

	evaluateOutput := run(true, "policy", "evaluate", "--json")
	payload := map[string]any{}
	if err := json.Unmarshal(evaluateOutput, &payload); err != nil {
		t.Fatalf("invalid policy evaluate JSON: %v\n%s", err, evaluateOutput)
	}
	if _, ok := payload["results"]; !ok {
		t.Fatalf("policy evaluate output missing results: %s", evaluateOutput)
	}

	reportOutput := run(true, "policy", "report", "--json")
	if !strings.Contains(string(reportOutput), "compliance_percentage") {
		t.Fatalf("policy report missing compliance summary: %s", reportOutput)
	}

	explainOutput := run(true, "policy", "explain", "require-schema")
	if !strings.Contains(string(explainOutput), "evaluation_logic") {
		t.Fatalf("policy explain missing evaluation logic: %s", explainOutput)
	}
}

func TestPolicyEnforcedModeBlocksOperation(t *testing.T) {
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
	if err := seedBlockingPolicyFixture(configHome); err != nil {
		t.Fatalf("seed policy fixture: %v", err)
	}
	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)

	run := func(expectSuccess bool, args ...string) []byte {
		command := exec.Command(binary, args...)
		command.Dir = repo
		command.Env = env
		output, err := command.CombinedOutput()
		if expectSuccess && err != nil {
			t.Fatalf("command failed: %v\nargs=%v\n%s", err, args, output)
		}
		if !expectSuccess && err == nil {
			t.Fatalf("command unexpectedly succeeded: args=%v\n%s", args, output)
		}
		return output
	}

	blocked := run(false, "export", "--output", filepath.Join(t.TempDir(), "blocked.aidev"), "--policy-mode", "enforced")
	if !strings.Contains(string(blocked), policyCodeOperationBlocked) {
		t.Fatalf("expected policy_operation_blocked code, got: %s", blocked)
	}

	advisory := run(true, "export", "--output", filepath.Join(t.TempDir(), "allowed.aidev"), "--policy-mode", "advisory")
	if !strings.Contains(string(advisory), "schema=") {
		t.Fatalf("expected successful advisory export output: %s", advisory)
	}
}

func TestPolicyDuplicateIdentifiersFail(t *testing.T) {
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
	policiesDir := filepath.Join(configHome, "policies")
	if err := os.MkdirAll(policiesDir, 0o755); err != nil {
		t.Fatalf("mkdir policies: %v", err)
	}
	policyOne := "schema = \"policy-v1\"\nid = \"dup\"\ntitle = \"One\"\nseverity = \"warning\"\nenabled = true\nenforcement = \"advisory\"\nscopes = [\"global\"]\n[condition]\nop = \"exists\"\npath = \"schema\"\n"
	policyTwo := "schema = \"policy-v1\"\nid = \"dup\"\ntitle = \"Two\"\nseverity = \"warning\"\nenabled = true\nenforcement = \"advisory\"\nscopes = [\"global\"]\n[condition]\nop = \"exists\"\npath = \"schema\"\n"
	if err := os.WriteFile(filepath.Join(policiesDir, "a.toml"), []byte(policyOne), 0o600); err != nil {
		t.Fatalf("write policy one: %v", err)
	}
	if err := os.WriteFile(filepath.Join(policiesDir, "b.toml"), []byte(policyTwo), 0o600); err != nil {
		t.Fatalf("write policy two: %v", err)
	}

	command := exec.Command(binary, "policy", "evaluate")
	command.Dir = repo
	command.Env = isolatedValidationEnvironment(configHome, dataHome, stateHome)
	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("expected duplicate policy failure, got success: %s", output)
	}
	if !strings.Contains(string(output), policyCodeDuplicate) {
		t.Fatalf("expected duplicate policy code, got: %s", output)
	}
}

func seedPolicyFixtures(configHome string) error {
	policiesDir := filepath.Join(configHome, "policies")
	if err := os.MkdirAll(filepath.Join(policiesDir, "org", "baseline"), 0o755); err != nil {
		return err
	}
	policyOne := "schema = \"policy-v1\"\nid = \"require-schema\"\ntitle = \"Require schema\"\ndescription = \"Resolved config must include schema\"\nversion = \"1.0.0\"\nauthor = \"tests\"\nseverity = \"warning\"\nenabled = true\nenforcement = \"advisory\"\nscopes = [\"global\"]\ntarget = \"configuration\"\nmessage = \"schema field is required\"\nfailure_outcome = \"warn\"\n[condition]\nop = \"exists\"\npath = \"schema\"\n"
	if err := os.WriteFile(filepath.Join(policiesDir, "org", "baseline", "require-schema.toml"), []byte(policyOne), 0o600); err != nil {
		return err
	}
	policyTwo := "schema = \"policy-v1\"\nid = \"mcp-token-reference\"\ntitle = \"MCP token reference\"\ndescription = \"MCP env must include secret reference\"\nversion = \"1.0.0\"\nauthor = \"tests\"\nseverity = \"error\"\nenabled = true\nenforcement = \"advisory\"\nscopes = [\"global\"]\ntarget = \"mcp\"\nmessage = \"TOKEN should be a secret reference\"\nfailure_code = \"policy_enforcement_failed\"\nfailure_outcome = \"warn\"\n[condition]\nop = \"contains\"\npath = \"environment.TOKEN\"\nvalue = \"secret://\"\n"
	if err := os.WriteFile(filepath.Join(policiesDir, "mcp-token-reference.toml"), []byte(policyTwo), 0o600); err != nil {
		return err
	}
	return nil
}

func seedBlockingPolicyFixture(configHome string) error {
	policiesDir := filepath.Join(configHome, "policies")
	if err := os.MkdirAll(policiesDir, 0o755); err != nil {
		return err
	}
	policy := "schema = \"policy-v1\"\nid = \"block-export\"\ntitle = \"Block export\"\ndescription = \"Disallow export operation\"\nversion = \"1.0.0\"\nauthor = \"tests\"\nseverity = \"critical\"\nenabled = true\nenforcement = \"enforced\"\nscopes = [\"bundle\"]\ntarget = \"bundle\"\nmessage = \"export operation is blocked by policy\"\nfailure_code = \"policy_operation_blocked\"\nfailure_outcome = \"fail\"\n[condition]\nop = \"not_equals\"\npath = \"operation\"\nvalue = \"export\"\n"
	return os.WriteFile(filepath.Join(policiesDir, "block-export.toml"), []byte(policy), 0o600)
}
