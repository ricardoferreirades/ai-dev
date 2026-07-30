package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	bundleSchemaV1      = "bundle-v1"
	bundleFileExtension = ".aidev"
)

const (
	bundleCodeNotFound          = "bundle_not_found"
	bundleCodeInvalid           = "invalid_bundle"
	bundleCodeUnsupportedSchema = "unsupported_bundle_schema"
	bundleCodeChecksumFailed    = "bundle_checksum_failed"
	bundleCodeManifestInvalid   = "bundle_manifest_invalid"
	bundleCodeImportFailed      = "bundle_import_failed"
	bundleCodeExportFailed      = "bundle_export_failed"
	bundleCodeConflict          = "bundle_conflict"
	bundleCodeResourceInvalid   = "bundle_resource_invalid"
	bundleCodeResourceDuplicate = "bundle_resource_duplicate"
	bundleCodeSyncFailed        = "bundle_sync_failed"
)

const (
	bundleConflictPolicyOverwrite    = "overwrite"
	bundleConflictPolicySkipExisting = "skip-existing"
	bundleConflictPolicyFail         = "fail-on-conflict"
)

type bundleError struct {
	Code    string
	Message string
}

func (err bundleError) Error() string {
	if err.Code == "" {
		return err.Message
	}
	return fmt.Sprintf("code=%s %s", err.Code, err.Message)
}

type bundleManifest struct {
	Schema            string            `json:"schema"`
	BundleVersion     string            `json:"bundle_version"`
	CreatedAt         string            `json:"created_at"`
	CreatorVersion    string            `json:"creator_version"`
	OriginPlatform    string            `json:"origin_platform"`
	ProjectIdentifier string            `json:"project_identifier,omitempty"`
	Resources         []bundleResource  `json:"resources"`
	Checksums         map[string]string `json:"checksums"`
}

type bundleResource struct {
	Path       string `json:"path"`
	Type       string `json:"type"`
	Checksum   string `json:"checksum"`
	Size       int64  `json:"size"`
	Provenance string `json:"provenance,omitempty"`
}

type bundleArchive struct {
	Manifest  bundleManifest
	Resources map[string][]byte
}

type bundleExportOptions struct {
	Output         string
	IncludeGlobal  bool
	IncludeProject bool
	IncludeMachine bool
	IncludePlugins bool
	SelectProfiles bool
	SelectPrompts  bool
	SelectRules    bool
	SelectConfig   bool
	SelectPlugins  bool
}

type bundleImportOptions struct {
	DryRun         bool
	ConflictPolicy string
	JSONOutput     bool
}

type bundleImportAction struct {
	ResourcePath string `json:"resource_path"`
	TargetPath   string `json:"target_path"`
	Action       string `json:"action"`
	Conflict     bool   `json:"conflict"`
	Reason       string `json:"reason,omitempty"`
}

type bundleImportReport struct {
	Creates   []bundleImportAction `json:"creates"`
	Updates   []bundleImportAction `json:"updates"`
	Skipped   []bundleImportAction `json:"skipped"`
	Conflicts []bundleImportAction `json:"conflicts"`
}

type bundleDiffEntry struct {
	ResourcePath string `json:"resource_path"`
	TargetPath   string `json:"target_path"`
	Status       string `json:"status"`
}

func exportCommand(paths Paths, arguments []string) error {
	options := bundleExportOptions{
		IncludeGlobal:  true,
		IncludeProject: true,
	}

	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--output":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--output requires a value"}
			}
			index++
			options.Output = arguments[index]
		case "--project":
			options.IncludeProject = true
		case "--global":
			options.IncludeGlobal = true
		case "--include-machine":
			options.IncludeMachine = true
		case "--include-plugins":
			options.IncludePlugins = true
		case "--profiles":
			options.SelectProfiles = true
		case "--prompts":
			options.SelectPrompts = true
		case "--rules":
			options.SelectRules = true
		case "--config":
			options.SelectConfig = true
		case "--plugins":
			options.SelectPlugins = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown export option: %s", argument)}
		}
	}

	if options.Output == "" {
		options.Output = defaultBundlePath()
	}

	archive, err := buildBundleArchive(paths, options)
	if err != nil {
		return err
	}
	if err := writeBundleArchive(options.Output, archive); err != nil {
		return err
	}
	if err := writeBundleLastStatus(paths, true, "export succeeded"); err != nil {
		return err
	}

	fmt.Printf("bundle=%s resources=%d schema=%s\n", options.Output, len(archive.Manifest.Resources), archive.Manifest.Schema)
	return nil
}

func importCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "import requires a bundle path"}
	}
	bundlePath := arguments[0]
	options := bundleImportOptions{
		ConflictPolicy: bundleConflictPolicyFail,
	}

	policySet := 0
	for _, argument := range arguments[1:] {
		switch argument {
		case "--dry-run":
			options.DryRun = true
		case "--overwrite":
			options.ConflictPolicy = bundleConflictPolicyOverwrite
			policySet++
		case "--skip-existing":
			options.ConflictPolicy = bundleConflictPolicySkipExisting
			policySet++
		case "--fail-on-conflict":
			options.ConflictPolicy = bundleConflictPolicyFail
			policySet++
		case "--json":
			options.JSONOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown import option: %s", argument)}
		}
	}
	if policySet > 1 {
		return UsageError{Message: "exactly one conflict policy may be specified"}
	}

	archive, err := readBundleArchive(bundlePath)
	if err != nil {
		_ = writeBundleLastStatus(paths, false, err.Error())
		return err
	}
	if err := verifyBundleArchive(archive); err != nil {
		_ = writeBundleLastStatus(paths, false, err.Error())
		return err
	}

	report, operations, err := planBundleImport(paths, archive, options)
	if err != nil {
		if options.DryRun {
			var typed bundleError
			if errors.As(err, &typed) && typed.Code == bundleCodeConflict {
				operations = []bundleImportAction{}
			} else {
				_ = writeBundleLastStatus(paths, false, err.Error())
				return err
			}
		} else {
			_ = writeBundleLastStatus(paths, false, err.Error())
			return err
		}
	}

	if options.JSONOutput {
		content, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			return fmt.Errorf("encode import report JSON: %w", marshalErr)
		}
		fmt.Println(string(content))
	} else {
		printBundleImportReport(report)
	}

	if options.DryRun {
		return nil
	}

	if err := applyBundleImportAtomically(paths, archive, operations); err != nil {
		_ = writeBundleLastStatus(paths, false, err.Error())
		return err
	}
	if err := writeBundleLastStatus(paths, true, "import succeeded"); err != nil {
		return err
	}
	if err := writeBundleProvenance(paths, archive.Manifest, operations); err != nil {
		return err
	}
	return nil
}

func bundleCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle requires a subcommand"}
	}
	switch arguments[0] {
	case "verify":
		return bundleVerifyCommand(paths, arguments[1:])
	case "show":
		return bundleShowCommand(arguments[1:])
	case "list":
		return bundleListCommand(arguments[1:])
	case "metadata":
		return bundleMetadataCommand(arguments[1:])
	case "diff":
		return bundleDiffCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown bundle subcommand: %s", arguments[0])}
	}
}

func syncCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "sync requires a subcommand or bundle path"}
	}
	if arguments[0] == "preview" {
		if len(arguments) < 2 {
			return UsageError{Message: "sync preview requires a bundle path"}
		}
		args := []string{arguments[1], "--dry-run"}
		args = append(args, arguments[2:]...)
		if err := importCommand(paths, args); err != nil {
			return bundleError{Code: bundleCodeSyncFailed, Message: err.Error()}
		}
		return nil
	}
	if err := importCommand(paths, arguments); err != nil {
		return bundleError{Code: bundleCodeSyncFailed, Message: err.Error()}
	}
	return nil
}

func bundleVerifyCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle verify requires a bundle path"}
	}
	archive, err := readBundleArchive(arguments[0])
	if err != nil {
		_ = writeBundleLastStatus(paths, false, err.Error())
		return err
	}
	if err := verifyBundleArchive(archive); err != nil {
		_ = writeBundleLastStatus(paths, false, err.Error())
		return err
	}
	if err := writeBundleLastStatus(paths, true, "verify succeeded"); err != nil {
		return err
	}
	fmt.Printf("valid=true schema=%s resources=%d\n", archive.Manifest.Schema, len(archive.Manifest.Resources))
	return nil
}

