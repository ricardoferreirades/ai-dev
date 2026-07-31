package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

type aiCredentialProvider struct {
	Name         string
	APIKeyEnv    string
	BaseURLEnv   string
	ModelEnv     string
	DefaultBase  string
	DefaultModel string
}

var aiCredentialProviders = map[string]aiCredentialProvider{
	"openai": {
		Name:         "openai",
		APIKeyEnv:    "OPENAI_API_KEY",
		BaseURLEnv:   "OPENAI_BASE_URL",
		ModelEnv:     "OPENAI_MODEL",
		DefaultBase:  "https://api.openai.com/v1",
		DefaultModel: "gpt-4.1-mini",
	},
	"gemini": {
		Name:         "gemini",
		APIKeyEnv:    "GEMINI_API_KEY",
		BaseURLEnv:   "GEMINI_BASE_URL",
		ModelEnv:     "GEMINI_MODEL",
		DefaultBase:  "https://generativelanguage.googleapis.com/v1beta",
		DefaultModel: "gemini-2.5-flash",
	},
	"openrouter": {
		Name:         "openrouter",
		APIKeyEnv:    "OPENROUTER_API_KEY",
		BaseURLEnv:   "OPENROUTER_BASE_URL",
		ModelEnv:     "OPENROUTER_MODEL",
		DefaultBase:  "https://openrouter.ai/api/v1",
		DefaultModel: "openrouter/free",
	},
}

func aiCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "ai requires a subcommand"}
	}

	switch arguments[0] {
	case "context":
		return aiContextCommand(paths, arguments[1:])
	case "agents":
		return aiAgentsCommand(paths, arguments[1:])
	case "launch":
		return aiLaunchCommand(paths, arguments[1:])
	case "sync":
		return aiSyncCommand(paths, arguments[1:])
	case "env":
		return aiEnvCommand(paths, arguments[1:])
	case "credentials":
		return aiCredentialsCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown ai subcommand: %s", arguments[0])}
	}
}

func aiAgentsCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "ai agents requires a subcommand"}
	}

	switch arguments[0] {
	case "sync":
		return aiAgentsSyncCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown ai agents subcommand: %s", arguments[0])}
	}
}

func aiAgentsSyncCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "ai agents sync requires a client name"}
	}
	clientName := arguments[0]
	merged := []string{clientName, "--target", "user"}
	merged = append(merged, arguments[1:]...)
	return aiSyncCommand(paths, merged)
}

func aiEnvCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "ai env requires a subcommand"}
	}

	switch arguments[0] {
	case "init":
		return aiEnvInitCommand(paths, arguments[1:])
	case "show":
		return aiEnvShowCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown ai env subcommand: %s", arguments[0])}
	}
}

func aiEnvInitCommand(paths Paths, arguments []string) error {
	force := false
	for _, argument := range arguments {
		switch argument {
		case "--force":
			force = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown ai env init option: %s", argument)}
		}
	}

	path := aiConfigEnvPath(paths, false)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if fileExists(path) && !force {
		return nil
	}
	content := aiConfigEnvContent()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}

	examplePath := aiConfigEnvPath(paths, true)
	if !fileExists(examplePath) || force {
		if err := os.WriteFile(examplePath, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func aiEnvShowCommand(paths Paths, arguments []string) error {
	for _, argument := range arguments {
		switch argument {
		case "--example":
			data, err := os.ReadFile(aiConfigEnvPath(paths, true))
			if err != nil {
				return err
			}
			fmt.Print(string(data))
			return nil
		default:
			return UsageError{Message: fmt.Sprintf("unknown ai env show option: %s", argument)}
		}
	}
	data, err := os.ReadFile(aiConfigEnvPath(paths, false))
	if err != nil {
		if fileExists(aiConfigEnvPath(paths, true)) {
			data, err = os.ReadFile(aiConfigEnvPath(paths, true))
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	fmt.Print(string(data))
	return nil
}

func aiConfigEnvPath(paths Paths, example bool) string {
	name := "ai-dev.env"
	if example {
		name += ".example"
	}
	return filepath.Join(paths.ConfigHome, name)
}

func aiConfigEnvContent() string {
	lines := []string{
		"# ai-dev config env",
		"# Copy this file to ~/.config/ai-dev/ai-dev.env and fill in the keys you use.",
		"# The tool falls back to cached offline instructions if no key is present.",
		"# Optional provider preference: openrouter, openai, or gemini.",
		"AI_DEV_AI_PROVIDER=",
		"",
		"# OpenAI",
		"OPENAI_API_KEY=",
		"OPENAI_BASE_URL=https://api.openai.com/v1",
		"OPENAI_MODEL=gpt-4.1-mini",
		"",
		"# Gemini",
		"GEMINI_API_KEY=",
		"GEMINI_BASE_URL=https://generativelanguage.googleapis.com/v1beta",
		"GEMINI_MODEL=gemini-2.5-flash",
		"",
		"# OpenRouter",
		"OPENROUTER_API_KEY=",
		"OPENROUTER_BASE_URL=https://openrouter.ai/api/v1",
		"OPENROUTER_MODEL=openrouter/free",
		"",
	}
	return strings.Join(lines, "\n")
}

func aiCredentialsCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "ai credentials requires a subcommand"}
	}

	switch arguments[0] {
	case "init":
		return aiCredentialsInitCommand(paths, arguments[1:])
	case "show":
		return aiCredentialsShowCommand(paths, arguments[1:])
	default:
		return UsageError{Message: fmt.Sprintf("unknown ai credentials subcommand: %s", arguments[0])}
	}
}

func aiCredentialsInitCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "ai credentials init requires a client name"}
	}
	clientName := arguments[0]
	provider, ok := aiCredentialProviders[clientName]
	if !ok {
		return UsageError{Message: fmt.Sprintf("unsupported ai credentials provider: %s", clientName)}
	}
	format := "env"
	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--format":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--format requires a value"}
			}
			index++
			format = arguments[index]
		default:
			if argument != "--format" {
				return UsageError{Message: fmt.Sprintf("unknown ai credentials init option: %s", argument)}
			}
		}
	}

	if format != "env" && format != "json" {
		return UsageError{Message: fmt.Sprintf("unsupported credentials format: %s", format)}
	}

	path := aiCredentialsPath(paths, clientName, format, true)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if fileExists(path) {
		return nil
	}

	var content string
	if format == "env" {
		content = strings.Join([]string{
			"# ai-dev provider credentials example",
			"# Copy this file to the non-example variant and fill in your secret.",
			fmt.Sprintf("%s=", provider.APIKeyEnv),
			fmt.Sprintf("%s=%s", provider.BaseURLEnv, provider.DefaultBase),
			fmt.Sprintf("%s=%s", provider.ModelEnv, provider.DefaultModel),
			"",
		}, "\n")
	} else {
		payload := map[string]string{
			provider.APIKeyEnv:  "",
			provider.BaseURLEnv: provider.DefaultBase,
			provider.ModelEnv:   provider.DefaultModel,
		}
		bytes, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		content = string(bytes) + "\n"
	}

	return os.WriteFile(path, []byte(content), 0o600)
}

func aiCredentialsShowCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "ai credentials show requires a client name"}
	}
	clientName := arguments[0]
	if _, ok := aiCredentialProviders[clientName]; !ok {
		return UsageError{Message: fmt.Sprintf("unsupported ai credentials provider: %s", clientName)}
	}
	format := "env"
	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--format":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--format requires a value"}
			}
			index++
			format = arguments[index]
		default:
			return UsageError{Message: fmt.Sprintf("unknown ai credentials show option: %s", argument)}
		}
	}
	if format != "env" && format != "json" {
		return UsageError{Message: fmt.Sprintf("unsupported credentials format: %s", format)}
	}
	path := aiCredentialsPath(paths, clientName, format, false)
	if !fileExists(path) {
		path = aiCredentialsPath(paths, clientName, format, true)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func aiCredentialsPath(paths Paths, clientName, format string, example bool) string {
	base := filepath.Join(paths.ConfigHome, "clients")
	suffix := ".env"
	if format == "json" {
		suffix = ".json"
	}
	if example {
		suffix += ".example"
	}
	return filepath.Join(base, clientName+suffix)
}

func aiSyncCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "ai sync requires a client name"}
	}

	clientName := arguments[0]
	target := "project"
	force := false
	dryRun := false
	importName := ""

	for index := 1; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--force":
			force = true
		case "--dry-run":
			dryRun = true
		case "--target":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--target requires a value"}
			}
			index++
			target = arguments[index]
		case "--import":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--import requires a profile name"}
			}
			index++
			importName = arguments[index]
		default:
			return UsageError{Message: fmt.Sprintf("unknown ai sync option: %s", argument)}
		}
	}
	if importName != "" {
		if err := validateSourceImportProfile(paths, importName); err != nil {
			return err
		}
	}
	previousImportName := activeImportName
	activeImportName = importName
	defer func() { activeImportName = previousImportName }()

	remote, remoteErr := aiProbeRemoteModel(paths, clientName, target)
	if remoteErr == nil {
		fmt.Printf("ai_remote=%s provider=%s model=%s\n", remote.Status, remote.Provider, remote.Model)
		if updateErr := aiWriteClientRemoteResult(paths, clientName, remote); updateErr != nil {
			return updateErr
		}
	} else {
		fmt.Printf("ai_remote=unavailable\n")
		fmt.Printf("ai_remote_error=%s\n", remoteErr.Error())
	}
	discovery, err := aiRefreshClientDefinition(paths, clientName, target)
	if err != nil {
		return err
	}
	if remoteErr == nil {
		discovery = aiApplyRemoteDecision(discovery, remote)
		if _, err := aiWriteClientDiscovery(paths, clientName, discovery); err != nil {
			return err
		}
		if updateErr := aiApplyRemoteConfigUpdate(paths, clientName, remote, discovery); updateErr != nil {
			return updateErr
		}
	} else {
		discovery.Source = "local"
		if definition, loadErr := aiLoadClientDefinition(paths, clientName); loadErr == nil {
			discovery.Status = "cached"
			discovery.Message = "remote model unavailable; using cached client definition"
			if layout, ok := definition["layout"].(map[string]any); ok {
				if root, ok := layout["root"].(string); ok {
					discovery.Selected = root
				}
			}
			if _, err := aiWriteClientDiscovery(paths, clientName, discovery); err != nil {
				return err
			}
		}
	}
	fmt.Printf("client_definition=%s\n", discovery.Status)
	fmt.Printf("ai_source=%s\n", discovery.Source)

	plan, err := aiBuildClientBundle(paths, clientName, target)
	if err != nil {
		return err
	}

	if dryRun {
		for _, path := range plan.Paths {
			fmt.Printf("write=%s\n", path)
		}
		return nil
	}

	if err := aiWriteClientBundle(plan, force); err != nil {
		return err
	}

	fmt.Printf("synced client=%s target=%s\n", clientName, target)
	return nil
}

type aiRemoteProbeResult struct {
	Status   string         `json:"status"`
	Provider string         `json:"provider"`
	Model    string         `json:"model"`
	Mode     string         `json:"mode"`
	Response string         `json:"response,omitempty"`
	Parsed   map[string]any `json:"parsed,omitempty"`
}

func aiProbeRemoteModel(paths Paths, clientName, target string) (aiRemoteProbeResult, error) {
	envValues, err := aiLoadConfigEnv(paths)
	if err != nil {
		return aiRemoteProbeResult{}, err
	}

	providers := []string{"openrouter", "openai", "gemini"}
	if preferred := strings.ToLower(strings.TrimSpace(envValues["AI_DEV_AI_PROVIDER"])); preferred != "" {
		if _, ok := aiCredentialProviders[preferred]; !ok {
			return aiRemoteProbeResult{}, fmt.Errorf("unsupported AI_DEV_AI_PROVIDER %q", preferred)
		}
		providers = append([]string{preferred}, providers...)
	}
	providers = uniqueStrings(providers)

	prompt := aiClientDiscoveryPrompt(paths, clientName, target)
	errors := []string{}
	configured := false
	for _, providerName := range providers {
		provider := aiCredentialProviders[providerName]
		if strings.TrimSpace(envValues[provider.APIKeyEnv]) == "" {
			continue
		}
		configured = true
		result, probeErr := aiProbeProvider(provider, envValues, prompt)
		if probeErr == nil {
			return result, nil
		}
		errors = append(errors, providerName+": "+probeErr.Error())
	}
	if !configured {
		return aiRemoteProbeResult{}, fmt.Errorf("no configured AI provider key")
	}
	return aiRemoteProbeResult{}, fmt.Errorf("all configured AI providers failed: %s", strings.Join(errors, "; "))
}

