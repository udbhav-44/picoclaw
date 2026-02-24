package tools

import (
	"context"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestCalendarTool_Metadata(t *testing.T) {
	tool := NewCalendarTool(config.CalendarConfig{})

	if tool.Name() != "calendar" {
		t.Errorf("Expected name 'calendar', got '%s'", tool.Name())
	}

	if tool.Description() == "" {
		t.Error("Expected tool to have a description")
	}

	params := tool.Parameters()
	if params == nil {
		t.Error("Expected parameters to not be nil")
	}
}

func TestCalendarTool_ExecuteDisabled(t *testing.T) {
	tool := NewCalendarTool(config.CalendarConfig{Enabled: false})
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "list_events"})

	if res.Err == nil {
		t.Error("Expected error when executing disabled tool")
	}
}

func TestCalendarTool_UnknownAction(t *testing.T) {
	tool := NewCalendarTool(config.CalendarConfig{Enabled: true})
	res := tool.Execute(context.Background(), map[string]interface{}{"action": "unknown_action"})

	if res.Err == nil {
		t.Error("Expected error for unknown action")
	}
}
