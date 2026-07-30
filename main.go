package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

const version = "0.3.0"

type Paths struct {
	ConfigHome string
	DataHome   string
	StateHome  string
}

type ProjectInfo struct {
	Repository         bool   `json:"repository"`
	CurrentDirectory   string `json:"current_directory"`
	ProjectRoot        string `json:"project_root"`
	GitDirectory       string `json:"git_directory,omitempty"`
	CommonGitDirectory string `json:"common_git_directory,omitempty"`
	RemoteURL          string `json:"remote_url,omitempty"`
	ProjectID          string `json:"project_id"`
	IdentitySource     string `json:"identity_source"`
	Branch             string `json:"branch,omitempty"`
	ConfigHome         string `json:"config_home"`
	DataHome           string `json:"data_home"`
	StateHome          string `json:"state_home"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	paths, err := resolvePaths()
	if err != nil {
		die(err)
	}

	command := os.Args[1]

	switch command {
	case "info":
		info, err := resolveProjectInfo(paths)
		if err != nil {
			die(err)
		}
		printProjectInfo(info)

	case "project-id":
		info, err := resolveProjectInfo(paths)
		if err != nil {
			die(err)
		}
		fmt.Println(info.ProjectID)

	case "root":
		info, err := resolveProjectInfo(paths)
		if err != nil {
			die(err)
		}
		fmt.Println(info.ProjectRoot)

	case "config":
		if err := configCommand(paths, os.Args[2:]); err != nil {
			die(err)
		}

	case "env":
		if err := envCommand(paths, os.Args[2:]); err != nil {
			die(err)
		}

	case "config-path":
		info, err := resolveProjectInfo(paths)
		if err != nil {
			die(err)
		}
		fmt.Println(projectConfigPath(paths, info.ProjectID))

	case "doctor":
		if err := doctor(paths); err != nil {
			os.Exit(1)
		}

	case "version":
		fmt.Printf("ai-dev %s\n", version)

	case "help", "-h", "--help":
		usage()

	default:
		die(fmt.Errorf("unknown command: %s", command))
	}
}

func usage() {
	fmt.Print(`Usage:
  ai-dev info
  ai-dev project-id
  ai-dev root
  ai-dev config [--json | --compact]
  ai-dev env [--shell sh]
  ai-dev config-path
  ai-dev doctor
  ai-dev version

Commands:
  info         Print resolved project and Git information
  project-id   Print the stable project identifier
  root         Print the current project root
  config       Print the resolved global and project configuration
  env          Print shell-safe environment exports
  config-path  Print the expected project configuration path
  doctor       Check commands, directories, and configuration files
  version      Print the ai-dev version
`)
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "ai-dev: %v\n", err)
	os.Exit(1)
}

func resolvePaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve home directory: %w", err)
	}

	configBase := environmentOrDefault(
		"XDG_CONFIG_HOME",
		filepath.Join(home, ".config"),
	)
	dataBase := environmentOrDefault(
		"XDG_DATA_HOME",
		filepath.Join(home, ".local", "share"),
	)
	stateBase := environmentOrDefault(
		"XDG_STATE_HOME",
		filepath.Join(home, ".local", "state"),
	)

	return Paths{
		ConfigHome: environmentOrDefault(
			"AI_DEV_CONFIG_HOME",
			filepath.Join(configBase, "ai-dev"),
		),
		DataHome: environmentOrDefault(
			"AI_DEV_DATA_HOME",
			filepath.Join(dataBase, "ai-dev"),
		),
		StateHome: environmentOrDefault(
			"AI_DEV_STATE_HOME",
			filepath.Join(stateBase, "ai-dev"),
		),
	}, nil
}

func environmentOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func resolveProjectInfo(paths Paths) (ProjectInfo, error) {
	currentDirectory, err := os.Getwd()
	if err != nil {
		return ProjectInfo{}, fmt.Errorf("get current directory: %w", err)
	}

	currentDirectory, err = filepath.EvalSymlinks(currentDirectory)
	if err != nil {
		return ProjectInfo{}, fmt.Errorf("resolve current directory: %w", err)
	}

	info := ProjectInfo{
		CurrentDirectory: currentDirectory,
		ConfigHome:       paths.ConfigHome,
		DataHome:         paths.DataHome,
		StateHome:        paths.StateHome,
	}

	if !isGitRepository() {
		info.Repository = false
		info.ProjectRoot = currentDirectory
		info.ProjectID = filesystemProjectID(currentDirectory)
		info.IdentitySource = "filesystem"
		return info, nil
	}

	info.Repository = true

	projectRoot, err := gitOutput("rev-parse", "--show-toplevel")
	if err != nil {
		return ProjectInfo{}, err
	}

	gitDirectory, err := gitOutput("rev-parse", "--absolute-git-dir")
	if err != nil {
		return ProjectInfo{}, err
	}

	commonGitDirectory, err := gitOutput(
		"rev-parse",
		"--path-format=absolute",
		"--git-common-dir",
	)
	if err != nil {
		return ProjectInfo{}, err
	}

	projectRoot, err = canonicalPath(projectRoot)
	if err != nil {
		return ProjectInfo{}, err
	}

	gitDirectory, err = canonicalPath(gitDirectory)
	if err != nil {
		return ProjectInfo{}, err
	}

	commonGitDirectory, err = canonicalPath(commonGitDirectory)
	if err != nil {
		return ProjectInfo{}, err
	}

	remoteURL, _ := gitOutput("config", "--get", "remote.origin.url")
	branch, _ := gitOutput("symbolic-ref", "--quiet", "--short", "HEAD")

	if branch == "" {
		branch = "detached"
	}

	info.ProjectRoot = projectRoot
	info.GitDirectory = gitDirectory
	info.CommonGitDirectory = commonGitDirectory
	info.RemoteURL = remoteURL
	info.Branch = branch

	if remoteURL != "" {
		info.ProjectID = normalizeRemoteURL(remoteURL)
		info.IdentitySource = "remote"
	} else {
		info.ProjectID = filesystemProjectID(commonGitDirectory)
		info.IdentitySource = "git-directory"
	}

	return info, nil
}

func isGitRepository() bool {
	command := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	if command.Run() == nil {
		return true
	}

	command = exec.Command("git", "rev-parse", "--is-bare-repository")
	return command.Run() == nil
}

func gitOutput(arguments ...string) (string, error) {
	command := exec.Command("git", arguments...)
	output, err := command.Output()

	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			message := strings.TrimSpace(string(exitError.Stderr))
			if message != "" {
				return "", fmt.Errorf("git %s: %s", strings.Join(arguments, " "), message)
			}
		}

		return "", fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
	}

	return strings.TrimSpace(string(output)), nil
}

func canonicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path %q: %w", path, err)
	}

	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve path %q: %w", path, err)
	}

	return resolved, nil
}

func normalizeRemoteURL(remote string) string {
	remote = strings.TrimSpace(remote)

	var host string
	var path string

	switch {
	case strings.HasPrefix(remote, "git@") && strings.Contains(remote, ":"):
		value := strings.TrimPrefix(remote, "git@")
		parts := strings.SplitN(value, ":", 2)
		host = parts[0]
		path = parts[1]

	case strings.HasPrefix(remote, "ssh://"):
		value := strings.TrimPrefix(remote, "ssh://")
		value = strings.TrimPrefix(value, "git@")
		parts := strings.SplitN(value, "/", 2)
		host = parts[0]
		if len(parts) == 2 {
			path = parts[1]
		}

	case strings.HasPrefix(remote, "http://") ||
		strings.HasPrefix(remote, "https://"):
		value := remote[strings.Index(remote, "://")+3:]
		if at := strings.LastIndex(value, "@"); at >= 0 {
			value = value[at+1:]
		}
		parts := strings.SplitN(value, "/", 2)
		host = parts[0]
		if len(parts) == 2 {
			path = parts[1]
		}

	case strings.HasPrefix(remote, "file://"):
		host = "local"
		path = strings.TrimPrefix(remote, "file://")

	case filepath.IsAbs(remote):
		host = "local"
		path = remote

	default:
		host = "unknown"
		path = remote
	}

	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")

	if path == "" {
		return host
	}

	return host + "/" + path
}

func filesystemProjectID(path string) string {
	home, _ := os.UserHomeDir()
	value := path

	if home != "" && strings.HasPrefix(path, home) {
		value = "home" + strings.TrimPrefix(path, home)
	}

	invalid := regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	value = invalid.ReplaceAllString(value, "-")

	repeated := regexp.MustCompile(`-+`)
	value = repeated.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")

	return "filesystem/" + value
}

func printProjectInfo(info ProjectInfo) {
	fmt.Printf("repository=%t\n", info.Repository)
	fmt.Printf("current_directory=%s\n", info.CurrentDirectory)
	fmt.Printf("project_root=%s\n", info.ProjectRoot)

	if info.Repository {
		fmt.Printf("git_directory=%s\n", info.GitDirectory)
		fmt.Printf("common_git_directory=%s\n", info.CommonGitDirectory)
		fmt.Printf("remote_url=%s\n", info.RemoteURL)
	}

	fmt.Printf("project_id=%s\n", info.ProjectID)
	fmt.Printf("identity_source=%s\n", info.IdentitySource)

	if info.Repository {
		fmt.Printf("branch=%s\n", info.Branch)
	}

	fmt.Printf("config_home=%s\n", info.ConfigHome)
	fmt.Printf("data_home=%s\n", info.DataHome)
	fmt.Printf("state_home=%s\n", info.StateHome)
}

func configCommand(paths Paths, arguments []string) error {
	compact := false

	for _, argument := range arguments {
		switch argument {
		case "--json":
			// JSON is the default format.
		case "--compact":
			compact = true
		default:
			return fmt.Errorf("unknown config option: %s", argument)
		}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}

	resolved, sources, err := resolveConfiguration(paths, info)
	if err != nil {
		return err
	}

	resolved["project_id"] = info.ProjectID
	resolved["project_root"] = info.ProjectRoot
	resolved["config_sources"] = sources

	var output []byte

	if compact {
		output, err = json.Marshal(resolved)
	} else {
		output, err = json.MarshalIndent(resolved, "", "  ")
	}

	if err != nil {
		return fmt.Errorf("encode resolved configuration: %w", err)
	}

	fmt.Println(string(output))
	return nil
}

func resolveConfiguration(
	paths Paths,
	info ProjectInfo,
) (map[string]any, []string, error) {
	globalPath := filepath.Join(paths.ConfigHome, "global.toml")
	projectPath := projectConfigPath(paths, info.ProjectID)

	resolved := map[string]any{}
	sources := []string{}

	if fileExists(globalPath) {
		globalConfig, err := readTOML(globalPath)
		if err != nil {
			return nil, nil, err
		}
		resolved = mergeMaps(resolved, globalConfig)
		sources = append(sources, globalPath)
	}

	if fileExists(projectPath) {
		projectConfig, err := readTOML(projectPath)
		if err != nil {
			return nil, nil, err
		}
		resolved = mergeMaps(resolved, projectConfig)
		sources = append(sources, projectPath)
	}

	return resolved, sources, nil
}

func projectConfigPath(paths Paths, projectID string) string {
	filename := safeProjectFilename(projectID) + ".toml"
	return filepath.Join(paths.ConfigHome, "projects", filename)
}

func safeProjectFilename(projectID string) string {
	invalid := regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	filename := invalid.ReplaceAllString(projectID, "-")

	repeated := regexp.MustCompile(`-+`)
	filename = repeated.ReplaceAllString(filename, "-")

	return strings.Trim(filename, "-")
}

func readTOML(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read configuration %s: %w", path, err)
	}

	configuration := map[string]any{}

	if err := toml.Unmarshal(content, &configuration); err != nil {
		return nil, fmt.Errorf("parse configuration %s: %w", path, err)
	}

	return configuration, nil
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	result := cloneMap(base)

	for key, overlayValue := range overlay {
		baseValue, exists := result[key]

		if !exists {
			result[key] = cloneValue(overlayValue)
			continue
		}

		result[key] = mergeValues(baseValue, overlayValue)
	}

	return result
}

func mergeValues(base, overlay any) any {
	baseMap, baseIsMap := base.(map[string]any)
	overlayMap, overlayIsMap := overlay.(map[string]any)

	if baseIsMap && overlayIsMap {
		return mergeMaps(baseMap, overlayMap)
	}

	baseArray, baseIsArray := base.([]any)
	overlayArray, overlayIsArray := overlay.([]any)

	if baseIsArray && overlayIsArray {
		return mergeArrays(baseArray, overlayArray)
	}

	return cloneValue(overlay)
}

func mergeArrays(base, overlay []any) []any {
	result := make([]any, 0, len(base)+len(overlay))

	for _, value := range append(base, overlay...) {
		found := false

		for _, existing := range result {
			if reflect.DeepEqual(existing, value) {
				found = true
				break
			}
		}

		if !found {
			result = append(result, cloneValue(value))
		}
	}

	return result
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))

	for key, value := range source {
		result[key] = cloneValue(value)
	}

	return result
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneMap(typed)

	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneValue(item)
		}
		return result

	default:
		return typed
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func doctor(paths Paths) error {
	problems := 0

	fmt.Println("ai-dev doctor")
	fmt.Println()

	if path, err := exec.LookPath("git"); err == nil {
		fmt.Printf("[ok] git: %s\n", path)
	} else {
		fmt.Println("[error] git is unavailable")
		problems++
	}

	directories := []string{
		paths.ConfigHome,
		filepath.Join(paths.ConfigHome, "projects"),
		paths.DataHome,
		paths.StateHome,
	}

	for _, directory := range directories {
		info, err := os.Stat(directory)

		switch {
		case err != nil:
			fmt.Printf("[error] missing directory: %s\n", directory)
			problems++

		case !info.IsDir():
			fmt.Printf("[error] not a directory: %s\n", directory)
			problems++

		default:
			fmt.Printf("[ok] directory: %s\n", directory)
		}
	}

	globalPath := filepath.Join(paths.ConfigHome, "global.toml")

	if fileExists(globalPath) {
		if _, err := readTOML(globalPath); err != nil {
			fmt.Printf("[error] %v\n", err)
			problems++
		} else {
			fmt.Printf("[ok] global configuration: %s\n", globalPath)
		}
	} else {
		fmt.Printf("[notice] global configuration does not exist: %s\n", globalPath)
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		fmt.Printf("[error] project resolution: %v\n", err)
		problems++
	} else {
		fmt.Printf("[ok] project ID: %s\n", info.ProjectID)

		projectPath := projectConfigPath(paths, info.ProjectID)

		if fileExists(projectPath) {
			if _, err := readTOML(projectPath); err != nil {
				fmt.Printf("[error] %v\n", err)
				problems++
			} else {
				fmt.Printf("[ok] project configuration: %s\n", projectPath)
			}
		} else {
			fmt.Printf("[notice] project configuration does not exist: %s\n", projectPath)
		}
	}

	fmt.Println()

	if problems > 0 {
		fmt.Printf("Doctor found %d problem(s).\n", problems)
		return errors.New("doctor checks failed")
	}

	fmt.Println("Everything required for Checkpoint 3 is available.")
	return nil
}

func envCommand(paths Paths, arguments []string) error {
	shell := "sh"

	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]

		switch argument {
		case "--shell":
			if index+1 >= len(arguments) {
				return errors.New("--shell requires a value")
			}

			index++
			shell = arguments[index]

		default:
			return fmt.Errorf("unknown env option: %s", argument)
		}
	}

	if shell != "sh" {
		return fmt.Errorf("unsupported shell: %s", shell)
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}

	resolved, _, err := resolveConfiguration(paths, info)
	if err != nil {
		return err
	}

	environmentValue, exists := resolved["environment"]
	if !exists {
		return nil
	}

	environment, ok := environmentValue.(map[string]any)
	if !ok {
		return errors.New("[environment] must be a TOML table")
	}

	keys := make([]string, 0, len(environment))

	for key := range environment {
		if err := validateEnvironmentName(key); err != nil {
			return err
		}

		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		value, err := environmentStringValue(key, environment[key])
		if err != nil {
			return err
		}

		fmt.Printf("export %s=%s\n", key, shellQuote(value))
	}

	return nil
}

func validateEnvironmentName(name string) error {
	validName := regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

	if !validName.MatchString(name) {
		return fmt.Errorf("invalid environment variable name %q", name)
	}

	return nil
}

func environmentStringValue(name string, value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil

	case bool:
		if typed {
			return "true", nil
		}
		return "false", nil

	case int64:
		return fmt.Sprintf("%d", typed), nil

	case float64:
		return fmt.Sprintf("%v", typed), nil

	default:
		return "", fmt.Errorf(
			"environment variable %s must be a string, boolean, integer, or float",
			name,
		)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