func aiClientDiscoveryPrompt(paths Paths, clientName, target string) string {
	cached := "none"
	if data, err := os.ReadFile(filepath.Join(paths.ConfigHome, "clients", clientName, "client.toml")); err == nil {
		cached = string(data)
	}
	return fmt.Sprintf(`Determine the current native configuration layout for the generative-AI coding client named %q.
Target scope: %q.
Use the client's current official documentation when available. Compare it with the cached ai-dev definition below.

Cached definition:
%s

Return ONLY one JSON object. Required schema:
{"status":"aligned|updated","client":%q,"target":%q,"root":"native user or project configuration root","instruction_files":["relative native filename"],"prompt_files":["relative native filename"],"rule_files":["relative native filename"],"agent_files":["relative native filename"],"skill_files":["relative native SKILL.md filename"],"mcp_files":["relative native filename"],"official_docs":["https://official-source"],"note":"short explanation"}

Rules:
- status must be exactly "aligned" when the cache is current, otherwise "updated".
- Never return pending, unknown, prose, Markdown, or code fences.
- Every file path below root must be relative and must follow this client's official folder and filename conventions, including required suffixes such as .prompt.md, .instructions.md, .agent.md, or SKILL.md.
- For user scope, root may start with ~/; for project scope, root must be relative to the project.
- Do not invent support: use an empty value for unsupported resource types.
`, clientName, target, cached, clientName, target)
}

func aiProbeProvider(provider aiCredentialProvider, envValues map[string]string, prompt string) (aiRemoteProbeResult, error) {
	apiKey := strings.TrimSpace(envValues[provider.APIKeyEnv])
	baseURL := strings.TrimRight(strings.TrimSpace(envValues[provider.BaseURLEnv]), "/")
	if baseURL == "" {
		baseURL = provider.DefaultBase
	}
	model := strings.TrimSpace(envValues[provider.ModelEnv])
	if model == "" {
		model = provider.DefaultModel
	}

	var response string
	var err error
	if provider.Name == "gemini" {
		response, err = aiProbeGemini(baseURL, apiKey, model, prompt)
	} else {
		response, err = aiProbeOpenAICompatible(provider.Name, baseURL, apiKey, model, prompt)
	}
	if err != nil {
		return aiRemoteProbeResult{}, err
	}
	parsed, err := aiParseJSONObject(response)
	if err != nil {
		return aiRemoteProbeResult{}, fmt.Errorf("invalid model JSON: %w", err)
	}
	aiNormalizeRemoteDefinition(parsed)
	if err := aiValidateRemoteDefinition(parsed); err != nil {
		return aiRemoteProbeResult{}, err
	}
	return aiRemoteProbeResult{
		Status:   parsed["status"].(string),
		Provider: provider.Name,
		Model:    model,
		Mode:     "ai",
		Response: response,
		Parsed:   parsed,
	}, nil
}

func aiProbeOpenAICompatible(providerName, baseURL, apiKey, model, prompt string) (string, error) {
	requestBody := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You validate coding-client configuration. Return only strict JSON matching the requested schema."},
			{"role": "user", "content": prompt},
		},
		"temperature": 0,
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(baseURL, "/")+"/chat/completions", strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	if providerName == "openrouter" {
		req.Header.Set("HTTP-Referer", "https://ai-dev.local")
		req.Header.Set("X-OpenRouter-Title", "ai-dev")
		requestBody["plugins"] = []map[string]any{{"id": "web", "max_results": 5}}
		body, err = json.Marshal(requestBody)
		if err != nil {
			return "", err
		}
		req.Body = io.NopCloser(strings.NewReader(string(body)))
		req.ContentLength = int64(len(body))
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("http %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var payload struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	if len(payload.Choices) == 0 {
		return "", fmt.Errorf("model returned no choices")
	}
	return strings.TrimSpace(payload.Choices[0].Message.Content), nil
}

func aiProbeGemini(baseURL, apiKey, model, prompt string) (string, error) {
	requestBody := map[string]any{
		"contents":         []map[string]any{{"parts": []map[string]string{{"text": prompt}}}},
		"generationConfig": map[string]any{"temperature": 0, "responseMimeType": "application/json"},
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}
	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s", baseURL, model, apiKey)
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var payload struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	if len(payload.Candidates) == 0 || len(payload.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("model returned no candidates")
	}
	return strings.TrimSpace(payload.Candidates[0].Content.Parts[0].Text), nil
}

func aiValidateRemoteDefinition(payload map[string]any) error {
	status, ok := payload["status"].(string)
	if !ok || (status != "aligned" && status != "updated") {
		return fmt.Errorf("model status must be aligned or updated")
	}
	for _, key := range []string{"client", "target", "root", "note"} {
		if _, ok := payload[key].(string); !ok {
			return fmt.Errorf("model field %s must be a string", key)
		}
	}
	if strings.TrimSpace(payload["root"].(string)) == "" {
		return fmt.Errorf("model root must not be empty")
	}
	for _, key := range []string{"instruction_files", "prompt_files", "rule_files", "agent_files", "skill_files", "mcp_files", "official_docs"} {
		values, ok := payload[key].([]any)
		if !ok {
			return fmt.Errorf("model field %s must be an array", key)
		}
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("model field %s must contain only strings", key)
			}
			if key != "official_docs" && strings.ContainsAny(text, "*?[]") {
				return fmt.Errorf("model field %s contains an unresolved path pattern", key)
			}
		}
	}
	return nil
}

func aiNormalizeRemoteDefinition(payload map[string]any) {
	for _, key := range []string{"instruction_files", "prompt_files", "rule_files", "agent_files", "skill_files", "mcp_files"} {
		values, ok := payload[key].([]any)
		if !ok {
			continue
		}
		for index, value := range values {
			path, ok := value.(string)
			if !ok || !strings.Contains(path, "*") {
				continue
			}
			if key == "skill_files" {
				switch {
				case strings.Contains(path, "*.SKILL.md"):
					path = strings.Replace(path, "*.SKILL.md", filepath.Join("ai-dev", "SKILL.md"), 1)
				case strings.Contains(path, "*/SKILL.md"):
					path = strings.Replace(path, "*/SKILL.md", "ai-dev/SKILL.md", 1)
				default:
					path = strings.ReplaceAll(path, "*", "ai-dev")
				}
			} else {
				path = strings.ReplaceAll(path, "*", "ai-dev")
			}
			values[index] = path
		}
		payload[key] = values
	}
	if client, _ := payload["client"].(string); strings.EqualFold(client, "copilot") {
		files := aiRemoteStringSlice(payload, "instruction_files")
		const additionalInstructions = "instructions/ai-dev.instructions.md"
		found := false
		for _, file := range files {
			if file == additionalInstructions {
				found = true
				break
			}
		}
		if !found {
			payload["instruction_files"] = append(files, additionalInstructions)
		}
	}
}

