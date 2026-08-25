// Package tools defines the complete MCP surface for CaidoBridge.
package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
)

const (
	ReadOnlyToolCount = 8
	ActiveToolCount   = 2
)

func ToolCount(replayEnabled bool) int {
	if replayEnabled {
		return ReadOnlyToolCount + ActiveToolCount
	}
	return ReadOnlyToolCount
}

// RegisterAll is intentionally limited to observation and local preview tools.
// Active tools require a separate RegisterActiveReplay call.
func RegisterAll(server *mcp.Server, reader caidoread.Reader) *PreviewStore {
	previews := NewPreviewStore()
	registerGetCurrentProject(server, reader)
	registerListProjects(server, reader)
	registerListRequests(server, reader)
	registerGetRequest(server, reader)
	registerGetSitemap(server, reader)
	registerListScopes(server, reader)
	registerIsInScope(server, reader)
	registerPreviewReplay(server, reader, previews)
	return previews
}