func bundleShowCommand(arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle show requires a bundle path"}
	}
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown bundle show option: %s", argument)}
		}
	}
	archive, err := readBundleArchive(arguments[0])
	if err != nil {
		return err
	}
	if err := verifyBundleArchive(archive); err != nil {
		return err
	}
	counts := map[string]int{}
	for _, resource := range archive.Manifest.Resources {
		counts[resource.Type]++
	}
	payload := map[string]any{
		"manifest":        archive.Manifest,
		"resource_counts": counts,
		"checksum_count":  len(archive.Manifest.Checksums),
	}
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bundle show output: %w", err)
	}
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func bundleMetadataCommand(arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle metadata requires a bundle path"}
	}
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown bundle metadata option: %s", argument)}
		}
	}
	archive, err := readBundleArchive(arguments[0])
	if err != nil {
		return err
	}
	metadata := map[string]any{
		"schema":             archive.Manifest.Schema,
		"bundle_version":     archive.Manifest.BundleVersion,
		"created_at":         archive.Manifest.CreatedAt,
		"creator_version":    archive.Manifest.CreatorVersion,
		"origin_platform":    archive.Manifest.OriginPlatform,
		"project_identifier": archive.Manifest.ProjectIdentifier,
	}
	content, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bundle metadata output: %w", err)
	}
	if jsonOutput {
		fmt.Println(string(content))
		return nil
	}
	fmt.Println(string(content))
	return nil
}

func bundleListCommand(arguments []string) error {
	directory := "."
	jsonOutput := false
	for _, argument := range arguments {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			if strings.HasPrefix(argument, "--") {
				return UsageError{Message: fmt.Sprintf("unknown bundle list option: %s", argument)}
			}
			directory = argument
		}
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		return bundleError{Code: bundleCodeNotFound, Message: fmt.Sprintf("bundle directory not found: %s", directory)}
	}
	bundles := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), bundleFileExtension) {
			bundles = append(bundles, filepath.Join(directory, entry.Name()))
		}
	}
	sort.Strings(bundles)

	if jsonOutput {
		content, err := json.MarshalIndent(map[string]any{"bundles": bundles}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode bundle list output: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}
	for _, bundle := range bundles {
		fmt.Println(bundle)
	}
	return nil
}

func bundleDiffCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "bundle diff requires a bundle path"}
	}
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown bundle diff option: %s", argument)}
		}
	}
	archive, err := readBundleArchive(arguments[0])
	if err != nil {
		return err
	}
	if err := verifyBundleArchive(archive); err != nil {
		return err
	}
	diff := []bundleDiffEntry{}
	for _, resource := range archive.Manifest.Resources {
		targetPath, mapErr := bundleResourceTargetPath(paths, resource.Path)
		if mapErr != nil {
			diff = append(diff, bundleDiffEntry{ResourcePath: resource.Path, TargetPath: "", Status: "invalid"})
			continue
		}
		current, err := os.ReadFile(targetPath)
		if errors.Is(err, os.ErrNotExist) {
			diff = append(diff, bundleDiffEntry{ResourcePath: resource.Path, TargetPath: targetPath, Status: "addition"})
			continue
		}
		if err != nil {
			diff = append(diff, bundleDiffEntry{ResourcePath: resource.Path, TargetPath: targetPath, Status: "invalid"})
			continue
		}
		bundled := archive.Resources[resource.Path]
		if checksumForContent(current) != checksumForContent(bundled) {
			diff = append(diff, bundleDiffEntry{ResourcePath: resource.Path, TargetPath: targetPath, Status: "modification"})
		} else {
			diff = append(diff, bundleDiffEntry{ResourcePath: resource.Path, TargetPath: targetPath, Status: "unchanged"})
		}
	}

	if jsonOutput {
		content, err := json.MarshalIndent(map[string]any{"diff": diff}, "", "  ")
		if err != nil {
			return fmt.Errorf("encode bundle diff JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}
	for _, entry := range diff {
		fmt.Printf("resource=%s target=%s status=%s\n", entry.ResourcePath, entry.TargetPath, entry.Status)
	}
	return nil
}

func buildBundleArchive(paths Paths, options bundleExportOptions) (bundleArchive, error) {
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return bundleArchive{}, bundleError{Code: bundleCodeExportFailed, Message: err.Error()}
	}

	selectionConfigured := options.SelectProfiles || options.SelectPrompts || options.SelectRules || options.SelectConfig || options.SelectPlugins
	includeConfig := options.SelectConfig || !selectionConfigured
	includeProfiles := options.SelectProfiles || !selectionConfigured
	includePrompts := options.SelectPrompts || !selectionConfigured
	includeRules := options.SelectRules || !selectionConfigured
	includePlugins := options.SelectPlugins || options.IncludePlugins

	resources := map[string][]byte{}
	resourceTypes := map[string]string{}

	addFile := func(resourcePath string, filePath string, resourceType string) error {
		content, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("read resource %s: %v", filePath, readErr)}
		}
		resources[resourcePath] = content
		resourceTypes[resourcePath] = resourceType
		return nil
	}

	if includeConfig {
		if options.IncludeGlobal {
			globalPath := filepath.Join(paths.ConfigHome, "global.toml")
			if fileExists(globalPath) {
				if err := addFile("config/global.toml", globalPath, "global-config"); err != nil {
					return bundleArchive{}, err
				}
			}
		}
		if options.IncludeProject {
			projectPath := projectConfigPath(paths, info.ProjectID)
			if fileExists(projectPath) {
				if err := addFile("config/projects/"+filepath.Base(projectPath), projectPath, "project-config"); err != nil {
					return bundleArchive{}, err
				}
			}
		}
	}

	if includeProfiles {
		if err := addDirectoryFiles("profiles", filepath.Join(paths.ConfigHome, "profiles"), ".toml", resources, resourceTypes, "profile"); err != nil {
			return bundleArchive{}, err
		}
	}

	if includePrompts {
		if err := addDirectoryFiles("prompts", filepath.Join(paths.ConfigHome, "prompts"), "", resources, resourceTypes, "prompt"); err != nil {
			return bundleArchive{}, err
		}
	}

	if includeRules {
		if err := addDirectoryFiles("rules", filepath.Join(paths.ConfigHome, "rules"), "", resources, resourceTypes, "rule"); err != nil {
			return bundleArchive{}, err
		}
	}

	if options.IncludeMachine {
		if err := addDirectoryFiles("machines", filepath.Join(paths.ConfigHome, "machines"), ".toml", resources, resourceTypes, "machine-overlay"); err != nil {
			return bundleArchive{}, err
		}
	}

	if includePlugins {
		pluginResources, pluginErr := discoverPluginManifestFiles(paths)
		if pluginErr != nil {
			return bundleArchive{}, pluginErr
		}
		for resourcePath, sourcePath := range pluginResources {
			if err := addFile(resourcePath, sourcePath, "plugin-manifest"); err != nil {
				return bundleArchive{}, err
			}
		}
	}

	resourcePaths := bundleResourceKeys(resources)
	sort.Strings(resourcePaths)

	manifest := bundleManifest{
		Schema:            bundleSchemaV1,
		BundleVersion:     "1.0.0",
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		CreatorVersion:    version,
		OriginPlatform:    runtime.GOOS + "/" + runtime.GOARCH,
		ProjectIdentifier: info.ProjectID,
		Resources:         []bundleResource{},
		Checksums:         map[string]string{},
	}
	for _, resourcePath := range resourcePaths {
		checksum := checksumForContent(resources[resourcePath])
		manifest.Checksums[resourcePath] = checksum
		manifest.Resources = append(manifest.Resources, bundleResource{
			Path:       resourcePath,
			Type:       resourceTypes[resourcePath],
			Checksum:   checksum,
			Size:       int64(len(resources[resourcePath])),
			Provenance: "local-export",
		})
	}
	sort.Strings(resourcePaths)
	return bundleArchive{Manifest: manifest, Resources: resources}, nil
}

func discoverPluginManifestFiles(paths Paths) (map[string]string, error) {
	files := map[string]string{}
	discovery, err := discoverPluginsForCurrentInvocation(paths)
	if err != nil {
		return files, bundleError{Code: bundleCodeExportFailed, Message: err.Error()}
	}
	for _, plugin := range sortDiscoveredPlugins(discovery.Plugins) {
		if plugin.Manifest.ID == "" || !fileExists(plugin.ManifestPath) {
			continue
		}
		resourcePath := filepath.ToSlash(filepath.Join("plugins", plugin.Manifest.ID, "plugin.toml"))
		files[resourcePath] = plugin.ManifestPath
	}
	return files, nil
}

