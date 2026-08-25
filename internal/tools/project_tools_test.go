package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
)

type recordingReader struct {
	current     caidoread.Project
	currentErr  error
	available   []caidoread.Project
	requestPage caidoread.RequestPage
	request     caidoread.RequestDetails
	sitemap     []caidoread.SitemapEntry
	scopes      []caidoread.Scope

	listRequestsCalls int
	getRequestCalls   int
	getRequestID      string
	sitemapCalls      int
	listScopesCalls   int
	projectAtRead     []string
}

func (r *recordingReader) CurrentProject(context.Context) (caidoread.Project, error) {
	if r.currentErr != nil {
		return caidoread.Project{}, r.currentErr
	}
	return r.current, nil
}

func (r *recordingReader) ListProjects(context.Context) ([]caidoread.Project, error) {
	return r.available, nil
}

func (r *recordingReader) ListRequests(context.Context, caidoread.ListRequestsParams) (caidoread.RequestPage, error) {
	r.listRequestsCalls++
	r.projectAtRead = append(r.projectAtRead, r.current.ID)
	return r.requestPage, nil
}

func (r *recordingReader) GetRequest(_ context.Context, id string) (caidoread.RequestDetails, error) {
	r.getRequestCalls++
	r.getRequestID = id
	r.projectAtRead = append(r.projectAtRead, r.current.ID)
	return r.request, nil
}

func (r *recordingReader) ListSitemap(context.Context, string) ([]caidoread.SitemapEntry, error) {
	r.sitemapCalls++
	r.projectAtRead = append(r.projectAtRead, r.current.ID)
	return r.sitemap, nil
}

func (r *recordingReader) ListScopes(context.Context) ([]caidoread.Scope, error) {
	r.listScopesCalls++
	r.projectAtRead = append(r.projectAtRead, r.current.ID)
	return r.scopes, nil
}

func connectToolClient(t *testing.T, reader caidoread.Reader) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "test"}, nil)
	RegisterAll(server, reader)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		serverSession.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		clientSession.Close()
		serverSession.Close()
	})
	return clientSession
}

func toolErrorText(result *mcp.CallToolResult) string {
	var parts []string
	for _, block := range result.Content {
		if text, ok := block.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func schemaObject(t *testing.T, schema any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func stringSet(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	sort.Strings(result)
	return result
}

func propertyNames(schema map[string]any) []string {
	properties, _ := schema["properties"].(map[string]any)
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func requireStringsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("got %v; want %v", got, want)
	}
}

func TestGetCurrentProjectReturnsFlatDynamicIdentity(t *testing.T) {
	reader := &recordingReader{
		current:   project(testProjectID, testProjectName),
		available: []caidoread.Project{project(testProjectID, testProjectName)},
	}
	client := connectToolClient(t, reader)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "caido_get_current_project",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected tool error: %s", toolErrorText(result))
	}
	output, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type = %T", result.StructuredContent)
	}
	if output["id"] != testProjectID || output["name"] != testProjectName {
		t.Fatalf("unexpected current project: %+v", output)
	}
	if _, nested := output["project"]; nested {
		t.Fatalf("current project output must be flat: %+v", output)
	}
}

func TestGetCurrentProjectReturnsReadableErrorWhenNoneIsActive(t *testing.T) {
	reader := &recordingReader{currentErr: caidoread.ErrNoCurrentProject}
	client := connectToolClient(t, reader)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "caido_get_current_project",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(toolErrorText(result), "no project is selected") {
		t.Fatalf("unexpected no-project result: error=%v content=%q", result.IsError, toolErrorText(result))
	}
}

func TestPublishedProjectAwareSchemasMatchHandlerInputs(t *testing.T) {
	client := connectToolClient(t, &recordingReader{
		current:   project(testProjectID, testProjectName),
		available: []caidoread.Project{project(testProjectID, testProjectName)},
	})
	listed, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		inputProperties  []string
		inputRequired    []string
		outputProperties []string
		outputRequired   []string
	}{
		"caido_get_current_project": {
			inputProperties:  []string{},
			inputRequired:    []string{},
			outputProperties: []string{"id", "name", "readOnly", "status", "version"},
			outputRequired:   []string{"id", "name", "readOnly"},
		},
		"caido_list_requests": {
			inputProperties:  []string{"after", "httpql", "limit", "projectId"},
			inputRequired:    []string{"projectId"},
			outputProperties: []string{"hasMore", "nextCursor", "project", "requests"},
			outputRequired:   []string{"hasMore", "project", "requests"},
		},
		"caido_get_request": {
			inputProperties:  []string{"bodyLimit", "bodyOffset", "id", "include", "projectId"},
			inputRequired:    []string{"id", "projectId"},
			outputProperties: []string{"host", "id", "method", "path", "port", "project", "query", "request", "response", "roundtripMs", "statusCode", "timestamp", "tls", "url"},
			outputRequired:   []string{"host", "id", "method", "path", "port", "project", "timestamp", "tls", "url"},
		},
		"caido_get_sitemap": {
			inputProperties:  []string{"parentId", "projectId"},
			inputRequired:    []string{"projectId"},
			outputProperties: []string{"entries", "project"},
			outputRequired:   []string{"entries", "project"},
		},
	}
	seen := map[string]bool{}
	for _, tool := range listed.Tools {
		expected, ok := want[tool.Name]
		if !ok {
			continue
		}
		seen[tool.Name] = true
		inputSchema := schemaObject(t, tool.InputSchema)
		requireStringsEqual(t, propertyNames(inputSchema), expected.inputProperties)
		requireStringsEqual(t, stringSet(inputSchema["required"]), expected.inputRequired)
		if additional, ok := inputSchema["additionalProperties"].(bool); !ok || additional {
			t.Fatalf("tool %q permits undeclared input properties: %+v", tool.Name, inputSchema)
		}
		outputSchema := schemaObject(t, tool.OutputSchema)
		requireStringsEqual(t, propertyNames(outputSchema), expected.outputProperties)
		requireStringsEqual(t, stringSet(outputSchema["required"]), expected.outputRequired)
		if additional, ok := outputSchema["additionalProperties"].(bool); !ok || additional {
			t.Fatalf("tool %q permits undeclared output properties: %+v", tool.Name, outputSchema)
		}
		formattedInput, err := json.MarshalIndent(inputSchema, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		formattedOutput, err := json.MarshalIndent(outputSchema, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s inputSchema:\n%s", tool.Name, formattedInput)
		t.Logf("%s outputSchema:\n%s", tool.Name, formattedOutput)
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("tools/list omitted %q", name)
		}
	}
}

