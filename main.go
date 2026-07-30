package main

import (
	"context"
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

const version = "0.11.0"

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

type UsageError struct {
	Message string
}

func (error UsageError) Error() string {
	return error.Message
}

func main() {
	runtimeOptions, remainingArguments, err := parseRuntimeOptions(os.Args[1:])
	if err != nil {
		die(err)
	}
	if runtimeOptions.MachineOverride != "" {
		if err := validateMachineIdentifierForFlag(runtimeOptions.MachineOverride); err != nil {
			die(err)
		}
	}
	if err := validateExplicitProfiles(runtimeOptions.CLIProfiles); err != nil {
		die(err)
	}
	activeRuntimeOptions = runtimeOptions

	if len(remainingArguments) < 1 {
		usage()
		return
	}

	paths, err := resolvePaths()
	if err != nil {
		die(err)
	}

	command := remainingArguments[0]

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
		if err := configCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "env":
		if err := envCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "validate":
		if err := validateCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "secret":
		if err := secretCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "mcp":
		if err := mcpCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "client":
		if err := clientCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "prompt":
		if err := promptCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "rule":
		if err := ruleCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "profile":
		if err := profileCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "machine":
		if err := machineCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "context":
		if err := contextCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "export":
		if err := exportCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "import":
		if err := importCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "bundle":
		if err := bundleCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "sync":
		if err := syncCommand(paths, remainingArguments[1:]); err != nil {
			die(err)
		}

	case "plugin":
		if err := pluginCommand(paths, remainingArguments[1:]); err != nil {
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
		die(UsageError{Message: fmt.Sprintf("unknown command: %s", command)})
	}
}

func usage() {
	fmt.Print(`Usage:
	ai-dev [--machine <machine-id>] [--profile <profile>]... [--profile-only <profile>]... [--plugin-path <path>]... <command>
  ai-dev info
  ai-dev project-id
  ai-dev root
  ai-dev config [--json | --compact]
  ai-dev env [--shell sh]
	ai-dev validate [--strict] [--json] [--bundle <path>]
  ai-dev secret resolve <reference>
  ai-dev secret check [--json]
	ai-dev mcp list [--enabled] [--json]
	ai-dev mcp show <server-name> [--json]
	ai-dev mcp resolve [--include-disabled] [--resolve-secrets]
	ai-dev mcp check [--json]
	ai-dev client list [--json]
	ai-dev client show <client> [--json]
	ai-dev client path <client> [--scope <scope>] [--json]
	ai-dev client validate <client> [--scope <scope>] [--format <format>] [--strict] [--json]
	ai-dev client generate <client> [--json] [--format <format>] [--scope <scope>] [--include-disabled] [--resolve-secrets] [--with-metadata] [--strict] [--output <path>] [--force]
	ai-dev client compare [--json]
	ai-dev prompt list [--json]
	ai-dev prompt show <identifier> [--json]
	ai-dev prompt search <query> [--json]
	ai-dev prompt resolve [--json]
	ai-dev prompt info [--json]
	ai-dev rule list [--json]
	ai-dev rule show <identifier> [--json]
	ai-dev rule search <query> [--json]
	ai-dev rule resolve [--json]
	ai-dev rule info [--json]
	ai-dev profile list [--json]
	ai-dev profile show <profile> [--json]
	ai-dev profile active [--json]
	ai-dev profile resolve [--with-project] [--json]
	ai-dev machine show [--json]
	ai-dev context [--json]
	ai-dev export [--output <path>] [--project] [--global] [--include-machine] [--include-plugins] [--profiles] [--prompts] [--rules] [--config] [--plugins]
	ai-dev import <bundle> [--dry-run] [--overwrite | --skip-existing | --fail-on-conflict] [--json]
	ai-dev bundle verify <bundle>
	ai-dev bundle show <bundle> [--json]
	ai-dev bundle list [directory] [--json]
	ai-dev bundle metadata <bundle> [--json]
	ai-dev bundle diff <bundle> [--json]
	ai-dev sync preview <bundle> [--overwrite | --skip-existing | --fail-on-conflict] [--json]
	ai-dev sync <bundle> [--overwrite | --skip-existing | --fail-on-conflict] [--json]
	ai-dev plugin list [--json]
	ai-dev plugin show <plugin-id> [--json] [--handshake]
	ai-dev plugin validate [<plugin-id>] [--json]
	ai-dev plugin status [--json]
	ai-dev plugin refresh [--json]
	ai-dev plugin run <plugin-id> <operation> [--capability <name>] [--input <path>] [--json]
	ai-dev config sources [--json]
	ai-dev config origin <field-path> [--json]
  ai-dev config-path
  ai-dev doctor
  ai-dev version

Commands:
  info         Print resolved project and Git information
  project-id   Print the stable project identifier
  root         Print the current project root
  config       Print the resolved global and project configuration
  env          Print shell-safe environment exports
  validate     Validate source and resolved configuration
  secret       Resolve and inspect secret references
	mcp          Inspect and validate resolved MCP registry
	client       Inspect and generate client adapter configurations
	prompt       Inspect and resolve prompt registry resources
	rule         Inspect and resolve rule registry resources
	profile      Inspect and resolve reusable profile overlays
	machine      Inspect machine overlay selection and status
	context      Display active source resolution context
	export       Create a portable configuration bundle
	import       Validate and import a configuration bundle
	bundle       Verify, inspect, and diff configuration bundles
	sync         Preview or apply local bundle synchronization
	plugin       Discover, validate, and invoke external ai-dev plugins
  config-path  Print the expected project configuration path
  doctor       Check commands, directories, and configuration files
  version      Print the ai-dev version
`)
}

func die(err error) {
	fmt.Fprintf(os.Stderr, "ai-dev: %v\n", err)

	var usageError UsageError
	if errors.As(err, &usageError) {
		os.Exit(2)
	}

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
	if len(arguments) > 0 {
		switch arguments[0] {
		case "sources":
			return configSourcesCommand(paths, arguments[1:])
		case "origin":
			return configOriginCommand(paths, arguments[1:])
		}
	}

	compact := false

	for _, argument := range arguments {
		switch argument {
		case "--json":
			// JSON is the default format.
		case "--compact":
			compact = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown config option: %s", argument)}
		}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}

	validation, err := validateConfigurationForProject(paths, info, false)
	if err != nil {
		return err
	}
	if len(validation.Errors) > 0 {
		return configurationValidationError(validation)
	}
	printConfigurationWarnings(validation.Warnings)

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
		fmt.Printf("[ok] global configuration found: %s\n", globalPath)
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
			fmt.Printf("[ok] project configuration found: %s\n", projectPath)
		} else {
			fmt.Printf("[notice] project configuration does not exist: %s\n", projectPath)
		}

		validation, validationErr := validateConfigurationForProject(paths, info, false)
		if validationErr != nil {
			fmt.Printf("[error] configuration validation: %v\n", validationErr)
			problems++
		} else {
			for _, warning := range validation.Warnings {
				fmt.Printf(
					"[notice] deprecated configuration: source=%s path=%s code=%s message=%s\n",
					warning.Source,
					warning.Path,
					warning.Code,
					warning.Message,
				)
			}

			for _, finding := range validation.Errors {
				switch finding.Code {
				case validationCodeUnsupportedSchema:
					fmt.Printf(
						"[error] unsupported schema version: source=%s path=%s code=%s message=%s\n",
						finding.Source,
						finding.Path,
						finding.Code,
						finding.Message,
					)
				case validationCodeConflictingValue:
					fmt.Printf(
						"[error] conflicting configuration: source=%s path=%s code=%s message=%s\n",
						finding.Source,
						finding.Path,
						finding.Code,
						finding.Message,
					)
				default:
					fmt.Printf(
						"[error] invalid configuration: source=%s path=%s code=%s message=%s\n",
						finding.Source,
						finding.Path,
						finding.Code,
						finding.Message,
					)
				}
				problems++
			}

			if len(validation.Errors) == 0 {
				if len(validation.Warnings) == 0 {
					fmt.Println("[ok] configuration validation: valid")
				} else {
					fmt.Println("[ok] configuration validation: valid (with warnings)")
				}

				resolved, _, err := resolveConfiguration(paths, info)
				if err != nil {
					fmt.Printf("[error] secret resolution context: %v\n", err)
					problems++
				} else {
					loadedSources, _, _ := loadConfigurationSources(paths, info)
					mcpSummary := collectMCPDoctorSummary(resolved, loadedSources)
					fmt.Printf(
						"[ok] mcp summary: configured=%d enabled=%d invalid_definitions=%d unavailable_executables=%d invalid_working_directories=%d unresolved_secret_references=%d unsupported_transports=%d\n",
						mcpSummary.ConfiguredServers,
						mcpSummary.EnabledServers,
						mcpSummary.InvalidDefinitions,
						mcpSummary.UnavailableExecutables,
						mcpSummary.InvalidWorkingDirectory,
						mcpSummary.UnresolvedSecrets,
						mcpSummary.UnsupportedTransports,
					)
					problems += mcpSummary.InvalidDefinitions
					problems += mcpSummary.UnavailableExecutables
					problems += mcpSummary.InvalidWorkingDirectory
					problems += mcpSummary.UnresolvedSecrets
					problems += mcpSummary.UnsupportedTransports

					for _, line := range clientDoctorSummary(paths, info, resolved, loadedSources) {
						fmt.Println(line)
						if strings.HasPrefix(line, "[error]") {
							problems++
						}
					}

					for _, line := range registryDoctorLines(paths, info, resolved, loadedSources) {
						fmt.Println(line)
						if strings.HasPrefix(line, "[error]") {
							problems++
						}
					}

					for _, line := range profileMachineDoctorLines(paths, info) {
						fmt.Println(line)
						if strings.HasPrefix(line, "[error]") {
							problems++
						}
					}

					pluginStatus, pluginErr := pluginStatus(paths, true)
					if pluginErr != nil {
						fmt.Printf("[error] plugins: %v\n", pluginErr)
						problems++
					} else {
						fmt.Printf(
							"[ok] plugins summary: discovered=%d enabled=%d compatible=%d invalid=%d conflicts=%d handshake_failures=%d\n",
							pluginStatus.DiscoveredPlugins,
							pluginStatus.EnabledPlugins,
							pluginStatus.CompatiblePlugins,
							pluginStatus.InvalidPlugins,
							pluginStatus.CapabilityConflicts,
							pluginStatus.HandshakeFailures,
						)
						for _, finding := range pluginStatus.Findings {
							prefix := "[notice]"
							if finding.Severity == "error" {
								prefix = "[error]"
								problems++
							}
							fmt.Printf("%s plugin=%s capability=%s operation=%s code=%s message=%s\n", prefix, finding.PluginID, finding.Capability, finding.Operation, finding.Code, finding.Message)
						}
					}

					for _, line := range bundleDoctorLines(paths) {
						fmt.Println(line)
						if strings.HasPrefix(line, "[error]") {
							problems++
						}
					}

					resolver := newProjectSecretResolver(paths, loadSecretCommandDefinitions(resolved))
					results, err := secretCheckResults(context.Background(), resolved, resolver)
					if err != nil {
						fmt.Printf("[error] secret provider checks: %v\n", err)
						problems++
					} else if len(results) == 0 {
						fmt.Println("[notice] secret references: none configured")
					} else {
						allResolved := true
						for _, result := range results {
							if result.Resolved {
								fmt.Printf(
									"[ok] secret reference: provider=%s reference=%s\n",
									result.Provider,
									result.Reference,
								)
								continue
							}

							allResolved = false
							fmt.Printf(
								"[error] secret reference: provider=%s reference=%s message=%s\n",
								result.Provider,
								result.Reference,
								result.Error,
							)
							problems++
						}

						if allResolved {
							fmt.Println("[ok] secret references: all resolvable")
						}
					}
				}
			}
		}
	}

	fmt.Println()

	if problems > 0 {
		fmt.Printf("Doctor found %d problem(s).\n", problems)
		return errors.New("doctor checks failed")
	}

	fmt.Println("Everything required for Checkpoint 11 is available.")
	return nil
}