func addDirectoryFiles(prefix string, directory string, extension string, resources map[string][]byte, resourceTypes map[string]string, resourceType string) error {
	info, err := os.Stat(directory)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("read directory %s: %v", directory, err)}
	}
	if !info.IsDir() {
		return nil
	}
	files := []string{}
	walkErr := filepath.WalkDir(directory, func(path string, entry os.DirEntry, entryErr error) error {
		if entryErr != nil {
			return entryErr
		}
		if entry.IsDir() {
			return nil
		}
		if extension != "" && strings.ToLower(filepath.Ext(path)) != extension {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("walk directory %s: %v", directory, walkErr)}
	}
	sort.Strings(files)
	for _, path := range files {
		relative, relErr := filepath.Rel(directory, path)
		if relErr != nil {
			return bundleError{Code: bundleCodeExportFailed, Message: relErr.Error()}
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return bundleError{Code: bundleCodeExportFailed, Message: readErr.Error()}
		}
		resourcePath := filepath.ToSlash(filepath.Join(prefix, relative))
		resources[resourcePath] = content
		resourceTypes[resourcePath] = resourceType
	}
	return nil
}

func writeBundleArchive(path string, archive bundleArchive) error {
	if !strings.HasSuffix(path, bundleFileExtension) {
		path += bundleFileExtension
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("prepare output directory: %v", err)}
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), ".aidev-export-*.tmp")
	if err != nil {
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("create temporary bundle: %v", err)}
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	writer := zip.NewWriter(tempFile)
	manifestBytes, err := json.MarshalIndent(archive.Manifest, "", "  ")
	if err != nil {
		_ = tempFile.Close()
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("encode bundle manifest: %v", err)}
	}

	if err := writeZipEntry(writer, "manifest.json", manifestBytes); err != nil {
		_ = writer.Close()
		_ = tempFile.Close()
		return err
	}

	resourcePaths := bundleResourceKeys(archive.Resources)
	sort.Strings(resourcePaths)
	for _, resourcePath := range resourcePaths {
		entryPath := filepath.ToSlash(filepath.Join("resources", resourcePath))
		if err := writeZipEntry(writer, entryPath, archive.Resources[resourcePath]); err != nil {
			_ = writer.Close()
			_ = tempFile.Close()
			return err
		}
	}

	if err := writer.Close(); err != nil {
		_ = tempFile.Close()
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("finalize bundle archive: %v", err)}
	}
	if err := tempFile.Close(); err != nil {
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("close bundle archive: %v", err)}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("move bundle archive: %v", err)}
	}
	return nil
}

func writeZipEntry(writer *zip.Writer, path string, content []byte) error {
	header := &zip.FileHeader{Name: path, Method: zip.Deflate}
	header.SetModTime(time.Unix(0, 0).UTC())
	entryWriter, err := writer.CreateHeader(header)
	if err != nil {
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("create archive entry %s: %v", path, err)}
	}
	if _, err := entryWriter.Write(content); err != nil {
		return bundleError{Code: bundleCodeExportFailed, Message: fmt.Sprintf("write archive entry %s: %v", path, err)}
	}
	return nil
}

func readBundleArchive(path string) (bundleArchive, error) {
	if !fileExists(path) {
		return bundleArchive{}, bundleError{Code: bundleCodeNotFound, Message: fmt.Sprintf("bundle not found: %s", path)}
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return bundleArchive{}, bundleError{Code: bundleCodeInvalid, Message: fmt.Sprintf("open bundle archive: %v", err)}
	}
	defer func() {
		_ = reader.Close()
	}()

	archive := bundleArchive{Resources: map[string][]byte{}}
	for _, file := range reader.File {
		content, readErr := readZipFile(file)
		if readErr != nil {
			return bundleArchive{}, bundleError{Code: bundleCodeInvalid, Message: readErr.Error()}
		}
		if file.Name == "manifest.json" {
			if err := json.Unmarshal(content, &archive.Manifest); err != nil {
				return bundleArchive{}, bundleError{Code: bundleCodeManifestInvalid, Message: "bundle manifest is invalid JSON"}
			}
			continue
		}
		if strings.HasPrefix(file.Name, "resources/") {
			resourcePath := strings.TrimPrefix(file.Name, "resources/")
			archive.Resources[resourcePath] = content
		}
	}
	if archive.Manifest.Schema == "" {
		return bundleArchive{}, bundleError{Code: bundleCodeManifestInvalid, Message: "bundle manifest is missing"}
	}
	return archive, nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open archive entry %s: %w", file.Name, err)
	}
	defer func() {
		_ = reader.Close()
	}()
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read archive entry %s: %w", file.Name, err)
	}
	return content, nil
}