func TestProjectAwareRequestAndSitemapToolsReadOnlyFromRequestedProject(t *testing.T) {
	reader := &recordingReader{
		current:   project(testProjectID, testProjectName),
		available: []caidoread.Project{project(testProjectID, testProjectName)},
		requestPage: caidoread.RequestPage{Requests: []caidoread.RequestSummary{{
			ID: "2442", Method: "GET", Host: "app.example.test", Port: 443, Path: "/", TLS: true,
		}}},
		request: caidoread.RequestDetails{
			ID: "2442", Method: "GET", Host: "app.example.test", Port: 443, Path: "/", TLS: true,
		},
		sitemap: []caidoread.SitemapEntry{{ID: "root", Label: "app.example.test", Kind: "HOST"}},
	}
	client := connectToolClient(t, reader)

	calls := []struct {
		name string
		args map[string]any
	}{
		{name: "caido_list_requests", args: map[string]any{"projectId": testProjectID, "limit": 1}},
		{name: "caido_get_request", args: map[string]any{"projectId": testProjectID, "id": "2442"}},
		{name: "caido_get_sitemap", args: map[string]any{"projectId": testProjectID}},
	}
	for _, call := range calls {
		result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: call.name, Arguments: call.args})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatalf("%s returned error for schema-valid projectId: %s", call.name, toolErrorText(result))
		}
		output := result.StructuredContent.(map[string]any)
		origin := output["project"].(map[string]any)
		if origin["id"] != testProjectID || origin["name"] != testProjectName {
			t.Fatalf("%s returned wrong origin: %+v", call.name, origin)
		}
	}
	if reader.listRequestsCalls != 1 || reader.getRequestCalls != 1 || reader.sitemapCalls != 1 {
		t.Fatalf("unexpected read counts: list=%d get=%d sitemap=%d", reader.listRequestsCalls, reader.getRequestCalls, reader.sitemapCalls)
	}
	if reader.getRequestID != "2442" {
		t.Fatalf("caido_get_request passed ID %q; want UI row ID 2442", reader.getRequestID)
	}
	for _, projectID := range reader.projectAtRead {
		if projectID != testProjectID {
			t.Fatalf("data read occurred under project %q", projectID)
		}
	}
}

func TestListRequestsRegressionValidPublishedProjectIDIsAccepted(t *testing.T) {
	reader := &recordingReader{
		current:   project(testProjectID, testProjectName),
		available: []caidoread.Project{project(testProjectID, testProjectName)},
	}
	client := connectToolClient(t, reader)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "caido_list_requests",
		Arguments: map[string]any{
			"projectId": testProjectID,
			"limit":     5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		message := toolErrorText(result)
		if strings.Contains(message, `missing properties: ["projectId"]`) {
			t.Fatalf("regression reproduced: %s", message)
		}
		t.Fatalf("unexpected list error: %s", message)
	}
}

func TestProjectAwareToolRejectsInvalidUUIDBeforeReadingData(t *testing.T) {
	reader := &recordingReader{
		current:   project(testProjectID, testProjectName),
		available: []caidoread.Project{project(testProjectID, testProjectName)},
	}
	client := connectToolClient(t, reader)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "caido_list_requests",
		Arguments: map[string]any{"projectId": "not-a-uuid"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(toolErrorText(result), "projectId must be a canonical UUID") {
		t.Fatalf("unexpected invalid-UUID result: error=%v content=%q", result.IsError, toolErrorText(result))
	}
	if reader.listRequestsCalls != 0 {
		t.Fatalf("invalid UUID reached HTTP History: calls=%d", reader.listRequestsCalls)
	}
}

func TestProjectAwareToolRejectsNonexistentProjectBeforeReadingData(t *testing.T) {
	reader := &recordingReader{
		current:   project(testProjectID, testProjectName),
		available: []caidoread.Project{project(testProjectID, testProjectName)},
	}
	client := connectToolClient(t, reader)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "caido_get_sitemap",
		Arguments: map[string]any{"projectId": missingProjectID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(toolErrorText(result), "does not exist") {
		t.Fatalf("unexpected missing-project result: error=%v content=%q", result.IsError, toolErrorText(result))
	}
	if reader.sitemapCalls != 0 {
		t.Fatalf("nonexistent project reached Sitemap: calls=%d", reader.sitemapCalls)
	}
}

func TestNoProjectSelectionMutationExists(t *testing.T) {
	client := connectToolClient(t, &recordingReader{
		current:   project(testProjectID, testProjectName),
		available: []caidoread.Project{project(testProjectID, testProjectName)},
	})
	listed, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == "caido_select_project" {
			t.Fatal("mutation tool caido_select_project must not be registered")
		}
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %q is not explicitly read-only", tool.Name)
		}
	}
}

var _ caidoread.Reader = (*recordingReader)(nil)
