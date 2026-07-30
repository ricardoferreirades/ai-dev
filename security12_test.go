package main

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpoint12KeyTrustSigningAndEncryptionFlow(t *testing.T) {
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
	env = append(env, "AI_DEV_KEY_PASS=checkpoint12-passphrase")

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

	run(true, "key", "generate", "--purpose", "signing", "--id", "release-key", "--passphrase-ref", "secret://env/AI_DEV_KEY_PASS")
	run(true, "key", "generate", "--purpose", "encryption", "--id", "work-laptop", "--passphrase-ref", "secret://env/AI_DEV_KEY_PASS")

	keysJSON := run(true, "key", "list", "--json")
	if !strings.Contains(string(keysJSON), "release-key") || !strings.Contains(string(keysJSON), "work-laptop") {
		t.Fatalf("key list output missing expected keys: %s", keysJSON)
	}

	run(true, "trust", "set", "release-key", "trusted", "--scope", "project")
	trustJSON := run(true, "trust", "show", "release-key", "--json")
	if !strings.Contains(string(trustJSON), "trusted") {
		t.Fatalf("expected trusted state in trust show output: %s", trustJSON)
	}

	secureBundle := filepath.Join(t.TempDir(), "secure.aidev")
	run(true,
		"export",
		"--output", secureBundle,
		"--sign", "release-key",
		"--encrypt-for", "work-laptop",
		"--key-passphrase-ref", "secret://env/AI_DEV_KEY_PASS",
	)

	verifyJSON := run(true, "bundle", "verify", secureBundle, "--require-trusted-signature", "--json")
	if !strings.Contains(string(verifyJSON), "\"encrypted\": true") || !strings.Contains(string(verifyJSON), "\"signed\": true") {
		t.Fatalf("expected signed+encrypted verify output: %s", verifyJSON)
	}

	run(true, "bundle", "verify-signature", secureBundle)
	decryptInfo := run(true, "bundle", "decrypt", secureBundle, "--key", "work-laptop", "--passphrase-ref", "secret://env/AI_DEV_KEY_PASS", "--json")
	if !strings.Contains(string(decryptInfo), "\"decrypted\": true") {
		t.Fatalf("expected decrypt success output: %s", decryptInfo)
	}

	unsignedBundle := filepath.Join(t.TempDir(), "unsigned.aidev")
	run(true, "export", "--output", unsignedBundle)
	run(false, "import", unsignedBundle, "--require-signed")

	tamperedBundle := filepath.Join(t.TempDir(), "tampered.aidev")
	run(true, "export", "--output", tamperedBundle, "--sign", "release-key", "--key-passphrase-ref", "secret://env/AI_DEV_KEY_PASS")
	if err := tamperBundleResource(tamperedBundle); err != nil {
		t.Fatalf("tamper bundle: %v", err)
	}
	run(false, "bundle", "verify-signature", tamperedBundle)
}

func TestCheckpoint12DecryptWithWrongRecipientFails(t *testing.T) {
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
	env = append(env, "AI_DEV_KEY_PASS=checkpoint12-passphrase")

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

	run(true, "key", "generate", "--purpose", "encryption", "--id", "recipient-a", "--passphrase-ref", "secret://env/AI_DEV_KEY_PASS")
	run(true, "key", "generate", "--purpose", "encryption", "--id", "recipient-b", "--passphrase-ref", "secret://env/AI_DEV_KEY_PASS")

	bundlePath := filepath.Join(t.TempDir(), "enc.aidev")
	run(true, "export", "--output", bundlePath, "--encrypt-for", "recipient-a")
	failure := run(false, "bundle", "decrypt", bundlePath, "--key", "recipient-b", "--passphrase-ref", "secret://env/AI_DEV_KEY_PASS")
	if !strings.Contains(string(failure), securityCodeRecipientPrivateKeyUnavailable) && !strings.Contains(string(failure), securityCodeRecipientKeyNotFound) {
		t.Fatalf("expected recipient private key unavailable error, got: %s", failure)
	}
}

func tamperBundleResource(path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	tempPath := path + ".tmp"
	out, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(out)

	for _, file := range reader.File {
		rc, err := file.Open()
		if err != nil {
			_ = writer.Close()
			_ = out.Close()
			return err
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			_ = writer.Close()
			_ = out.Close()
			return err
		}
		if strings.HasPrefix(file.Name, "resources/") && !strings.HasSuffix(file.Name, "/") {
			content = append(content, []byte("\n# tampered\n")...)
		}
		header := file.FileHeader
		entry, err := writer.CreateHeader(&header)
		if err != nil {
			_ = writer.Close()
			_ = out.Close()
			return err
		}
		if _, err := entry.Write(content); err != nil {
			_ = writer.Close()
			_ = out.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}

func TestCheckpoint12TrustListJSONShape(t *testing.T) {
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	binary := buildValidationTestBinary(t, workspace)
	repo := t.TempDir()
	configHome := t.TempDir()
	dataHome := t.TempDir()
	stateHome := t.TempDir()
	env := isolatedValidationEnvironment(configHome, dataHome, stateHome)
	env = append(env, "AI_DEV_KEY_PASS=checkpoint12-passphrase")

	command := exec.Command(binary, "trust", "list", "--json")
	command.Dir = repo
	command.Env = env
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("trust list failed: %v\n%s", err, output)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("trust list JSON is invalid: %v\n%s", err, output)
	}
	if _, ok := payload["records"]; !ok {
		t.Fatalf("trust list output missing records: %s", output)
	}
}
