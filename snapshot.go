package main

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// aiSnapshotFiles makes the repository snapshots part of every ai-dev build.
// The Markdown files remain the source of truth; no generated duplicate is used.
//
//go:embed snapshots/*.md
var aiSnapshotFiles embed.FS

var aiPublicSnapshotBaseURL = "https://raw.githubusercontent.com/ricardoferreirades/ai-dev/main"
var aiSnapshotHTTPClient = &http.Client{Timeout: 10 * time.Second}

type aiSnapshotEntry struct {
	Path      string
	Directory bool
}

type aiSnapshotRoot struct {
	Path    string
	Scope   string
	Entries []aiSnapshotEntry
}

type aiSnapshotDefinition struct {
	Path    string
	Content string
	Roots   []aiSnapshotRoot
	Source  string
	Warning string
}

func aiSnapshotPathForClient(clientName string) string {
	name := strings.ToLower(strings.TrimSpace(clientName))
	if name == clientNameVSCode {
		name = "copilot"
	}
	return filepath.ToSlash(filepath.Join("snapshots", name+"-snapshot.md"))
}

func aiLoadSnapshotDefinition(clientName string) (aiSnapshotDefinition, error) {
	snapshotPath := aiSnapshotPathForClient(clientName)
	data, err := aiSnapshotFiles.ReadFile(snapshotPath)
	if err != nil {
		return aiSnapshotDefinition{}, fmt.Errorf("snapshot update required: %s: %w", snapshotPath, err)
	}
	return aiParseSnapshotDefinition(snapshotPath, string(data), "embedded")
}

func aiResolveSnapshot(paths Paths, clientName string) (aiSnapshotDefinition, error) {
	snapshotPath := aiSnapshotPathForClient(clientName)
	cachePath := filepath.Join(paths.ConfigHome, "clients", clientName, filepath.Base(snapshotPath))
	if data, err := os.ReadFile(cachePath); err == nil {
		return aiParseSnapshotDefinition(snapshotPath, string(data), "cache")
	} else if !os.IsNotExist(err) {
		return aiSnapshotDefinition{}, fmt.Errorf("read local snapshot %s: %w", cachePath, err)
	}

	publicURL := strings.TrimRight(aiPublicSnapshotBaseURL, "/") + "/" + snapshotPath
	request, err := http.NewRequest(http.MethodGet, publicURL, nil)
	if err == nil {
		request.Header.Set("User-Agent", "ai-dev-snapshot-resolver")
		response, requestErr := aiSnapshotHTTPClient.Do(request)
		if requestErr == nil {
			defer response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 300 {
				data, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
				if readErr == nil {
					return aiParseSnapshotDefinition(snapshotPath, string(data), "public")
				}
				err = readErr
			} else {
				err = fmt.Errorf("HTTP %s", response.Status)
			}
		} else {
			err = requestErr
		}
	}

	embedded, embeddedErr := aiLoadSnapshotDefinition(clientName)
	if embeddedErr != nil {
		return aiSnapshotDefinition{}, fmt.Errorf("snapshot update required: %s | public fetch failed: %v | embedded fallback failed: %w", snapshotPath, err, embeddedErr)
	}
	embedded.Warning = fmt.Sprintf("public snapshot unavailable (%v); using embedded fallback", err)
	return embedded, nil
}