func verifyBundleArchive(archive bundleArchive) error {
	manifest := archive.Manifest
	if manifest.Schema != bundleSchemaV1 {
		return bundleError{Code: bundleCodeUnsupportedSchema, Message: fmt.Sprintf("unsupported bundle schema %q", manifest.Schema)}
	}
	if manifest.BundleVersion == "" || manifest.CreatorVersion == "" || manifest.CreatedAt == "" {
		return bundleError{Code: bundleCodeManifestInvalid, Message: "bundle manifest is missing required metadata"}
	}
	if len(manifest.Resources) == 0 {
		return bundleError{Code: bundleCodeManifestInvalid, Message: "bundle manifest has no resources"}
	}

	seenPaths := map[string]bool{}
	for _, resource := range manifest.Resources {
		if strings.TrimSpace(resource.Path) == "" {
			return bundleError{Code: bundleCodeManifestInvalid, Message: "bundle manifest contains empty resource path"}
		}
		if seenPaths[resource.Path] {
			return bundleError{Code: bundleCodeResourceDuplicate, Message: fmt.Sprintf("duplicate resource path %s", resource.Path)}
		}
		seenPaths[resource.Path] = true
		content, exists := archive.Resources[resource.Path]
		if !exists {
			return bundleError{Code: bundleCodeResourceInvalid, Message: fmt.Sprintf("resource %s is missing from bundle", resource.Path)}
		}
		checksum := checksumForContent(content)
		if resource.Checksum != "" && checksum != resource.Checksum {
			return bundleError{Code: bundleCodeChecksumFailed, Message: fmt.Sprintf("resource checksum mismatch for %s", resource.Path)}
		}
		manifestChecksum, exists := manifest.Checksums[resource.Path]
		if !exists {
			return bundleError{Code: bundleCodeManifestInvalid, Message: fmt.Sprintf("manifest checksum is missing for %s", resource.Path)}
		}
		if checksum != manifestChecksum {
			return bundleError{Code: bundleCodeChecksumFailed, Message: fmt.Sprintf("manifest checksum mismatch for %s", resource.Path)}
		}
	}

	for checksumPath := range manifest.Checksums {
		if !seenPaths[checksumPath] {
			return bundleError{Code: bundleCodeManifestInvalid, Message: fmt.Sprintf("manifest checksum references unknown resource %s", checksumPath)}
		}
	}
	return nil
}

func planBundleImport(paths Paths, archive bundleArchive, options bundleImportOptions) (bundleImportReport, []bundleImportAction, error) {
	report := bundleImportReport{Creates: []bundleImportAction{}, Updates: []bundleImportAction{}, Skipped: []bundleImportAction{}, Conflicts: []bundleImportAction{}}
	operations := []bundleImportAction{}
	for _, resource := range archive.Manifest.Resources {
		targetPath, err := bundleResourceTargetPath(paths, resource.Path)
		if err != nil {
			return report, nil, err
		}
		exists := fileExists(targetPath)
		action := bundleImportAction{ResourcePath: resource.Path, TargetPath: targetPath}
		if !exists {
			action.Action = "create"
			report.Creates = append(report.Creates, action)
			operations = append(operations, action)
			continue
		}

		if options.ConflictPolicy == bundleConflictPolicySkipExisting {
			action.Action = "skip"
			action.Reason = "existing resource"
			action.Conflict = true
			report.Skipped = append(report.Skipped, action)
			continue
		}
		if options.ConflictPolicy == bundleConflictPolicyFail {
			action.Action = "conflict"
			action.Reason = "existing resource"
			action.Conflict = true
			report.Conflicts = append(report.Conflicts, action)
			continue
		}
		action.Action = "update"
		action.Conflict = true
		report.Updates = append(report.Updates, action)
		operations = append(operations, action)
	}

	if len(report.Conflicts) > 0 {
		return report, nil, bundleError{Code: bundleCodeConflict, Message: "bundle import has conflicts; retry with conflict policy"}
	}
	return report, operations, nil
}

