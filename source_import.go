package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

type sourceImportOptions struct {
	DryRun bool
	Force  bool
	JSON   bool
	Name   string
}

type sourceImportFile struct {
	SourcePath string `json:"source_path"`
	TargetPath string `json:"target_path"`
	Category   string `json:"category"`
	Action     string `json:"action"`
}

type sourceImportReport struct {
	Source       string             `json:"source"`
	SourceRoot   string             `json:"source_root"`
	ImportName   string             `json:"import_name"`
	DryRun       bool               `json:"dry_run"`
	Files        []sourceImportFile `json:"files"`
	Categories   []string           `json:"categories"`
	ManifestPath string             `json:"manifest_path"`
}

func sourceImportRequested(source string) bool {
	if info, err := os.Stat(source); err == nil {
		return info.IsDir()
	}
	return isSourceRepositoryURL(source)
}

func isSourceRepositoryURL(source string) bool {
	if strings.HasPrefix(source, "git@") || strings.HasPrefix(source, "ssh://") || strings.HasPrefix(source, "git://") {
		return true
	}
	parsed, err := url.Parse(source)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func importSourceCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "import requires a repository or directory"}
	}
	source := arguments[0]
	options := sourceImportOptions{}
	for index := 1; index < len(arguments); index++ {
		switch arguments[index] {
		case "--dry-run":
			options.DryRun = true
		case "--force":
			options.Force = true
		case "--json":
			options.JSON = true
		case "--name":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--name requires a value"}
			}
			index++
			options.Name = arguments[index]
		default:
			return UsageError{Message: fmt.Sprintf("unknown source import option: %s", arguments[index])}
		}
	}

	root, cleanup, err := resolveSourceImportRoot(source)
	if err != nil {
		return err
	}
	defer cleanup()

	name := options.Name
	if name == "" {
		name = sourceImportName(source, root)
	}
	name, err = sanitizeSourceImportName(name)
	if err != nil {
		return err
	}

	files, categories, err := discoverSourceImportFiles(root, paths, name)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no supported AI development files found in %s", source)
	}

	manifestPath := filepath.Join(paths.ConfigHome, "imports", name, "import.json")
	report := sourceImportReport{
		Source:       source,
		SourceRoot:   root,
		ImportName:   name,
		DryRun:       options.DryRun,
		Files:        files,
		Categories:   categories,
		ManifestPath: manifestPath,
	}
	if !options.Force {
		for index := range report.Files {
			if fileExists(report.Files[index].TargetPath) {
				report.Files[index].Action = "conflict"
			}
		}
		for _, file := range report.Files {
			if file.Action == "conflict" {
				return fmt.Errorf("source import has existing files; retry with --force (first conflict: %s)", file.TargetPath)
			}
		}
	}
	for index := range report.Files {
		if report.Files[index].Action == "" {
			report.Files[index].Action = "create"
		} else if options.Force {
			report.Files[index].Action = "update"
		}
	}

	if !options.DryRun {
		for _, file := range report.Files {
			content, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.SourcePath)))
			if readErr != nil {
				return fmt.Errorf("read source file %s: %w", file.SourcePath, readErr)
			}
			if err := writeImportedSourceFile(file.TargetPath, content); err != nil {
				return err
			}
		}
		manifest := map[string]any{
			"schema":        "source-import-v1",
			"source":        source,
			"import_name":   name,
			"imported_at":   time.Now().UTC().Format(time.RFC3339),
			"categories":    categories,
			"source_root":   root,
			"managed_files": report.Files,
		}
		content, marshalErr := json.MarshalIndent(manifest, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode source import manifest: %w", marshalErr)
		}
		if err := writeImportedSourceFile(manifestPath, append(content, '\n')); err != nil {
			return err
		}
		if err := enableImportedRegistries(paths, report.Files); err != nil {
			return err
		}
	}

	if options.JSON {
		content, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode source import report: %w", marshalErr)
		}
		fmt.Println(string(content))
	} else {
		fmt.Printf("imported_source=%s files=%d categories=%s dry_run=%t\n", source, len(report.Files), strings.Join(categories, ","), options.DryRun)
		for _, file := range report.Files {
			fmt.Printf("%s category=%s source=%s target=%s\n", file.Action, file.Category, file.SourcePath, file.TargetPath)
		}
	}
	return nil
}

