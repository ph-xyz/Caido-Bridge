package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
)

type GetSitemapInput struct {
	ProjectID string `json:"projectId" jsonschema:"Required current project ID from caido_get_current_project"`
	ParentID  string `json:"parentId,omitempty" jsonschema:"Parent sitemap entry ID; omit to list root entries"`
}

type SitemapEntry struct {
	ID             string  `json:"id"`
	Label          string  `json:"label"`
	Type           string  `json:"type"`
	Method         *string `json:"method,omitempty"`
	RequestID      *string `json:"requestId,omitempty"`
	StatusCode     *int    `json:"statusCode,omitempty"`
	HasDescendants bool    `json:"hasDescendants"`
}

type GetSitemapOutput struct {
	Project ProjectContext `json:"project"`
	Entries []SitemapEntry `json:"entries"`
}

func registerGetSitemap(server *mcp.Server, reader caidoread.Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_get_sitemap",
		Title:        "Browse the active Caido sitemap",
		Description:  "Read Sitemap entries only after projectId is a valid existing project and matches the MCP caller's current project. Project identity is included in the response.",
		InputSchema:  schemaFor[GetSitemapInput](),
		OutputSchema: schemaFor[GetSitemapOutput](),
		Annotations:  readOnlyAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input GetSitemapInput,
	) (*mcp.CallToolResult, GetSitemapOutput, error) {
		parentID := strings.TrimSpace(input.ParentID)
		if input.ParentID != "" && parentID == "" {
			return nil, GetSitemapOutput{}, fmt.Errorf("parentId cannot contain only whitespace")
		}
		if len(parentID) > maxRequestIDLength {
			return nil, GetSitemapOutput{}, fmt.Errorf("parentId exceeds %d characters", maxRequestIDLength)
		}
		entries, project, err := guardedProjectRead(
			ctx,
			reader,
			input.ProjectID,
			func() ([]caidoread.SitemapEntry, error) {
				return reader.ListSitemap(ctx, parentID)
			},
		)
		if err != nil {
			return nil, GetSitemapOutput{}, fmt.Errorf("read Caido sitemap: %w", err)
		}
		out := GetSitemapOutput{
			Project: projectContext(project),
			Entries: make([]SitemapEntry, 0, len(entries)),
		}
		for _, entry := range entries {
			out.Entries = append(out.Entries, SitemapEntry{
				ID:             entry.ID,
				Label:          entry.Label,
				Type:           entry.Kind,
				Method:         entry.Method,
				RequestID:      entry.RequestID,
				StatusCode:     entry.StatusCode,
				HasDescendants: entry.HasDescendants,
			})
		}
		return nil, out, nil
	})
}