func applyBundleImportAtomically(paths Paths, archive bundleArchive, operations []bundleImportAction) error {
	type backupEntry struct {
		Path    string
		Content []byte
		Existed bool
	}
	backups := []backupEntry{}
	created := []string{}

	rollback := func() {
		for _, path := range created {
			_ = os.Remove(path)
		}
		for _, entry := range backups {
			if !entry.Existed {
				_ = os.Remove(entry.Path)
				continue
			}
			_ = os.MkdirAll(filepath.Dir(entry.Path), 0o755)
			_ = os.WriteFile(entry.Path, entry.Content, 0o600)
		}
	}

	for _, operation := range operations {
		targetPath := operation.TargetPath
		content := archive.Resources[operation.ResourcePath]
		original, readErr := os.ReadFile(targetPath)
		existed := readErr == nil
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			rollback()
			return bundleError{Code: bundleCodeImportFailed, Message: fmt.Sprintf("read current resource %s: %v", targetPath, readErr)}
		}
		backups = append(backups, backupEntry{Path: targetPath, Content: original, Existed: existed})

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			rollback()
			return bundleError{Code: bundleCodeImportFailed, Message: fmt.Sprintf("prepare target directory for %s: %v", targetPath, err)}
		}

		tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".aidev-import-*.tmp")
		if err != nil {
			rollback()
			return bundleError{Code: bundleCodeImportFailed, Message: fmt.Sprintf("create temporary file for %s: %v", targetPath, err)}
		}
		tempPath := tempFile.Name()
		if _, err := tempFile.Write(content); err != nil {
			_ = tempFile.Close()
			_ = os.Remove(tempPath)
			rollback()
			return bundleError{Code: bundleCodeImportFailed, Message: fmt.Sprintf("write temporary file for %s: %v", targetPath, err)}
		}
		if err := tempFile.Close(); err != nil {
			_ = os.Remove(tempPath)
			rollback()
			return bundleError{Code: bundleCodeImportFailed, Message: fmt.Sprintf("close temporary file for %s: %v", targetPath, err)}
		}
		if err := os.Rename(tempPath, targetPath); err != nil {
			_ = os.Remove(tempPath)
			rollback()
			return bundleError{Code: bundleCodeImportFailed, Message: fmt.Sprintf("replace target resource %s: %v", targetPath, err)}
		}
		if !existed {
			created = append(created, targetPath)
		}
	}
	return nil
}

func bundleResourceTargetPath(paths Paths, resourcePath string) (string, error) {
	resourcePath = filepath.ToSlash(resourcePath)
	switch {
	case resourcePath == "config/global.toml":
		return filepath.Join(paths.ConfigHome, "global.toml"), nil
	case strings.HasPrefix(resourcePath, "config/projects/"):
		fileName := filepath.Base(resourcePath)
		if fileName == "." || fileName == "" {
			return "", bundleError{Code: bundleCodeResourceInvalid, Message: fmt.Sprintf("invalid project config path %s", resourcePath)}
		}
		return filepath.Join(paths.ConfigHome, "projects", fileName), nil
	case strings.HasPrefix(resourcePath, "profiles/"):
		return filepath.Join(paths.ConfigHome, filepath.FromSlash(resourcePath)), nil
	case strings.HasPrefix(resourcePath, "machines/"):
		return filepath.Join(paths.ConfigHome, filepath.FromSlash(resourcePath)), nil
	case strings.HasPrefix(resourcePath, "prompts/"):
		return filepath.Join(paths.ConfigHome, filepath.FromSlash(resourcePath)), nil
	case strings.HasPrefix(resourcePath, "rules/"):
		return filepath.Join(paths.ConfigHome, filepath.FromSlash(resourcePath)), nil
	case strings.HasPrefix(resourcePath, "plugins/"):
		relative := strings.TrimPrefix(resourcePath, "plugins/")
		return filepath.Join(paths.DataHome, "plugins", filepath.FromSlash(relative)), nil
	default:
		return "", bundleError{Code: bundleCodeResourceInvalid, Message: fmt.Sprintf("unsupported bundle resource path %s", resourcePath)}
	}
}

