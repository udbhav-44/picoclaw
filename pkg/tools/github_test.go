package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestGitHubTool_Metadata(t *testing.T) {
	tool := NewGitHubTool(config.GitHubConfig{})

	if tool.Name() != "github" {
		t.Errorf("Expected name 'github', got '%s'", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Expected tool to have a description")
	}

	params := tool.Parameters()
	if params == nil {
		t.Error("Expected parameters to not be nil")
	}
}

func TestGitHubTool_Disabled(t *testing.T) {
	tool := NewGitHubTool(config.GitHubConfig{Enabled: false})
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "user_info"})

	if res.Err == nil && !strings.Contains(res.ForLLM, "not enabled") {
		t.Errorf("Expected error when tool is disabled, got: %v", res.ForLLM)
	}
}
