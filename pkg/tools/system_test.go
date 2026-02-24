package tools

import (
	"context"
	"testing"
)

func TestSystemTool_Metadata(t *testing.T) {
	tool := NewSystemTool()

	if tool.Name() != "system_stats" {
		t.Errorf("Expected name 'system_stats', got '%s'", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Expected tool to have a description")
	}

	params := tool.Parameters()
	if params == nil {
		t.Error("Expected parameters to not be nil")
	}
}

func TestSystemTool_Execute(t *testing.T) {
	tool := NewSystemTool()
	res := tool.Execute(context.Background(), nil)

	if res == nil {
		t.Fatal("Expected ToolResult to not be nil")
	}

	if res.Err != nil {
		t.Errorf("Expected no error, got: %v", res.Err)
	}

	if res.ForLLM == "" {
		t.Error("Expected ForLLM to contain stats")
	}
}