func aiRemoteStringSlice(payload map[string]any, key string) []any {
	values, _ := payload[key].([]any)
	return values
}

func aiParseJSONObject(content string) (map[string]any, error) {
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("response is not JSON")
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content[start:end+1]), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func aiLoadConfigEnv(paths Paths) (map[string]string, error) {
	path := aiConfigEnvPath(paths, false)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(strings.Trim(value, `"'`))
	}
	return result, nil
}

func aiLaunchCommand(paths Paths, arguments []string) error {
	if len(arguments) == 0 {
		return UsageError{Message: "ai launch requires a client name"}
	}

	clientName := arguments[0]
	jsonOutput := false
	for _, argument := range arguments[1:] {
		switch argument {
		case "--json":
			jsonOutput = true
		default:
			return UsageError{Message: fmt.Sprintf("unknown ai launch option: %s", argument)}
		}
	}

	if _, err := adapterByName(clientName); err != nil {
		return err
	}

	contextOutput, err := buildAIContextOutput(paths, clientName)
	if err != nil {
		return err
	}

	handoff := map[string]any{
		"client":    clientName,
		"context":   contextOutput,
		"next_step": "paste the generated context into the client or pipe it into your launch workflow",
		"command":   fmt.Sprintf("ai-dev ai context --client %s", clientName),
	}

	if jsonOutput {
		content, err := json.MarshalIndent(handoff, "", "  ")
		if err != nil {
			return fmt.Errorf("encode ai launch JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	fmt.Printf("client=%s\n", clientName)
	fmt.Printf("command=ai-dev ai context --client %s\n\n", clientName)
	fmt.Print(contextOutput)
	fmt.Println("\nNext: paste the generated context into the client or use it in your launch flow.")
	return nil
}

func aiContextCommand(paths Paths, arguments []string) error {
	jsonOutput := false
	clientName := ""

	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		switch argument {
		case "--json":
			jsonOutput = true
		case "--client":
			if index+1 >= len(arguments) {
				return UsageError{Message: "--client requires a value"}
			}
			index++
			clientName = arguments[index]
		default:
			return UsageError{Message: fmt.Sprintf("unknown ai context option: %s", argument)}
		}
	}
	payload, err := buildAIContextPayload(paths, clientName)
	if err != nil {
		return err
	}

	if jsonOutput {
		content, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encode ai context JSON: %w", err)
		}
		fmt.Println(string(content))
		return nil
	}

	var builder strings.Builder
	builder.WriteString("# ai-dev context\n\n")
	builder.WriteString("Generated from the current project configuration.\n\n")
	builder.WriteString("## Project\n\n")
	builder.WriteString(formatContextSectionText(payload["project"]))
	builder.WriteString("\n## Configuration\n\n")
	builder.WriteString(formatContextSectionText(payload["config"]))
	builder.WriteString("\n## Active Context\n\n")
	builder.WriteString(formatContextSectionText(payload["context"]))
	builder.WriteString("\n## Prompts\n\n")
	builder.WriteString(formatContextSectionText(payload["prompts"]))
	builder.WriteString("\n## Rules\n\n")
	builder.WriteString(formatContextSectionText(payload["rules"]))
	builder.WriteString("\n## MCP\n\n")
	builder.WriteString(formatContextSectionText(payload["mcp"]))
	if clientName != "" {
		builder.WriteString("\n## Client\n\n")
		builder.WriteString(formatContextSectionText(payload["client"]))
	}
	fmt.Println(builder.String())
	return nil
}

func buildAIContextOutput(paths Paths, clientName string) (string, error) {
	payload, err := buildAIContextPayload(paths, clientName)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	builder.WriteString("# ai-dev context\n\n")
	builder.WriteString("Generated from the current project configuration.\n\n")
	builder.WriteString("## Project\n\n")
	builder.WriteString(formatContextSectionText(payload["project"]))
	builder.WriteString("\n## Configuration\n\n")
	builder.WriteString(formatContextSectionText(payload["config"]))
	builder.WriteString("\n## Active Context\n\n")
	builder.WriteString(formatContextSectionText(payload["context"]))
	builder.WriteString("\n## Prompts\n\n")
	builder.WriteString(formatContextSectionText(payload["prompts"]))
	builder.WriteString("\n## Rules\n\n")
	builder.WriteString(formatContextSectionText(payload["rules"]))
	builder.WriteString("\n## MCP\n\n")
	builder.WriteString(formatContextSectionText(payload["mcp"]))
	if client, ok := payload["client"]; ok {
		builder.WriteString("\n## Client\n\n")
		builder.WriteString(formatContextSectionText(client))
	}
	return builder.String(), nil
}

func buildAIContextPayload(paths Paths, clientName string) (map[string]any, error) {
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return nil, err
	}
	contextModel, err := validateContextModel(paths, info)
	if err != nil {
		return nil, err
	}
	registryModel, err := resolveRegistrySourceModel(paths)
	if err != nil {
		return nil, err
	}
	promptIndex, err := registryIndexFromModel(paths, registryModel, registryKindPrompt)
	if err != nil {
		return nil, err
	}
	ruleIndex, err := registryIndexFromModel(paths, registryModel, registryKindRule)
	if err != nil {
		return nil, err
	}
	promptIdentifiers, err := collectEnabledRegistryIdentifiers(registryKindPrompt, registryModel.LoadedSource, registryModel.Resolved)
	if err != nil {
		return nil, err
	}
	ruleIdentifiers, err := collectEnabledRegistryIdentifiers(registryKindRule, registryModel.LoadedSource, registryModel.Resolved)
	if err != nil {
		return nil, err
	}
	if activeImportName != "" {
		promptIdentifiers = sortedRegistryIdentifiers(promptIndex.Resources)
		ruleIdentifiers = sortedRegistryIdentifiers(ruleIndex.Resources)
	}
	promptContent, err := composeRegistryDocument(registryKindPrompt, promptIndex, promptIdentifiers)
	if err != nil {
		return nil, err
	}
	ruleContent, err := composeRegistryDocument(registryKindRule, ruleIndex, ruleIdentifiers)
	if err != nil {
		return nil, err
	}
	scopes := mcpServerScopes(registryModel.LoadedSource)
	servers, diagnostics := parseMCPServers(registryModel.Resolved, scopes)
	if len(diagnostics) > 0 {
		sortMCPDiagnostics(diagnostics)
		first := diagnostics[0]
		return nil, mcpError{
			Code:    first.Code,
			Message: fmt.Sprintf("MCP server %q field %q: %s", first.Name, first.Path, first.Message),
		}
	}
	importedResources, err := loadImportedAIResources(paths)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"project": map[string]any{
			"id":          info.ProjectID,
			"root":        info.ProjectRoot,
			"repository":  info.Repository,
			"branch":      info.Branch,
			"identity":    info.IdentitySource,
			"worktree":    info.Repository && info.CommonGitDirectory != info.GitDirectory,
			"current_dir": info.CurrentDirectory,
		},
		"config": registryModel.Resolved,
		"context": map[string]any{
			"machine_source":  contextModel.MachineRawSource,
			"machine_id":      contextModel.MachineIdentifier,
			"active_profiles": contextModel.ActiveProfiles,
			"sources":         buildConfigSourceOutputs(contextModel.Sources),
		},
		"prompts": map[string]any{
			"enabled": promptIdentifiers,
			"text":    promptContent,
		},
		"rules": map[string]any{
			"enabled": ruleIdentifiers,
			"text":    ruleContent,
		},
		"mcp": map[string]any{
			"servers": servers,
		},
		"resources": importedResources,
	}

	if clientName != "" {
		clientDetails := aiClientContextDetails(paths, info, clientName)
		discovery, discoveryErr := aiReadClientDiscovery(paths, clientName)
		if discoveryErr != nil {
			discovery, _ = aiRefreshClientDefinition(paths, clientName, "user")
		}
		payload["client"] = map[string]any{
			"name":           clientName,
			"known":          clientDetails["known"],
			"default_format": clientDetails["default_format"],
			"capabilities":   clientDetails["capabilities"],
			"destinations":   clientDetails["destinations"],
			"limitations":    clientDetails["limitations"],
			"launch_hint":    launchHintForClient(clientName),
		}
		payload["client_discovery"] = discovery
	}

	return payload, nil
}