func printBundleImportReport(report bundleImportReport) {
	for _, action := range report.Creates {
		fmt.Printf("create resource=%s target=%s\n", action.ResourcePath, action.TargetPath)
	}
	for _, action := range report.Updates {
		fmt.Printf("update resource=%s target=%s\n", action.ResourcePath, action.TargetPath)
	}
	for _, action := range report.Skipped {
		fmt.Printf("skip resource=%s target=%s reason=%s\n", action.ResourcePath, action.TargetPath, action.Reason)
	}
	for _, action := range report.Conflicts {
		fmt.Printf("conflict resource=%s target=%s reason=%s\n", action.ResourcePath, action.TargetPath, action.Reason)
	}
}

func checksumForContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func defaultBundlePath() string {
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	return "ai-dev-" + timestamp + bundleFileExtension
}

func writeBundleProvenance(paths Paths, manifest bundleManifest, operations []bundleImportAction) error {
	filePath := filepath.Join(paths.StateHome, "bundle-provenance.json")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return bundleError{Code: bundleCodeImportFailed, Message: fmt.Sprintf("prepare provenance directory: %v", err)}
	}
	payload := map[string]any{
		"schema":             manifest.Schema,
		"bundle_version":     manifest.BundleVersion,
		"created_at":         manifest.CreatedAt,
		"creator_version":    manifest.CreatorVersion,
		"project_identifier": manifest.ProjectIdentifier,
		"imported_at":        time.Now().UTC().Format(time.RFC3339),
		"operations":         operations,
	}
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return bundleError{Code: bundleCodeImportFailed, Message: fmt.Sprintf("encode provenance file: %v", err)}
	}
	if err := os.WriteFile(filePath, content, 0o600); err != nil {
		return bundleError{Code: bundleCodeImportFailed, Message: fmt.Sprintf("write provenance file: %v", err)}
	}
	return nil
}

func writeBundleLastStatus(paths Paths, valid bool, message string) error {
	filePath := filepath.Join(paths.StateHome, "bundle-last-status.json")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return bundleError{Code: bundleCodeInvalid, Message: fmt.Sprintf("prepare bundle status directory: %v", err)}
	}
	payload := map[string]any{
		"valid":      valid,
		"message":    message,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return bundleError{Code: bundleCodeInvalid, Message: fmt.Sprintf("encode bundle status payload: %v", err)}
	}
	if err := os.WriteFile(filePath, content, 0o600); err != nil {
		return bundleError{Code: bundleCodeInvalid, Message: fmt.Sprintf("write bundle status payload: %v", err)}
	}
	return nil
}

func readBundleLastStatus(paths Paths) (map[string]any, error) {
	filePath := filepath.Join(paths.StateHome, "bundle-last-status.json")
	if !fileExists(filePath) {
		return map[string]any{"valid": true, "message": "no bundle operations have run"}, nil
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func bundleDoctorLines(paths Paths) []string {
	lines := []string{}
	defaultDirectory := filepath.Join(paths.DataHome, "plugins")
	status, err := readBundleLastStatus(paths)
	if err != nil {
		lines = append(lines, fmt.Sprintf("[error] bundle status: code=%s message=%s", bundleCodeInvalid, err.Error()))
		return lines
	}
	valid, _ := status["valid"].(bool)
	message, _ := status["message"].(string)
	updatedAt, _ := status["updated_at"].(string)
	state := "ok"
	if !valid {
		state = "error"
	}
	lines = append(lines, fmt.Sprintf("[%s] bundle support: schema=%s extension=%s default_directory=%s", state, bundleSchemaV1, bundleFileExtension, defaultDirectory))
	lines = append(lines, fmt.Sprintf("[%s] bundle last_status: valid=%t updated_at=%s message=%s", state, valid, updatedAt, message))
	return lines
}

func bundleResourceKeys(resources map[string][]byte) []string {
	keys := make([]string, 0, len(resources))
	for key := range resources {
		keys = append(keys, key)
	}
	return keys
}
