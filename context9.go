package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

type profileListEntry struct {
	Identifier string `json:"identifier"`
	Path       string `json:"path"`
	Valid      bool   `json:"valid"`
	Active     bool   `json:"active"`
}

type configSourceOutput struct {
	Type       string `json:"source_type"`
	Identifier string `json:"identifier"`
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Valid      bool   `json:"valid"`
	Precedence int    `json:"precedence"`
	SelectedBy string `json:"selected_by,omitempty"`
}

func profileCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "profile requires a subcommand"}
	}
	switch arguments[0] {
	case "list":
		return profileListCommand(paths, arguments[1:])
	case "show":
		return profileShowCommand(paths, arguments[1:])
	case "active":
		return profileActiveCommand(paths, arguments[1:])
	case "resolve":
		return profileResolveCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown profile subcommand: %s", arguments[0])}
	}
}

func machineCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "machine requires a subcommand"}
	}
	switch arguments[0] {
	case "show":
		return machineShowCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown machine subcommand: %s", arguments[0])}
	}
}

func contextCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown context option: %s", argument)}
		}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	ctx, err := validateContextModel(paths, info)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"project_id":          info.ProjectID,
		"project_root":        info.ProjectRoot,
		"repository":          info.Repository,
		"worktree":            info.Repository && info.CommonGitDirectory != info.GitDirectory,
		"branch":              info.Branch,
		"machine_source":      ctx.MachineRawSource,
		"machine_raw":         ctx.MachineRawIdentifier,
		"machine_id":          ctx.MachineIdentifier,
		"machine_overlay":     ctx.MachineOverlayPath,
		"machine_exists":      ctx.MachineOverlayExists,
		"active_profiles":     ctx.ActiveProfiles,
		"merge_order_sources": buildConfigSourceOutputs(ctx.Sources),
	}

	if jsonOutput {
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode context JSON: %w", err)
		}
		fmt.Println(string(encoded))
		return nil
	}

	fmt.Printf("project_id=%s project_root=%s repository=%t worktree=%t branch=%s\n", info.ProjectID, info.ProjectRoot, info.Repository, info.Repository && info.CommonGitDirectory != info.GitDirectory, info.Branch)
	fmt.Printf("machine_source=%s machine_raw=%s machine_id=%s machine_overlay=%s machine_exists=%t\n", ctx.MachineRawSource, ctx.MachineRawIdentifier, ctx.MachineIdentifier, ctx.MachineOverlayPath, ctx.MachineOverlayExists)
	for _, profile := range ctx.ActiveProfiles {
		fmt.Printf("active_profile=%s selected_by=%s path=%s\n", profile.Identifier, profile.SelectedBy, profile.Path)
	}
	for _, source := range buildConfigSourceOutputs(ctx.Sources) {
		fmt.Printf("source precedence=%d type=%s identifier=%s path=%s exists=%t valid=%t selected_by=%s\n", source.Precedence, source.Type, source.Identifier, source.Path, source.Exists, source.Valid, source.SelectedBy)
	}
	return nil
}

func profileListCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown profile list option: %s", argument)}
		}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	ctx, err := validateContextModel(paths, info)
	if err != nil {
		return err
	}

	profilesDir := filepath.Join(paths.ConfigHome, "profiles")
	entries := []profileListEntry{}
	files := []string{}
	_ = filepath.WalkDir(profilesDir, func(path string, entryDir fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entryDir.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) == ".toml" {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)

	active := map[string]bool{}
	for _, profile := range ctx.ActiveProfiles {
		active[profile.Identifier] = true
	}

	for _, path := range files {
		identifier := strings.TrimSuffix(filepath.Base(path), ".toml")
		entry := profileListEntry{Identifier: identifier, Path: path, Valid: true, Active: active[identifier]}
		if err := validateProfileIdentifier(identifier); err != nil {
			entry.Valid = false
		} else if config, err := readTOML(path); err != nil {
			entry.Valid = false
		} else if err := validateProfileDefinition(config); err != nil {
			entry.Valid = false
		}
		entries = append(entries, entry)
	}

	if jsonOutput {
		encoded, err := json.MarshalIndent(map[string]any{"profiles": entries}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode profile list JSON: %w", err)
		}
		fmt.Println(string(encoded))
		return nil
	}

	for _, entry := range entries {
		fmt.Printf("profile=%s source=%s valid=%t active=%t\n", entry.Identifier, entry.Path, entry.Valid, entry.Active)
	}
	return nil
}

func profileShowCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "profile show requires a profile identifier"}
	}
	identifier := arguments[0]
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown profile show option: %s", argument)}
		}
	}

	if err := validateProfileIdentifier(identifier); err != nil {
		return err
	}
	path := filepath.Join(paths.ConfigHome, "profiles", identifier+".toml")
	if !fileExists(path) {
		return registryOrMachineError(profileCodeNotFound, fmt.Sprintf("profile %q does not exist", identifier))
	}
	config, err := readTOML(path)
	if err != nil {
		return registryOrMachineError(profileCodeInvalidProfile, "profile could not be parsed")
	}
	if err := validateProfileDefinition(config); err != nil {
		return err
	}

	payload := map[string]any{"identifier": identifier, "path": path, "config": config}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profile show output: %w", err)
	}
	if jsonOutput {
		fmt.Println(string(encoded))
		return nil
	}
	fmt.Println(string(encoded))
	return nil
}

func profileActiveCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown profile active option: %s", argument)}
		}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	ctx, err := validateContextModel(paths, info)
	if err != nil {
		return err
	}

	if jsonOutput {
		encoded, err := json.MarshalIndent(map[string]any{"active_profiles": ctx.ActiveProfiles, "duplicate_references": ctx.DuplicateProfiles}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode profile active JSON: %w", err)
		}
		fmt.Println(string(encoded))
		return nil
	}

	for _, profile := range ctx.ActiveProfiles {
		fmt.Printf("profile=%s selected_by=%s source=%s\n", profile.Identifier, profile.SelectedBy, profile.Path)
	}
	for _, duplicate := range ctx.DuplicateProfiles {
		fmt.Printf("[warning] code=%s profile=%s selected_by=%s\n", profileCodeDuplicateReference, duplicate.Identifier, duplicate.SelectedBy)
	}
	return nil
}

func profileResolveCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	includeProject := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		case "--with-project":
			includeProject = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown profile resolve option: %s", argument)}
		}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	ctx, err := resolveConfigurationWithContext(paths, info, includeProject)
	if err != nil {
		return err
	}
	if err := ensureNoSourceErrors(ctx); err != nil {
		return err
	}

	if jsonOutput {
		encoded, err := json.MarshalIndent(ctx.Resolved, "", "  ")
		if err != nil {
			return fmt.Errorf("encode profile resolve JSON: %w", err)
		}
		fmt.Println(string(encoded))
		return nil
	}

	encoded, err := json.MarshalIndent(ctx.Resolved, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profile resolve output: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

func machineShowCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown machine show option: %s", argument)}
		}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	ctx, err := validateContextModel(paths, info)
	if err != nil {
		return err
	}

	valid := ctx.MachineIdentifier != ""
	payload := map[string]any{
		"raw_source":            ctx.MachineRawSource,
		"raw_identifier":        ctx.MachineRawIdentifier,
		"normalized_identifier": ctx.MachineIdentifier,
		"overlay_path":          ctx.MachineOverlayPath,
		"overlay_exists":        ctx.MachineOverlayExists,
		"valid":                 valid,
	}

	if jsonOutput {
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode machine show JSON: %w", err)
		}
		fmt.Println(string(encoded))
		return nil
	}

	fmt.Printf("raw_source=%s raw_identifier=%s machine_id=%s overlay_path=%s overlay_exists=%t valid=%t\n", ctx.MachineRawSource, ctx.MachineRawIdentifier, ctx.MachineIdentifier, ctx.MachineOverlayPath, ctx.MachineOverlayExists, valid)
	return nil
}

func configSourcesCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown config sources option: %s", argument)}
		}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	ctx, err := validateContextModel(paths, info)
	if err != nil {
		return err
	}

	entries := buildConfigSourceOutputs(ctx.Sources)
	if jsonOutput {
		encoded, err := json.MarshalIndent(map[string]any{"sources": entries}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode config sources JSON: %w", err)
		}
		fmt.Println(string(encoded))
		return nil
	}

	for _, entry := range entries {
		fmt.Printf("precedence=%d type=%s identifier=%s path=%s exists=%t valid=%t selected_by=%s\n", entry.Precedence, entry.Type, entry.Identifier, entry.Path, entry.Exists, entry.Valid, entry.SelectedBy)
	}
	return nil
}