func launchHintForClient(clientName string) string {
	return fmt.Sprintf("ai-dev ai context --client %s", clientName)
}

func aiClientContextDetails(paths Paths, info ProjectInfo, clientName string) map[string]any {
	details := map[string]any{
		"known":          false,
		"default_format": "text",
		"capabilities":   map[string]string{},
		"destinations":   []ClientDestination{},
		"limitations":    []string{"unknown client; using generic offline instructions"},
	}
	adapter, err := adapterByName(clientName)
	if err != nil {
		return details
	}
	pathResult, err := adapter.Destinations(paths, info, "")
	if err != nil {
		details["limitations"] = append(details["limitations"].([]string), err.Error())
		return details
	}
	details["known"] = true
	details["default_format"] = adapter.DefaultFormat()
	details["capabilities"] = adapter.Capabilities()
	details["destinations"] = pathResult.Destinations
	details["limitations"] = adapter.KnownLimitations()
	return details
}

type aiClientDiscoveryReport struct {
	Client   string              `json:"client"`
	Known    bool                `json:"known"`
	Target   string              `json:"target"`
	Status   string              `json:"status"`
	Source   string              `json:"source"`
	Selected string              `json:"selected,omitempty"`
	Expected []ClientDestination `json:"expected,omitempty"`
	Mismatch bool                `json:"mismatch"`
	Message  string              `json:"message"`
}

func aiApplyRemoteDecision(report aiClientDiscoveryReport, remote aiRemoteProbeResult) aiClientDiscoveryReport {
	if remote.Status == "" {
		return report
	}
	report.Source = remote.Mode
	if remote.Status == "aligned" {
		report.Status = "aligned"
		report.Mismatch = false
		if note, ok := remote.Parsed["note"].(string); ok && note != "" {
			report.Message = note
		}
		return report
	}
	if remote.Status == "updated" {
		report.Status = "updated"
		report.Mismatch = true
		if note, ok := remote.Parsed["note"].(string); ok && note != "" {
			report.Message = note
		}
	}
	return report
}

func aiApplyRemoteConfigUpdate(paths Paths, clientName string, remote aiRemoteProbeResult, discovery aiClientDiscoveryReport) error {
	dir := filepath.Join(paths.ConfigHome, "clients", clientName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	clientConfig := map[string]any{
		"schema":     "v1",
		"client":     clientName,
		"source":     discovery.Source,
		"status":     discovery.Status,
		"target":     discovery.Target,
		"known":      discovery.Known,
		"mismatch":   discovery.Mismatch,
		"updated_at": time.Now().UTC().Format(time.RFC3339),
		"provider":   remote.Provider,
		"model":      remote.Model,
	}
	if remote.Parsed != nil {
		clientConfig["layout"] = remote.Parsed
	}
	content, err := toml.Marshal(clientConfig)
	if err != nil {
		return err
	}
	definitionPath := filepath.Join(dir, "client.toml")
	if err := os.WriteFile(definitionPath, content, 0o600); err != nil {
		return err
	}
	return aiRegisterClientDefinition(paths, clientName, definitionPath)
}

func aiRegisterClientDefinition(paths Paths, clientName, definitionPath string) error {
	globalPath := filepath.Join(paths.ConfigHome, "global.toml")
	configuration := map[string]any{"schema": "v1"}
	if fileExists(globalPath) {
		loaded, err := readTOML(globalPath)
		if err != nil {
			return err
		}
		configuration = loaded
	}
	clients, _ := configuration["clients"].(map[string]any)
	if clients == nil {
		clients = map[string]any{}
	}
	entry, _ := clients[clientName].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}
	entry["enabled"] = true
	entry["definition"] = definitionPath
	clients[clientName] = entry
	configuration["clients"] = clients
	content, err := toml.Marshal(configuration)
	if err != nil {
		return fmt.Errorf("encode global configuration: %w", err)
	}
	return os.WriteFile(globalPath, content, 0o600)
}

