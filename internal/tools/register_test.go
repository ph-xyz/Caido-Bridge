package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
)

const (
	testProjectID    = "1072a2a9-7ba4-4c58-98e9-887b7882a9e7"
	otherProjectID   = "f3c3c296-7608-4662-95ef-111111111111"
	missingProjectID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	testProjectName  = "Authorized Web Test"
	otherProjectName = "Local Security Lab"
)

func schemaRequiredFields(t *testing.T, schema any) map[string]bool {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	fields := make(map[string]bool, len(decoded.Required))
	for _, field := range decoded.Required {
		fields[field] = true
	}
	return fields
}

type fakeReader struct{}

func (fakeReader) CurrentProject(context.Context) (caidoread.Project, error) {
	return caidoread.Project{ID: testProjectID, Name: testProjectName}, nil
}

func (fakeReader) ListProjects(context.Context) ([]caidoread.Project, error) {
	return []caidoread.Project{{ID: testProjectID, Name: testProjectName}}, nil
}

func (fakeReader) ListRequests(context.Context, caidoread.ListRequestsParams) (caidoread.RequestPage, error) {
	return caidoread.RequestPage{}, nil
}

func (fakeReader) GetRequest(context.Context, string) (caidoread.RequestDetails, error) {
	return caidoread.RequestDetails{}, caidoread.ErrRequestNotFound
}

func (fakeReader) ListSitemap(context.Context, string) ([]caidoread.SitemapEntry, error) {
	return nil, nil
}

func (fakeReader) ListScopes(context.Context) ([]caidoread.Scope, error) {
	return nil, nil
}

func TestRegisteredObservationToolsHaveExpectedSafetyBoundaryV030(t *testing.T) {
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "test"}, nil)
	RegisterAll(server, fakeReader{})
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantReadOnly := map[string]bool{
		"caido_get_current_project": true,
		"caido_list_projects":       true,
		"caido_list_requests":       true,
		"caido_get_request":         true,
		"caido_get_sitemap":         true,
		"caido_list_scopes":         true,
		"caido_is_in_scope":         true,
		"caido_preview_replay":      true,
	}
	projectScopedReads := map[string]bool{
		"caido_list_requests":  true,
		"caido_get_request":    true,
		"caido_get_sitemap":    true,
		"caido_list_scopes":    true,
		"caido_is_in_scope":    true,
		"caido_preview_replay": true,
	}
	seen := make(map[string]bool, len(wantReadOnly))
	if len(result.Tools) != ToolCount(false) || len(result.Tools) != len(wantReadOnly) {
		t.Fatalf("registered %d tools; want %d", len(result.Tools), ToolCount(false))
	}
	for _, tool := range result.Tools {
		expectedReadOnly, ok := wantReadOnly[tool.Name]
		if !ok {
			t.Fatalf("unexpected tool %q", tool.Name)
		}
		seen[tool.Name] = true
		ann := tool.Annotations
		if ann == nil || !expectedReadOnly || !ann.ReadOnlyHint || !ann.IdempotentHint {
			t.Fatalf("tool %q has unexpected safety annotations: %+v", tool.Name, ann)
		}
		if ann.OpenWorldHint == nil || *ann.OpenWorldHint {
			t.Fatalf("tool %q is not explicitly closed-world: %+v", tool.Name, ann)
		}
		if ann.DestructiveHint == nil || *ann.DestructiveHint {
			t.Fatalf("tool %q is not explicitly non-destructive: %+v", tool.Name, ann)
		}
		if projectScopedReads[tool.Name] {
			inputRequired := schemaRequiredFields(t, tool.InputSchema)
			if !inputRequired["projectId"] {
				t.Fatalf("tool %q does not require projectId: %+v", tool.Name, tool.InputSchema)
			}
			outputRequired := schemaRequiredFields(t, tool.OutputSchema)
			if !outputRequired["project"] {
				t.Fatalf("tool %q does not require project output: %+v", tool.Name, tool.OutputSchema)
			}
		}
	}
	for name := range wantReadOnly {
		if !seen[name] {
			t.Errorf("required tool %q was not registered", name)
		}
	}
}
