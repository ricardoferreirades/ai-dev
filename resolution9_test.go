package main

import (
	"reflect"
	"testing"
)

func TestParseRuntimeOptions(t *testing.T) {
	options, remaining, err := parseRuntimeOptions([]string{
		"--machine", "Dev-Host_01",
		"--profile", "team",
		"--profile-only", "local",
		"config", "--json",
	})
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}

	if options.MachineOverride != "Dev-Host_01" {
		t.Fatalf("unexpected machine override: %q", options.MachineOverride)
	}

	expectedProfiles := []string{"team", "local"}
	if !reflect.DeepEqual(options.CLIProfiles, expectedProfiles) {
		t.Fatalf("unexpected CLI profiles: got %v want %v", options.CLIProfiles, expectedProfiles)
	}

	if !options.ProfileOnly {
		t.Fatalf("expected profile-only mode to be enabled")
	}

	expectedRemaining := []string{"config", "--json"}
	if !reflect.DeepEqual(remaining, expectedRemaining) {
		t.Fatalf("unexpected remaining args: got %v want %v", remaining, expectedRemaining)
	}
}

func TestConfiguredProfileListSupportsLegacyProfile(t *testing.T) {
	legacy := map[string]any{"profile": "legacy"}
	legacyProfiles := configuredProfileList(legacy)
	if !reflect.DeepEqual(legacyProfiles, []string{"legacy"}) {
		t.Fatalf("legacy profile not recognized: got %v", legacyProfiles)
	}

	modern := map[string]any{"profiles": []any{"team", "local"}}
	modernProfiles := configuredProfileList(modern)
	if !reflect.DeepEqual(modernProfiles, []string{"team", "local"}) {
		t.Fatalf("profiles array mismatch: got %v", modernProfiles)
	}
}

func TestNormalizeMachineIdentifier(t *testing.T) {
	cases := map[string]string{
		"WORKSTATION-01":      "workstation-01",
		"dev laptop.local":    "dev-laptop-local",
		"__invalid__":         "invalid",
		"   MIXED__Case 123 ": "mixed-case-123",
		"":                    "",
	}

	for input, expected := range cases {
		if actual := normalizeMachineIdentifier(input); actual != expected {
			t.Fatalf("normalizeMachineIdentifier(%q) = %q, want %q", input, actual, expected)
		}
	}
}