func aiRefreshClientDefinition(paths Paths, clientName, target string) (aiClientDiscoveryReport, error) {
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return aiClientDiscoveryReport{}, err
	}

	report := aiClientDiscoveryReport{
		Client:  clientName,
		Target:  target,
		Source:  aiSyncSourceMode(paths),
		Status:  "generic",
		Message: "client definition refreshed",
	}

	adapter, err := adapterByName(clientName)
	if err != nil {
		return aiWriteClientDiscovery(paths, clientName, report)
	}

	pathResult, err := adapter.Destinations(paths, info, targetScope(target))
	if err != nil {
		return aiClientDiscoveryReport{}, err
	}

	report.Known = true
	report.Expected = pathResult.Destinations
	report.Selected = selectClientDestination(pathResult.Destinations, target)
	report.Mismatch = len(pathResult.Destinations) == 0 || report.Selected == ""
	if report.Mismatch {
		report.Status = "mismatch"
		report.Message = "client layout differs from the current ai-dev assumptions"
	} else {
		report.Status = "aligned"
		report.Message = "client layout matches the current ai-dev assumptions"
	}
	return aiWriteClientDiscovery(paths, clientName, report)
}

func aiWriteClientDiscovery(paths Paths, clientName string, report aiClientDiscoveryReport) (aiClientDiscoveryReport, error) {
	dir := filepath.Join(paths.ConfigHome, "clients", clientName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return aiClientDiscoveryReport{}, err
	}
	content, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return aiClientDiscoveryReport{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "discovery.json"), append(content, '\n'), 0o600); err != nil {
		return aiClientDiscoveryReport{}, err
	}
	return report, nil
}

func aiReadClientDiscovery(paths Paths, clientName string) (aiClientDiscoveryReport, error) {
	data, err := os.ReadFile(filepath.Join(paths.ConfigHome, "clients", clientName, "discovery.json"))
	if err != nil {
		return aiClientDiscoveryReport{}, err
	}
	var report aiClientDiscoveryReport
	if err := json.Unmarshal(data, &report); err != nil {
		return aiClientDiscoveryReport{}, err
	}
	return report, nil
}

func aiWriteClientRemoteResult(paths Paths, clientName string, result aiRemoteProbeResult) error {
	dir := filepath.Join(paths.ConfigHome, "clients", clientName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "remote.json"), append(content, '\n'), 0o600)
}

func targetScope(target string) string {
	if target == "project" {
		return clientScopeProject
	}
	return clientScopeUser
}

func selectClientDestination(destinations []ClientDestination, target string) string {
	scope := targetScope(target)
	for _, destination := range destinations {
		if destination.Scope == scope {
			return destination.Path
		}
	}
	if len(destinations) > 0 {
		return destinations[0].Path
	}
	return ""
}

type aiBundlePlan struct {
	Root         string
	Paths        []string
	Files        map[string]string
	ManifestPath string
}

func aiBuildClientBundle(paths Paths, clientName, target string) (aiBundlePlan, error) {
	payload, err := buildAIContextPayload(paths, clientName)
	if err != nil {
		return aiBundlePlan{}, err
	}
	info, err := resolveProjectInfo(paths)
	if err != nil {
		return aiBundlePlan{}, err
	}

	root, err := aiBundleRoot(paths, info, clientName, target)
	if err != nil {
		return aiBundlePlan{}, err
	}
	layout := map[string]any{}
	if definition, loadErr := aiLoadClientDefinition(paths, clientName); loadErr == nil {
		if configured, ok := definition["layout"].(map[string]any); ok {
			layout = configured
			aiNormalizeRemoteDefinition(layout)
			if configuredRoot, ok := configured["root"].(string); ok && strings.TrimSpace(configuredRoot) != "" {
				root, err = aiResolveLayoutRoot(info, configuredRoot, target)
				if err != nil {
					return aiBundlePlan{}, err
				}
			}
		}
	}

	files := map[string]string{}
	addFiles := func(relatives []string, content string) error {
		for _, relative := range relatives {
			path, pathErr := aiLayoutPath(root, relative)
			if pathErr != nil {
				return pathErr
			}
			files[path] = content
		}
		return nil
	}
	hasLayout := len(layout) > 0
	instructionFiles := aiLayoutStringSlice(layout, "instruction_files")
	if len(instructionFiles) == 0 {
		instructionFiles = aiLayoutStringSlice(layout, "instructions")
	}
	if len(instructionFiles) == 0 && !hasLayout {
		instructionFiles = []string{"AGENTS.md"}
	}
	if err := addFiles(instructionFiles, aiBundleAgentsDocument(paths, payload, clientName)); err != nil {
		return aiBundlePlan{}, err
	}

	promptFiles := aiLayoutStringSlice(layout, "prompt_files")
	promptFiles = aiExpandNativeFiles(promptFiles, "prompts", "ai-dev.prompt.md")
	if len(promptFiles) == 0 {
		if directory := aiLayoutString(layout, "prompts_dir", ""); directory != "" {
			promptFiles = []string{filepath.Join(directory, "ai-dev.prompt.md")}
		} else if !hasLayout {
			promptFiles = []string{filepath.Join("prompts", "resolved.md")}
		}
	}
	if err := addFiles(promptFiles, aiRegistryResolvedDocument("prompts", payload["prompts"].(map[string]any), paths)); err != nil {
		return aiBundlePlan{}, err
	}

	ruleFiles := aiLayoutStringSlice(layout, "rule_files")
	ruleFiles = aiExpandNativeFiles(ruleFiles, "rules", "ai-dev.instructions.md")
	if len(ruleFiles) == 0 {
		if directory := aiLayoutString(layout, "rules_dir", ""); directory != "" {
			ruleFiles = []string{filepath.Join(directory, "ai-dev.instructions.md")}
		} else if !hasLayout {
			ruleFiles = []string{filepath.Join("rules", "resolved.md")}
		}
	}
	if err := addFiles(ruleFiles, aiRegistryResolvedDocument("rules", payload["rules"].(map[string]any), paths)); err != nil {
		return aiBundlePlan{}, err
	}

	agentFiles := aiLayoutStringSlice(layout, "agent_files")
	agentFiles = aiExpandNativeFiles(agentFiles, "agents", "ai-dev.agent.md")
	if len(agentFiles) == 0 {
		if directory := aiLayoutString(layout, "agents_dir", ""); directory != "" {
			agentFiles = []string{filepath.Join(directory, "ai-dev.agent.md")}
		}
	}
	if err := addFiles(agentFiles, aiBundleNativeAgentDocument(paths, payload, clientName)); err != nil {
		return aiBundlePlan{}, err
	}

	skillFiles := aiLayoutStringSlice(layout, "skill_files")
	skillFiles = aiExpandNativeFiles(skillFiles, "skills", filepath.Join("ai-dev", "SKILL.md"))
	if len(skillFiles) == 0 {
		if directory := aiLayoutString(layout, "skills_dir", ""); directory != "" {
			skillFiles = []string{filepath.Join(directory, "ai-dev", "SKILL.md")}
		}
	}
	if err := addFiles(skillFiles, aiBundleSkillDocument(paths, payload)); err != nil {
		return aiBundlePlan{}, err
	}

	mcpFiles := aiLayoutStringSlice(layout, "mcp_files")
	if len(mcpFiles) == 0 {
		if file := aiLayoutString(layout, "mcp_file", ""); file != "" {
			mcpFiles = []string{file}
		} else if !hasLayout {
			mcpFiles = []string{"mcp.json"}
		}
	}
	mcpContent, err := json.MarshalIndent(payload["mcp"], "", "  ")
	if err != nil {
		return aiBundlePlan{}, err
	}
	if err := addFiles(mcpFiles, string(mcpContent)+"\n"); err != nil {
		return aiBundlePlan{}, err
	}

	pathsList := make([]string, 0, len(files))
	for path := range files {
		pathsList = append(pathsList, path)
	}
	sort.Strings(pathsList)

	return aiBundlePlan{
		Root:         root,
		Paths:        pathsList,
		Files:        files,
		ManifestPath: filepath.Join(paths.ConfigHome, "clients", clientName, "managed-files.json"),
	}, nil
}

