package tools

import "github.com/modelcontextprotocol/go-sdk/mcp"

func boolPtr(value bool) *bool { return &value }

// readOnlyAnnotations makes every safety hint explicit. These hints are backed
// by the loopback-only configuration guard and the narrow caidoread.Reader
// interface; they are not merely labels.
func readOnlyAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    true,
		DestructiveHint: boolPtr(false),
		IdempotentHint:  true,
		OpenWorldHint:   boolPtr(false),
	}
}

// activeReplayAnnotations are intentionally conservative: the tools can send
// target traffic and may change application state.
func activeReplayAnnotations() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: boolPtr(true),
		IdempotentHint:  false,
		OpenWorldHint:   boolPtr(true),
	}
}
