package tools

import (
	"context"

	"github.com/sipeed/picoclaw/pkg/bus"
)

type CheckMailTool struct {
	bus *bus.MessageBus
}

func NewCheckMailTool(bus *bus.MessageBus) *CheckMailTool {
	return &CheckMailTool{
		bus: bus,
	}
}

func (t *CheckMailTool) Name() string {
	return "check_email"
}

func (t *CheckMailTool) Description() string {
	return "Manually check for new emails immediately. Use this when the user asks to check email."
}

func (t *CheckMailTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
		"required":   []string{},
	}
}

func (t *CheckMailTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	// Publish a command to the email channel via outbound bus
	t.bus.PublishOutbound(bus.OutboundMessage{
		Channel: "email",
		ChatID:  "system", // Dummy ID to prevent empty recipient error if it falls through
		Content: "CMD:CHECK",
	})

	return &ToolResult{
		ForLLM:  "Initiated manual check for new emails. Any new emails will appear as messages shortly.",
		ForUser: "Checking for new emails...",
	}
}