func aiLoadClientDefinition(paths Paths, clientName string) (map[string]any, error) {
	return readTOML(filepath.Join(paths.ConfigHome, "clients", clientName, "client.toml"))
}

func aiResolveLayoutRoot(info ProjectInfo, configuredRoot, target string) (string, error) {
	configuredRoot = strings.TrimSpace(configuredRoot)
	if strings.HasPrefix(configuredRoot, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(configuredRoot, "~/")), nil
	}
	if filepath.IsAbs(configuredRoot) {
		return filepath.Clean(configuredRoot), nil
	}
	if target == "user" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, configuredRoot), nil
	}
	return filepath.Join(info.ProjectRoot, configuredRoot), nil
}

func aiLayoutPath(root, relative string) (string, error) {
	relative = filepath.Clean(strings.TrimSpace(relative))
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe client layout path %q", relative)
	}
	return filepath.Join(root, relative), nil
}

func aiLayoutString(layout map[string]any, key, fallback string) string {
	if value, ok := layout[key].(string); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

func aiLayoutStringSlice(layout map[string]any, key string) []string {
	var result []string
	switch values := layout[key].(type) {
	case []any:
		result = make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
				result = append(result, strings.TrimSpace(text))
			}
		}
	case []string:
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				result = append(result, strings.TrimSpace(value))
			}
		}
	}
	return result
}

func aiExpandNativeFiles(files []string, directoryName, filename string) []string {
	result := make([]string, 0, len(files))
	for _, file := range files {
		clean := filepath.ToSlash(strings.TrimSpace(file))
		if clean == directoryName || clean == directoryName+"/" {
			result = append(result, filepath.Join(directoryName, filename))
		} else {
			result = append(result, file)
		}
	}
	return result
}

func aiBundleSkillDocument(paths Paths, payload map[string]any) string {
	var builder strings.Builder
	builder.WriteString("---\nname: ai-dev\ndescription: Apply the centrally resolved ai-dev prompts, rules, agents, and MCP configuration.\n---\n\n")
	builder.WriteString("Use ai-dev as the source of truth for this project. Apply all resolved project rules before changing code.\n\n")
	builder.WriteString(aiRegistryResolvedDocument("prompts", payload["prompts"].(map[string]any), paths))
	builder.WriteString("\n")
	builder.WriteString(aiRegistryResolvedDocument("rules", payload["rules"].(map[string]any), paths))
	writeImportedResourceCategory(&builder, payload, "skills", "Imported skills")
	return builder.String()
}

func aiBundleNativeAgentDocument(paths Paths, payload map[string]any, clientName string) string {
	var builder strings.Builder
	builder.WriteString("---\nname: ai-dev\ndescription: Orchestrate the centrally managed architect, frontend, backend, DBA, and project-manager roles.\n---\n\n")
	builder.WriteString(aiBundleAgentsDocument(paths, payload, clientName))
	builder.WriteString("\n## Resolved role prompts\n\n")
	builder.WriteString(aiRegistryResolvedDocument("prompts", payload["prompts"].(map[string]any), paths))
	builder.WriteString("\n## Resolved project rules\n\n")
	builder.WriteString(aiRegistryResolvedDocument("rules", payload["rules"].(map[string]any), paths))
	writeImportedResourceCategory(&builder, payload, "agents", "Imported agents")
	return builder.String()
}

func aiBundleRoot(paths Paths, info ProjectInfo, clientName, target string) (string, error) {
	switch target {
	case "project":
		return filepath.Join(info.ProjectRoot, ".ai-dev", clientName), nil
	case "user":
		return aiUserBundleRoot(clientName), nil
	case "both":
		return filepath.Join(info.ProjectRoot, ".ai-dev", clientName), nil
	default:
		return "", UsageError{Message: fmt.Sprintf("unknown ai sync target: %s", target)}
	}
}

func aiUserBundleRoot(clientName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "."+clientName)
	}
	switch clientName {
	case clientNameCodex:
		return filepath.Join(home, ".codex")
	case clientNameClaude:
		return filepath.Join(home, ".claude")
	case clientNameCursor:
		return filepath.Join(home, ".cursor")
	case clientNameVSCode:
		return filepath.Join(home, ".vscode")
	default:
		return filepath.Join(home, "."+clientName)
	}
}