func enableImportedRegistries(paths Paths, files []sourceImportFile) error {
	identifiers := map[string][]string{"prompts": {}, "rules": {}}
	for _, file := range files {
		if file.Category != "prompt" && file.Category != "rule" {
			continue
		}
		root := filepath.Join(paths.ConfigHome, file.Category+"s")
		relative, err := filepath.Rel(root, file.TargetPath)
		if err != nil || strings.HasPrefix(relative, "..") {
			return fmt.Errorf("resolve imported %s identifier: %s", file.Category, file.TargetPath)
		}
		identifier := strings.TrimSuffix(filepath.ToSlash(relative), filepath.Ext(relative))
		identifiers[file.Category+"s"] = append(identifiers[file.Category+"s"], identifier)
	}
	if len(identifiers["prompts"]) == 0 && len(identifiers["rules"]) == 0 {
		return nil
	}

	globalPath := filepath.Join(paths.ConfigHome, "global.toml")
	configuration := map[string]any{"schema": "v1"}
	if data, err := os.ReadFile(globalPath); err == nil {
		if err := toml.Unmarshal(data, &configuration); err != nil {
			return fmt.Errorf("read global configuration for source import: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read global configuration for source import: %w", err)
	}
	for sectionName, values := range identifiers {
		if len(values) == 0 {
			continue
		}
		section, ok := configuration[sectionName].(map[string]any)
		if !ok {
			section = map[string]any{}
			configuration[sectionName] = section
		}
		enabled := stringValues(section["enabled"])
		for _, value := range values {
			if !containsSourceString(enabled, value) {
				enabled = append(enabled, value)
			}
		}
		section["enabled"] = enabled
	}
	content, err := toml.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("encode global configuration for source import: %w", err)
	}
	return writeImportedSourceFile(globalPath, content)
}

func stringValues(value any) []string {
	result := []string{}
	switch values := value.(type) {
	case []any:
		for _, item := range values {
			if text, ok := item.(string); ok {
				result = append(result, text)
			}
		}
	case []string:
		result = append(result, values...)
	}
	return result
}

func containsSourceString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func resolveSourceImportRoot(source string) (string, func(), error) {
	if info, err := os.Stat(source); err == nil {
		if !info.IsDir() {
			return "", func() {}, fmt.Errorf("source is not a directory: %s", source)
		}
		root, err := filepath.Abs(source)
		return root, func() {}, err
	}
	if !isSourceRepositoryURL(source) {
		return "", func() {}, fmt.Errorf("source directory does not exist: %s", source)
	}
	temporary, err := os.MkdirTemp("", "ai-dev-import-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temporary source directory: %w", err)
	}
	command := exec.Command("git", "clone", "--depth", "1", source, temporary)
	if output, err := command.CombinedOutput(); err != nil {
		_ = os.RemoveAll(temporary)
		return "", func() {}, fmt.Errorf("clone source repository: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return temporary, func() { _ = os.RemoveAll(temporary) }, nil
}

func sourceImportName(source, root string) string {
	if isSourceRepositoryURL(source) {
		if strings.HasPrefix(source, "git@") {
			name := filepath.Base(strings.TrimSuffix(strings.SplitN(source, ":", 2)[1], "/"))
			return strings.TrimSuffix(name, ".git")
		}
		parsed, err := url.Parse(source)
		if err == nil && parsed.Path != "" {
			name := filepath.Base(strings.TrimSuffix(parsed.Path, "/"))
			return strings.TrimSuffix(name, ".git")
		}
	}
	return filepath.Base(root)
}

func sanitizeSourceImportName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return "", UsageError{Message: "import name must be a non-empty single directory name"}
	}
	return name, nil
}

func discoverSourceImportFiles(root string, paths Paths, name string) ([]sourceImportFile, []string, error) {
	files := []sourceImportFile{}
	categorySet := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && skippedSourceImportDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		category, ok := classifySourceImportFile(relative)
		if !ok {
			return nil
		}
		categorySet[category] = true
		destination := filepath.Join(paths.ConfigHome, "imports", name, filepath.FromSlash(relative))
		if category == "prompt" || category == "rule" {
			destination = filepath.Join(paths.ConfigHome, category+"s", "imports", name, filepath.FromSlash(strings.TrimPrefix(relative, category+"s/")))
		}
		files = append(files, sourceImportFile{SourcePath: relative, TargetPath: destination, Category: category})
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("discover source files: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].SourcePath < files[j].SourcePath })
	categories := make([]string, 0, len(categorySet))
	for category := range categorySet {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	return files, categories, nil
}

func skippedSourceImportDirectory(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", "node_modules", "vendor", ".next", "dist", "build", "coverage":
		return true
	default:
		return false
	}
}

func classifySourceImportFile(relative string) (string, bool) {
	parts := strings.Split(relative, "/")
	base := parts[len(parts)-1]
	top := strings.ToLower(parts[0])
	switch top {
	case "rules":
		return "rule", true
	case "prompts":
		return "prompt", true
	case "instructions":
		return "instruction", true
	case "agents":
		return "agent", true
	case "skills":
		return "skill", true
	case "mcp", "mcps":
		return "mcp", true
	case "policies":
		return "policy", true
	case ".claude", ".codex", ".cursor", ".github", ".vscode", ".ai-dev":
		return "client", true
	}
	lower := strings.ToLower(base)
	if lower == "agents.md" || lower == "claude.md" || lower == "codex.md" || lower == "gemini.md" || lower == "copilot-instructions.md" || lower == ".cursorrules" || lower == ".clinerules" {
		return "instruction", true
	}
	if strings.HasSuffix(lower, ".instructions.md") {
		return "instruction", true
	}
	if strings.HasSuffix(lower, ".agent.md") {
		return "agent", true
	}
	if strings.EqualFold(base, "SKILL.md") {
		return "skill", true
	}
	return "", false
}

func writeImportedSourceFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("prepare imported file %s: %w", path, err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write imported file %s: %w", path, err)
	}
	return nil
}
