package claude

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/GujaLomsadze/helm/internal/agent"
	"github.com/GujaLomsadze/helm/internal/claudecfg"
)

// Presets are the provider setups Claude Code documents (code.claude.com/docs: "Connect
// Claude Code to an LLM gateway", "Claude Code on Amazon Bedrock"). Anything they don't
// cover goes in a profile's extra variables.
func (Adapter) Presets() []agent.Preset {
	baseURL := agent.PresetField{Var: "ANTHROPIC_BASE_URL", Label: "Gateway URL", Placeholder: "https://llm-gateway.example.com", Required: true, URL: true}
	return []agent.Preset{
		{ID: "gateway", Label: "LLM gateway (LiteLLM, Portkey, …)", Spend: "litellm",
			Hint: "Your company's gateway with a key it issued. Sent as Authorization: Bearer.",
			Fields: []agent.PresetField{baseURL,
				{Var: "ANTHROPIC_AUTH_TOKEN", Label: "Key", Placeholder: "sk-…", Secret: true, Required: true}}},
		{ID: "gateway-x-api-key", Label: "LLM gateway, x-api-key header", Spend: "litellm",
			Hint: "For a gateway that reads the x-api-key header. Claude Code asks once to approve the key.",
			Fields: []agent.PresetField{baseURL,
				{Var: "ANTHROPIC_API_KEY", Label: "Key", Placeholder: "sk-…", Secret: true, Required: true}}},
		{ID: "anthropic-api", Label: "Anthropic API key",
			Hint: "Pay per token on a Console account. Claude Code asks once to approve the key.",
			Fields: []agent.PresetField{
				{Var: "ANTHROPIC_API_KEY", Label: "API key", Placeholder: "sk-ant-…", Secret: true, Required: true}}},
		{ID: "bedrock-key", Label: "Amazon Bedrock API key",
			Fixed: map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1"},
			Fields: []agent.PresetField{
				{Var: "AWS_BEARER_TOKEN_BEDROCK", Label: "Bedrock API key", Secret: true, Required: true},
				{Var: "AWS_REGION", Label: "Region", Placeholder: "us-east-1"}}},
		{ID: "bedrock-profile", Label: "Amazon Bedrock, AWS profile",
			Hint:  "Uses a profile from ~/.aws (run aws sso login yourself when it expires).",
			Fixed: map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1"},
			Fields: []agent.PresetField{
				{Var: "AWS_PROFILE", Label: "AWS profile", Placeholder: "default", Required: true},
				{Var: "AWS_REGION", Label: "Region", Placeholder: "us-east-1"}}},
		{ID: "bedrock-gateway", Label: "Amazon Bedrock through a gateway", Spend: "litellm",
			Hint:  "The gateway holds the AWS credentials; Claude Code doesn't sign requests.",
			Fixed: map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1", "CLAUDE_CODE_SKIP_BEDROCK_AUTH": "1"},
			Fields: []agent.PresetField{
				{Var: "ANTHROPIC_BEDROCK_BASE_URL", Label: "Gateway URL", Placeholder: "https://llm-gateway.example.com/bedrock", Required: true, URL: true},
				{Var: "ANTHROPIC_AUTH_TOKEN", Label: "Gateway key", Placeholder: "if the gateway wants one", Secret: true}}},
		{ID: "vertex-gateway", Label: "Google Vertex through a gateway", Spend: "litellm",
			Fixed: map[string]string{"CLAUDE_CODE_USE_VERTEX": "1", "CLAUDE_CODE_SKIP_VERTEX_AUTH": "1"},
			Fields: []agent.PresetField{
				{Var: "ANTHROPIC_VERTEX_BASE_URL", Label: "Gateway URL", Placeholder: "https://llm-gateway.example.com/vertex", Required: true, URL: true},
				{Var: "ANTHROPIC_VERTEX_PROJECT_ID", Label: "GCP project", Required: true},
				{Var: "CLOUD_ML_REGION", Label: "Region", Placeholder: "us-east5", Required: true},
				{Var: "ANTHROPIC_AUTH_TOKEN", Label: "Gateway key", Placeholder: "if the gateway wants one", Secret: true}}},
		{ID: "custom", Label: "Custom",
			Hint:   "Only the extra variables you add below.",
			Fields: []agent.PresetField{}},
	}
}

// providerVar: the variables that choose where Claude Code sends requests and how it
// authenticates. CLAUDE_CODE_* never reaches a terminal anyway (PrivateEnv).
func providerVar(k string) bool {
	switch k {
	case "AWS_BEARER_TOKEN_BEDROCK", "AWS_PROFILE", "AWS_REGION", "CLOUD_ML_REGION":
		return true
	}
	return strings.HasPrefix(k, "ANTHROPIC_")
}

func (Adapter) ProfileScrub(k string) bool { return providerVar(k) }

// ProfileConflicts: Claude Code applies the env block of its user settings over the
// process environment, so a provider set there wins over any profile.
func (Adapter) ProfileConflicts() []string {
	p, err := claudecfg.DefaultPaths()
	if err != nil {
		return nil
	}
	return settingsConflicts(p.Settings)
}

func settingsConflicts(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s struct {
		Env        map[string]any `json:"env"`
		APIKeyHelp string         `json:"apiKeyHelper"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	var out []string
	for k := range s.Env {
		if providerVar(k) || strings.HasPrefix(k, "CLAUDE_CODE_USE_") || strings.HasPrefix(k, "CLAUDE_CODE_SKIP_") {
			out = append(out, path+": env."+k)
		}
	}
	sort.Strings(out)
	if s.APIKeyHelp != "" {
		out = append(out, path+": apiKeyHelper")
	}
	return out
}