func aiParseSnapshotDefinition(snapshotPath, content, source string) (aiSnapshotDefinition, error) {
	definition := aiSnapshotDefinition{Path: snapshotPath, Content: content, Source: source}
	scanner := bufio.NewScanner(strings.NewReader(definition.Content))
	inTree := false
	var root *aiSnapshotRoot
	stack := []string{}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inTree && root != nil {
				definition.Roots = append(definition.Roots, *root)
			}
			inTree = trimmed == "```text" && !inTree
			root = nil
			stack = nil
			continue
		}
		if !inTree || trimmed == "" {
			continue
		}
		if root == nil {
			path := aiSnapshotStripComment(trimmed)
			if !strings.HasSuffix(path, "/") {
				// A one-file block such as ~/.claude.json is documented but cannot
				// serve as the root of a managed hierarchy.
				continue
			}
			root = &aiSnapshotRoot{Path: strings.TrimSuffix(path, "/"), Scope: aiSnapshotScope(path)}
			continue
		}
		branch := strings.Index(line, "├── ")
		if branch < 0 {
			branch = strings.Index(line, "└── ")
		}
		if branch < 0 {
			continue
		}
		prefix := line[:branch]
		depth := 1
		for {
			switch {
			case strings.HasPrefix(prefix, "│   "):
				prefix = strings.TrimPrefix(prefix, "│   ")
				depth++
			case strings.HasPrefix(prefix, "    "):
				prefix = strings.TrimPrefix(prefix, "    ")
				depth++
			default:
				goto depthDone
			}
		}
	depthDone:
		name := aiSnapshotStripComment(line[branch+len("├── "):])
		if name == "" {
			continue
		}
		directory := strings.HasSuffix(name, "/")
		name = strings.TrimSuffix(name, "/")
		if depth-1 < len(stack) {
			stack = stack[:depth-1]
		}
		parts := append(append([]string{}, stack...), name)
		root.Entries = append(root.Entries, aiSnapshotEntry{Path: filepath.ToSlash(filepath.Join(parts...)), Directory: directory})
		if directory {
			if len(stack) == depth-1 {
				stack = append(stack, name)
			} else {
				stack[depth-1] = name
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return aiSnapshotDefinition{}, err
	}
	if len(definition.Roots) == 0 {
		return aiSnapshotDefinition{}, fmt.Errorf("snapshot update required: %s: no directory trees found", snapshotPath)
	}
	return definition, nil
}

func aiSnapshotStripComment(line string) string {
	if before, _, found := strings.Cut(line, "#"); found {
		line = before
	}
	return strings.TrimSpace(line)
}

func aiSnapshotScope(path string) string {
	if strings.HasPrefix(path, "~/") {
		return "user"
	}
	return "project"
}

func aiValidateSnapshotLayout(snapshot aiSnapshotDefinition, layout map[string]any, target string) error {
	root := aiLayoutString(layout, "root", "")
	if root == "" {
		return nil
	}
	paths := []string{}
	for _, key := range []string{"instruction_files", "instructions", "prompt_files", "rule_files", "agent_files", "skill_files", "mcp_files"} {
		paths = append(paths, aiLayoutStringSlice(layout, key)...)
	}
	invalid := []string{}
	for _, path := range paths {
		if !aiSnapshotAllowsLayoutPath(snapshot, target, root, path) {
			invalid = append(invalid, filepath.ToSlash(filepath.Join(root, path)))
		}
	}
	if len(invalid) > 0 {
		sort.Strings(invalid)
		return fmt.Errorf("snapshot update required: %s | undocumented layout: %s", snapshot.Path, strings.Join(invalid, ", "))
	}
	return nil
}

func aiSnapshotAllowsLayoutPath(snapshot aiSnapshotDefinition, target, configuredRoot, relative string) bool {
	configuredRoot = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(configuredRoot)), "/")
	relative = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(relative)), "./")
	for _, root := range snapshot.Roots {
		if root.Scope != target || !aiSnapshotRootsEquivalent(root.Path, configuredRoot, target) {
			continue
		}
		if aiSnapshotRootAllows(root, relative) {
			return true
		}
	}
	return false
}

func aiSnapshotRootsEquivalent(snapshotRoot, configuredRoot, target string) bool {
	if target == "user" {
		return snapshotRoot == configuredRoot
	}
	snapshotRoot = strings.TrimPrefix(snapshotRoot, "project/")
	configuredRoot = strings.TrimPrefix(configuredRoot, "project/")
	if configuredRoot == "project" || configuredRoot == "." {
		configuredRoot = ""
	}
	return snapshotRoot == configuredRoot || (snapshotRoot == "project" && configuredRoot == "")
}