func envCommand(paths Paths, arguments []string) error {
	shell := "sh"

	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]

		switch argument {
		case "--shell":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--shell requires a value"}
			}

			index++
			shell = arguments[index]

		default:
			return UsageError{Message: fmt.Sprintf("unknown env option: %s", argument)}
		}
	}

	if shell != "sh" {
		return UsageError{Message: fmt.Sprintf("unsupported shell: %s", shell)}
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}

	validation, err := validateConfigurationForProject(paths, info, false)
	if err != nil {
		return err
	}
	if len(validation.Errors) > 0 {
		return configurationValidationError(validation)
	}
	printConfigurationWarnings(validation.Warnings)

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

	resolver := newProjectSecretResolver(paths, loadSecretCommandDefinitions(resolved))
	values, err := resolveEnvironmentValues(context.Background(), environment, resolver)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("export %s=%s", key, shellQuote(values[key])))
	}

	for _, line := range lines {
		fmt.Println(line)
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

func configurationValidationError(report ValidationReport) error {
	if len(report.Errors) == 0 {
		return errors.New("configuration validation failed")
	}

	first := report.Errors[0]
	return fmt.Errorf(
		"configuration validation failed: source=%s path=%s code=%s",
		first.Source,
		first.Path,
		first.Code,
	)
}

func printConfigurationWarnings(warnings []ValidationFinding) {
	for _, warning := range warnings {
		fmt.Fprintf(
			os.Stderr,
			"ai-dev: configuration warning: source=%s path=%s code=%s message=%s\n",
			warning.Source,
			warning.Path,
			warning.Code,
			warning.Message,
		)
	}
}

func validateCommand(paths Paths, arguments []string) error {
	strict := false
	jsonOutput := false
	bundlePath := ""

	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--strict":
			strict = true
		case "--json":
			jsonOutput = true
		case "--bundle":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--bundle requires a value"}
			}
			index++
			bundlePath = arguments[index]
		default:
			return UsageError{Message: fmt.Sprintf("unknown validate option: %s", argument)}
		}
	}

	if bundlePath != "" {
		findings := []ValidationFinding{}
		archive, err := readBundleArchive(bundlePath)
		if err != nil {
			findings = append(findings, ValidationFinding{Source: bundlePath, Path: "$", Code: bundleCodeInvalid, Severity: "error", Message: err.Error()})
		} else if err := verifyBundleArchive(archive); err != nil {
			code := bundleCodeInvalid
			var typed bundleError
			if errors.As(err, &typed) {
				code = typed.Code
			}
			findings = append(findings, ValidationFinding{Source: bundlePath, Path: "$", Code: code, Severity: "error", Message: err.Error()})
		}

		report := ValidationReport{Valid: len(findings) == 0, Errors: findings, Warnings: []ValidationFinding{}, Sources: []string{bundlePath}}
		if jsonOutput {
			output, err := validationOutputJSON(report)
			if err != nil {
				return err
			}
			fmt.Println(output)
		} else {
			for _, source := range report.Sources {
				fmt.Printf("source=%s\n", source)
			}
			for _, finding := range report.Errors {
				fmt.Printf("[error] source=%s path=%s code=%s message=%s\n", finding.Source, finding.Path, finding.Code, finding.Message)
			}
			fmt.Printf("valid=%t\n", report.Valid)
		}
		if report.Valid {
			return nil
		}
		return errors.New("validation failed")
	}

	report, err := validateConfigurationForCurrentProject(paths, strict)
	if err != nil {
		return err
	}

	if jsonOutput {
		output, err := validationOutputJSON(report)
		if err != nil {
			return err
		}
		fmt.Println(output)
	} else {
		for _, source := range report.Sources {
			fmt.Printf("source=%s\n", source)
		}

		for _, warning := range report.Warnings {
			fmt.Printf(
				"[warning] source=%s path=%s code=%s message=%s\n",
				warning.Source,
				warning.Path,
				warning.Code,
				warning.Message,
			)
		}

		for _, finding := range report.Errors {
			fmt.Printf(
				"[error] source=%s path=%s code=%s message=%s\n",
				finding.Source,
				finding.Path,
				finding.Code,
				finding.Message,
			)
		}

		if report.Valid {
			fmt.Println("valid=true")
		} else {
			fmt.Println("valid=false")
		}
	}

	if report.Valid {
		return nil
	}

	return errors.New("validation failed")
}

func secretCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "secret requires a subcommand"}
	}

	switch arguments[0] {
	case "resolve":
		if len(arguments) != 2 {
			return UsageError{Message: "secret resolve requires exactly one reference"}
		}
		return secretResolveCommand(paths, arguments[1])

	case "check":
		jsonOutput := false
		for _, argument := range arguments[1:] {
			switch argument {
			case "--json":
				jsonOutput = true
			default:
				return UsageError{Message: fmt.Sprintf("unknown secret check option: %s", argument)}
			}
		}
		return secretCheckCommand(paths, jsonOutput)

	default:
		return UsageError{Message: fmt.Sprintf("unknown secret subcommand: %s", arguments[0])}
	}
}

func secretResolveCommand(paths Paths, rawReference string) error {
	reference, err := parseSecretReference(rawReference)
	if err != nil {
		return err
	}

	ctx := context.Background()
	if reference.Provider == secretProviderEnv {
		resolver := newProjectSecretResolver(paths, map[string]SecretCommandDefinition{})
		value, err := resolver.Resolve(ctx, reference)
		if err != nil {
			return err
		}
		fmt.Println(value)
		return nil
	}

	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}

	resolved, _, err := resolveConfiguration(paths, info)
	if err != nil {
		return err
	}

	resolver := newProjectSecretResolver(paths, loadSecretCommandDefinitions(resolved))
	value, err := resolver.Resolve(ctx, reference)
	if err != nil {
		return err
	}
	fmt.Println(value)
	return nil
}

func secretCheckCommand(paths Paths, jsonOutput bool) error {
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return err
	}

	resolved, _, err := resolveConfiguration(paths, info)
	if err != nil {
		return err
	}

	resolver := newProjectSecretResolver(paths, loadSecretCommandDefinitions(resolved))
	results, err := secretCheckResults(context.Background(), resolved, resolver)
	if err != nil {
		return err
	}

	valid := true
	for _, result := range results {
		if !result.Resolved {
			valid = false
			break
		}
	}

	if jsonOutput {
		output, err := secretResolutionJSON(results, valid)
		if err != nil {
			return err
		}
		fmt.Println(output)
	} else {
		for _, result := range results {
			if result.Resolved {
				fmt.Printf("[ok] secret provider=%s reference=%s\n", result.Provider, result.Reference)
			} else {
				fmt.Printf("[error] secret provider=%s reference=%s message=%s\n", result.Provider, result.Reference, result.Error)
			}
		}
		fmt.Printf("valid=%t\n", valid)
	}

	if valid {
		return nil
	}
	return errors.New("secret resolution failed")
}