func buildConfigSourceOutputs(sources []appliedSource) []configSourceOutput {
	entries := make([]configSourceOutput, 0, len(sources))
	for _, source := range sources {
		entries = append(entries, configSourceOutput{
			Type:       source.Type,
			Identifier: source.Identifier,
			Path:       source.Path,
			Exists:     source.Exists,
			Valid:      source.ParseError == nil,
			Precedence: source.Precedence,
			SelectedBy: source.SelectedBy,
		})
	}
	return entries
}

func configOriginCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "config origin requires a field path"}
	}
	fieldPath := arguments[0]
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown config origin option: %s", argument)}
		}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}
	ctx, err := validateContextModel(paths, info)
	if err != nil {
		return err
	}

	type contribution struct {
		SourceType string `json:"source_type"`
		Identifier string `json:"identifier"`
		Path       string `json:"path"`
		Action     string `json:"action"`
		Value      any    `json:"value,omitempty"`
	}

	contributions := []contribution{}
	var previous any
	finalFound := false
	for _, source := range ctx.Sources {
		if source.ParseError != nil {
			continue
		}
		value, exists := fieldPathValue(source.Config, fieldPath)
		if !exists {
			continue
		}
		action := mergeAction(previous, value)
		contributions = append(contributions, contribution{
			SourceType: source.Type,
			Identifier: source.Identifier,
			Path:       source.Path,
			Action:     action,
			Value:      sanitizeOriginValue(fieldPath, cloneValue(value)),
		})
		previous = cloneValue(value)
		finalFound = true
	}

	if !finalFound {
		return registryOrMachineError(configCodeOriginNotFound, fmt.Sprintf("field path %q does not exist in resolved configuration", fieldPath))
	}

	finalValue, _ := fieldPathValue(ctx.Resolved, fieldPath)
	payload := map[string]any{
		"field_path":    fieldPath,
		"final_source":  contributions[len(contributions)-1],
		"final_value":   sanitizeOriginValue(fieldPath, cloneValue(finalValue)),
		"contributions": contributions,
		"merge_order":   buildConfigSourceOutputs(ctx.Sources),
	}

	if jsonOutput {
		encoded, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode config origin JSON: %w", err)
		}
		fmt.Println(string(encoded))
		return nil
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config origin output: %w", err)
	}
	fmt.Println(string(encoded))
	return nil
}

func profileMachineDoctorLines(paths Paths, info ProjectInfo) []string {
	lines := []string{}
	ctx, err := validateContextModel(paths, info)
	if err != nil {
		lines = append(lines, fmt.Sprintf("[error] context resolution: %v", err))
		return lines
	}

	profilesDir := filepath.Join(paths.ConfigHome, "profiles")
	machinesDir := filepath.Join(paths.ConfigHome, "machines")
	profileCount := 0
	_ = filepath.WalkDir(profilesDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) == ".toml" {
			profileCount++
		}
		return nil
	})

	invalidProfiles := 0
	missingProfiles := 0
	for _, source := range ctx.Sources {
		if source.Type != sourceTypeProfile && source.Type != sourceTypeCLIProfile {
			continue
		}
		if source.ParseError == nil {
			continue
		}
		var registryErr registryError
		if errors.As(source.ParseError, &registryErr) && registryErr.Code == profileCodeNotFound {
			missingProfiles++
		} else {
			invalidProfiles++
		}
	}

	status := "ok"
	if invalidProfiles > 0 || missingProfiles > 0 {
		status = "error"
	}
	lines = append(lines, fmt.Sprintf("[%s] profiles: dir=%s count=%d active=%d missing_references=%d invalid=%d", status, profilesDir, profileCount, len(ctx.ActiveProfiles), missingProfiles, invalidProfiles))

	machineStatus := "ok"
	if ctx.MachineIdentifier == "" {
		machineStatus = "error"
	}
	invalidOverlay := false
	for _, source := range ctx.Sources {
		if source.Type == sourceTypeMachine && source.ParseError != nil {
			invalidOverlay = true
			machineStatus = "error"
		}
	}
	lines = append(lines, fmt.Sprintf("[%s] machine: source=%s id=%s overlay_path=%s overlay_exists=%t overlay_valid=%t machines_dir=%s", machineStatus, ctx.MachineRawSource, ctx.MachineIdentifier, ctx.MachineOverlayPath, ctx.MachineOverlayExists, !invalidOverlay, machinesDir))

	if len(ctx.DuplicateProfiles) > 0 {
		lines = append(lines, fmt.Sprintf("[notice] profiles: duplicate_references=%d code=%s", len(ctx.DuplicateProfiles), profileCodeDuplicateReference))
	}

	return lines
}