func aiSnapshotRootAllows(root aiSnapshotRoot, relative string) bool {
	for _, entry := range root.Entries {
		if aiSnapshotPathMatches(entry.Path, relative) {
			return true
		}
	}
	return false
}

func aiSnapshotPathMatches(pattern, path string) bool {
	quoted := regexp.QuoteMeta(filepath.ToSlash(pattern))
	placeholder := regexp.MustCompile(`<[^>]+>`)
	quoted = placeholder.ReplaceAllString(quoted, `[^/]+`)
	matched, _ := regexp.MatchString("^"+quoted+"$", filepath.ToSlash(path))
	return matched
}

func aiCheckSnapshotAlignment(snapshot aiSnapshotDefinition, target, root string, actual []string) error {
	invalid := []string{}
	for _, path := range actual {
		allowed := false
		for _, snapshotRoot := range snapshot.Roots {
			if snapshotRoot.Scope != target {
				continue
			}
			absoluteRoot, err := aiSnapshotAbsoluteRoot(snapshotRoot.Path, aiSnapshotProjectRoot(root, target))
			if err != nil {
				continue
			}
			relative, err := filepath.Rel(absoluteRoot, path)
			if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && aiSnapshotRootAllows(snapshotRoot, relative) {
				allowed = true
				break
			}
		}
		if !allowed {
			invalid = append(invalid, filepath.ToSlash(path))
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	sort.Strings(invalid)
	return fmt.Errorf("snapshot update required: %s | undocumented generated files: %s", snapshot.Path, strings.Join(invalid, ", "))
}

func aiSnapshotProjectRoot(root, target string) string {
	if target == "project" {
		return root
	}
	return ""
}

func aiSnapshotDirectories(snapshot aiSnapshotDefinition, target string, projectRoot string) []string {
	directories := []string{}
	for _, root := range snapshot.Roots {
		if root.Scope != target {
			continue
		}
		absoluteRoot, err := aiSnapshotAbsoluteRoot(root.Path, projectRoot)
		if err != nil {
			continue
		}
		directories = append(directories, absoluteRoot)
		for _, entry := range root.Entries {
			if entry.Directory && !strings.Contains(entry.Path, "<") && !strings.Contains(entry.Path, ">") {
				directories = append(directories, filepath.Join(absoluteRoot, filepath.FromSlash(entry.Path)))
			}
		}
	}
	return uniqueStrings(directories)
}

func aiSnapshotPlanDirectories(snapshot aiSnapshotDefinition, target, projectRoot string, files []string) []string {
	directories := aiSnapshotDirectories(snapshot, target, projectRoot)
	for _, path := range files {
		directories = append(directories, filepath.Dir(path))
	}
	return uniqueStrings(directories)
}

func aiSnapshotAbsoluteRoot(root, projectRoot string) (string, error) {
	if strings.HasPrefix(root, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, filepath.FromSlash(strings.TrimPrefix(root, "~/"))), nil
	}
	root = strings.TrimPrefix(root, "project/")
	if root == "project" || root == "" {
		return projectRoot, nil
	}
	return filepath.Join(projectRoot, filepath.FromSlash(root)), nil
}

func aiSnapshotConfiguredRoot(root, target string) string {
	if target == "project" {
		return "project"
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if relative, relErr := filepath.Rel(home, root); relErr == nil && !strings.HasPrefix(relative, "..") {
			return "~/" + filepath.ToSlash(relative)
		}
	}
	return filepath.ToSlash(root)
}

func aiCacheSnapshot(paths Paths, clientName string, snapshot aiSnapshotDefinition) error {
	directory := filepath.Join(paths.ConfigHome, "clients", clientName)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, filepath.Base(snapshot.Path)), []byte(snapshot.Content), 0o600)
}