func aiBundleAgentsDocument(paths Paths, payload map[string]any, clientName string) string {
	builder := strings.Builder{}
	builder.WriteString("# ai-dev agent bundle\n\n")
	builder.WriteString("Client: ")
	builder.WriteString(clientName)
	builder.WriteString("\n\n")
	builder.WriteString("Use ai-dev as the source of truth.\n")
	builder.WriteString("Follow the local bundle first. If the client has no API key, network, or compatible model, use the cached offline instructions below.\n")
	builder.WriteString("Before acting, compare the requested feature against the local cached definitions and the resolved prompts/rules/MCP payload.\n")
	builder.WriteString("If the client is unknown, treat the client name as opaque and use the generic bundle path.\n\n")
	builder.WriteString("Source mode: ")
	builder.WriteString(aiSyncSourceMode(paths))
	builder.WriteString("\n\n")
	builder.WriteString("## Project\n\n")
	builder.WriteString(formatContextSectionText(payload["project"]))
	builder.WriteString("\n## Context\n\n")
	builder.WriteString(formatContextSectionText(payload["context"]))
	builder.WriteString("\n## Client\n\n")
	builder.WriteString(formatContextSectionText(payload["client"]))
	if discovery, ok := payload["client_discovery"]; ok {
		builder.WriteString("\n## Client Discovery\n\n")
		builder.WriteString(formatContextSectionText(discovery))
	}
	builder.WriteString("\n## Offline Fallback\n\n")
	builder.WriteString("If the AI client cannot use remote APIs, continue with the cached local definitions from ai-dev.\n")
	builder.WriteString("Do not invent project rules when the cache and current request disagree; prefer the cache and report the mismatch.\n")
	builder.WriteString("When online access is available, verify the client setup against official documentation and update the local cache before use.\n")
	writeImportedResourceSections(&builder, payload)
	return builder.String()
}

func writeImportedResourceSections(builder *strings.Builder, payload map[string]any) {
	builder.WriteString("\n## Managed resource categories\n\n")
	builder.WriteString("ai-dev keeps these resource types distinct and they must be applied according to the client layout:\n\n")
	builder.WriteString("- prompts: reusable task instructions and generation templates\n")
	builder.WriteString("- rules: project constraints and engineering policies\n")
	builder.WriteString("- instructions: client-level operating instructions\n")
	builder.WriteString("- agents: role-specific behavior and delegation definitions\n")
	builder.WriteString("- skills: reusable procedural capabilities, usually represented by SKILL.md\n")
	builder.WriteString("- mcp: MCP server definitions and transport configuration\n")
	builder.WriteString("- client: client-specific native configuration files\n")
	for _, category := range []string{"instructions", "agents", "skills", "mcp", "client"} {
		writeImportedResourceCategory(builder, payload, category, "Imported "+category)
	}
}

func writeImportedResourceCategory(builder *strings.Builder, payload map[string]any, category, title string) {
	resources, ok := payload["resources"].(map[string][]importedAIResource)
	if !ok || len(resources[category]) == 0 {
		return
	}
	builder.WriteString("\n### ")
	builder.WriteString(title)
	builder.WriteString("\n\n")
	for _, resource := range resources[category] {
		builder.WriteString("#### ")
		builder.WriteString(resource.ImportName)
		builder.WriteString("/")
		builder.WriteString(resource.SourcePath)
		builder.WriteString("\n\n")
		builder.WriteString(resource.Content)
		if !strings.HasSuffix(resource.Content, "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
}

func aiSyncSourceMode(paths Paths) string {
	if aiConfigEnvExists(paths) {
		return "ai"
	}
	return "local"
}

func aiConfigEnvExists(paths Paths) bool {
	path := aiConfigEnvPath(paths, false)
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func aiRegistryResolvedDocument(kind string, section map[string]any, paths Paths) string {
	text, _ := section["text"].(string)
	enabled, _ := section["enabled"].([]string)
	var builder strings.Builder
	builder.WriteString("# resolved ")
	builder.WriteString(kind)
	builder.WriteString("\n\n")
	builder.WriteString("source=")
	builder.WriteString(aiSyncSourceMode(paths))
	builder.WriteString("\n\n")
	if len(enabled) > 0 {
		builder.WriteString("enabled:\n")
		for _, identifier := range enabled {
			builder.WriteString("- ")
			builder.WriteString(identifier)
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString(text)
	if !strings.HasSuffix(builder.String(), "\n") {
		builder.WriteString("\n")
	}
	return builder.String()
}

func pathsFromPayload(payload map[string]any) Paths {
	info, _ := payload["project"].(map[string]any)
	paths := Paths{}
	if info != nil {
		if configHome, ok := info["config_home"].(string); ok {
			paths.ConfigHome = configHome
		}
		if dataHome, ok := info["data_home"].(string); ok {
			paths.DataHome = dataHome
		}
		if stateHome, ok := info["state_home"].(string); ok {
			paths.StateHome = stateHome
		}
	}
	return paths
}

func aiWriteClientBundle(plan aiBundlePlan, force bool) error {
	if force && plan.ManifestPath != "" {
		data, err := os.ReadFile(plan.ManifestPath)
		if err == nil {
			previous := []string{}
			if json.Unmarshal(data, &previous) == nil {
				current := map[string]bool{}
				for _, path := range plan.Paths {
					current[path] = true
				}
				for _, path := range previous {
					if current[path] || !filepath.IsAbs(path) {
						continue
					}
					if info, statErr := os.Lstat(path); statErr == nil && !info.IsDir() {
						if removeErr := os.Remove(path); removeErr != nil {
							return removeErr
						}
					}
				}
			}
		}
	}
	for _, path := range plan.Paths {
		if !force {
			if _, err := os.Stat(path); err == nil {
				return UsageError{Message: fmt.Sprintf("output exists: %s (use --force to overwrite)", path)}
			}
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		content := plan.Files[path]
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return err
		}
	}
	if plan.ManifestPath != "" {
		if err := os.MkdirAll(filepath.Dir(plan.ManifestPath), 0o755); err != nil {
			return err
		}
		content, err := json.MarshalIndent(plan.Paths, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(plan.ManifestPath, append(content, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func formatContextSectionText(value any) string {
	content, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("encode error: %v", err)
	}
	return "```json\n" + string(content) + "\n```\n"
}
