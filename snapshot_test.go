package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIResolveSnapshotFetchesPublicRepositoryWhenLocalMissing(t *testing.T) {
	content := "# Remote client\n\n```text\n~/.remote/\n└── AGENTS.md # Instructions\n```\n"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/snapshots/remote-snapshot.md" {
			t.Fatalf("unexpected snapshot URL: %s", request.URL.Path)
		}
		_, _ = writer.Write([]byte(content))
	}))
	defer server.Close()
	previousURL := aiPublicSnapshotBaseURL
	previousClient := aiSnapshotHTTPClient
	aiPublicSnapshotBaseURL = server.URL
	aiSnapshotHTTPClient = server.Client()
	defer func() {
		aiPublicSnapshotBaseURL = previousURL
		aiSnapshotHTTPClient = previousClient
	}()

	snapshot, err := aiResolveSnapshot(Paths{ConfigHome: t.TempDir()}, "remote")
	if err != nil {
		t.Fatalf("resolve public snapshot: %v", err)
	}
	if snapshot.Source != "public" || snapshot.Content != content {
		t.Fatalf("unexpected public snapshot: %+v", snapshot)
	}
}

func TestAIResolveSnapshotPrefersLocalLibraryCache(t *testing.T) {
	paths := Paths{ConfigHome: t.TempDir()}
	content := "# Cached client\n\n```text\n~/.cached/\n└── AGENTS.md # Instructions\n```\n"
	cachePath := filepath.Join(paths.ConfigHome, "clients", "cached", "cached-snapshot.md")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("create cache: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte(content), 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}
	snapshot, err := aiResolveSnapshot(paths, "cached")
	if err != nil {
		t.Fatalf("resolve cached snapshot: %v", err)
	}
	if snapshot.Source != "cache" || snapshot.Content != content {
		t.Fatalf("unexpected cached snapshot: %+v", snapshot)
	}
}

func TestAILoadSnapshotDefinitionPreservesNestedHierarchy(t *testing.T) {
	snapshot, err := aiLoadSnapshotDefinition(clientNameCodex)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	found := false
	for _, root := range snapshot.Roots {
		if root.Path != "~/.agents" {
			continue
		}
		for _, entry := range root.Entries {
			if entry.Path == "skills/<skill-name>/SKILL.md" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("nested skill hierarchy was not parsed: %+v", snapshot.Roots)
	}
}

func TestAICacheSnapshotCopiesEmbeddedSourceToLibraryConfig(t *testing.T) {
	snapshot, err := aiLoadSnapshotDefinition(clientNameCodex)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	paths := Paths{ConfigHome: t.TempDir()}
	if err := aiCacheSnapshot(paths, clientNameCodex, snapshot); err != nil {
		t.Fatalf("cache snapshot: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(paths.ConfigHome, "clients", clientNameCodex, "codex-snapshot.md"))
	if err != nil {
		t.Fatalf("read cached snapshot: %v", err)
	}
	if string(data) != snapshot.Content {
		t.Fatal("cached snapshot differs from the embedded repository source")
	}
}

func TestAIValidateSnapshotLayoutAcceptsTemplatesAndRejectsUndocumentedPaths(t *testing.T) {
	snapshot, err := aiLoadSnapshotDefinition(clientNameCodex)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	aligned := map[string]any{
		"root":              "~/.codex",
		"instruction_files": []string{"AGENTS.md"},
		"prompt_files":      []string{"prompts/review.md"},
	}
	if err := aiValidateSnapshotLayout(snapshot, aligned, "user"); err != nil {
		t.Fatalf("expected aligned layout: %v", err)
	}
	mismatch := map[string]any{"root": "~/.codex", "mcp_files": []string{"mcp.json"}}
	err = aiValidateSnapshotLayout(snapshot, mismatch, "user")
	if err == nil || !strings.Contains(err.Error(), "snapshots/codex-snapshot.md") || !strings.Contains(err.Error(), "mcp.json") {
		t.Fatalf("expected named snapshot mismatch, got %v", err)
	}
}

func TestAISnapshotDirectoriesUseMostCompleteDocumentedTree(t *testing.T) {
	snapshot, err := aiLoadSnapshotDefinition(clientNameCodex)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	directories := aiSnapshotDirectories(snapshot, "project", "/tmp/project")
	want := filepath.Join("/tmp/project", ".agents", "skills")
	for _, directory := range directories {
		if strings.Contains(directory, "<") || strings.Contains(directory, ">") {
			t.Fatalf("snapshot template was created literally: %s", directory)
		}
		if directory == want {
			want = ""
		}
	}
	if want != "" {
		t.Fatalf("expected %s in complete project skeleton: %+v", want, directories)
	}
}

func TestAISnapshotPlanDirectoriesIncludesConcreteParityHierarchy(t *testing.T) {
	snapshot, err := aiLoadSnapshotDefinition(clientNameCodex)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	file := filepath.Join("/tmp/home", ".agents", "skills", "ai-dev", "SKILL.md")
	directories := aiSnapshotPlanDirectories(snapshot, "user", "/tmp/project", []string{file})
	want := filepath.Dir(file)
	for _, directory := range directories {
		if directory == want {
			return
		}
	}
	t.Fatalf("expected concrete parity directory %s: %+v", want, directories)
}
