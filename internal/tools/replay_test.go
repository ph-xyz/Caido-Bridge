package tools

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoreplay"
)

const replayScopeID = "scope-1"

func capturedReplayRecord() caidoread.RequestDetails {
	requestRaw := "PATCH /backend/cart/v1/unit/612633976?quantity=5 HTTP/1.1\r\n" +
		"Host: www.example.com\r\n" +
		"Cookie: session=secret\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: 14\r\n\r\n" +
		`{"quantity":5}`
	responseRaw := "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n" +
		`{"id_cart_unit":"612633976","quantity":5}`
	return caidoread.RequestDetails{
		ID:     "889",
		Method: "PATCH",
		Host:   "www.example.com",
		Port:   443,
		Path:   "/backend/cart/v1/unit/612633976",
		Query:  "quantity=5",
		TLS:    true,
		Raw:    base64.StdEncoding.EncodeToString([]byte(requestRaw)),
		Response: &caidoread.ResponseDetails{
			StatusCode:  200,
			Raw:         base64.StdEncoding.EncodeToString([]byte(responseRaw)),
			RoundtripMS: 123,
			Length:      len(responseRaw),
		},
	}
}

func replayReader() *recordingReader {
	return &recordingReader{
		current:   project(testProjectID, testProjectName),
		available: []caidoread.Project{project(testProjectID, testProjectName)},
		request:   capturedReplayRecord(),
		scopes: []caidoread.Scope{
			{ID: "scope-1", Name: "Example VDP", Allowlist: []string{"*.example.com"}},
		},
	}
}

func expectedReplayIdentity() ExpectedRequestIdentity {
	return ExpectedRequestIdentity{
		Method: "PATCH",
		Host:   "www.example.com",
		Path:   "/backend/cart/v1/unit/612633976",
	}
}

func TestPrepareReplayValidatesIdentityScopeAndOneMutation(t *testing.T) {
	reader := replayReader()
	prepared, err := prepareReplay(
		context.Background(),
		reader,
		testProjectID,
		"889",
		replayScopeID,
		expectedReplayIdentity(),
		[]ReplayMutationInput{{
			Type:   "change_path_segment",
			Target: "4",
			From:   "612633976",
			To:     "612679217",
		}},
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Source.Target != "/backend/cart/v1/unit/612633976?quantity=5" {
		t.Fatalf("source was modified: %q", prepared.Source.Target)
	}
	if prepared.Prepared.Target != "/backend/cart/v1/unit/612679217?quantity=5" {
		t.Fatalf("mutation not isolated: %q", prepared.Prepared.Target)
	}
	if !prepared.Scope.InScope || !prepared.PreparedView.PotentiallyStateChanging {
		t.Fatalf("missing safety facts: %+v %+v", prepared.Scope, prepared.PreparedView)
	}
	if !strings.Contains(prepared.PreparedView.RawPreview, "Cookie: [REDACTED: preserved]") ||
		strings.Contains(prepared.PreparedView.RawPreview, "session=secret") {
		t.Fatalf("preview leaked authentication: %s", prepared.PreparedView.RawPreview)
	}
	if prepared.SourceView.SourceFingerprint == "" {
		t.Fatal("source fingerprint missing")
	}
	if !prepared.PreparedView.Authentication.MaterialPresent ||
		!prepared.PreparedView.Authentication.ValuesRedacted ||
		!prepared.PreparedView.SensitiveHeadersRedacted ||
		prepared.PreparedView.BodyRedactionApplied {
		t.Fatalf("redaction facts are inaccurate: %+v", prepared.PreparedView)
	}
	if !strings.Contains(prepared.PreparedView.RawPreview, `{"quantity":5}`) {
		t.Fatal("body should remain visible because body redaction was not applied")
	}
}

func TestPrepareReplayBlocksWrongIDIdentityAndOutOfScope(t *testing.T) {
	reader := replayReader()
	reader.request.ID = "890"
	_, err := prepareReplay(
		context.Background(), reader, testProjectID, "889", replayScopeID,
		expectedReplayIdentity(), nil, false,
	)
	if err == nil || !strings.Contains(err.Error(), "ID inconsistency") {
		t.Fatalf("wrong row was not blocked: %v", err)
	}

	reader = replayReader()
	reader.scopes[0].Allowlist = []string{"other.example"}
	_, err = prepareReplay(
		context.Background(), reader, testProjectID, "889", replayScopeID,
		expectedReplayIdentity(), nil, false,
	)
	if err == nil || !strings.Contains(err.Error(), "outside Caido scope") {
		t.Fatalf("out-of-scope host was not blocked: %v", err)
	}
}

func TestPrepareReplayRequiresExplicitMultipleMutationOverride(t *testing.T) {
	_, err := prepareReplay(
		context.Background(), replayReader(), testProjectID, "889", replayScopeID,
		expectedReplayIdentity(),
		[]ReplayMutationInput{
			{Type: "change_method", From: "PATCH", To: "GET"},
			{Type: "remove_header", Target: "Content-Type"},
		},
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "one-mutation rule") {
		t.Fatalf("multiple mutations were not blocked: %v", err)
	}
}

func TestPrepareReplayAllowsMoreThanThreeExplicitMutationsWithOverride(t *testing.T) {
	mutations := []ReplayMutationInput{
		{Type: "add_header", Target: "X-Hunt-Step-1", To: "one"},
		{Type: "add_header", Target: "X-Hunt-Step-2", To: "two"},
		{Type: "add_header", Target: "X-Hunt-Step-3", To: "three"},
		{Type: "add_header", Target: "X-Hunt-Step-4", To: "four"},
	}
	prepared, err := prepareReplay(
		context.Background(), replayReader(), testProjectID, "889", replayScopeID,
		expectedReplayIdentity(), mutations, true,
	)
	if err != nil {
		t.Fatalf("explicit multi-mutation Replay was capped: %v", err)
	}
	for _, mutation := range mutations {
		if !strings.Contains(prepared.PreparedView.RawPreview, mutation.Target+": "+mutation.To) {
			t.Fatalf("mutation %s missing from preview: %s", mutation.Target, prepared.PreparedView.RawPreview)
		}
	}
}

type fakeReplayExecutor struct {
	startCalls    int
	continueCalls int
	lastSessionID string
	lastEntryID   string
	lastRaw       []byte
	responseRaw   []byte
}

func (f *fakeReplayExecutor) Start(
	_ context.Context,
	_ caidoreplay.Connection,
	raw []byte,
) (caidoreplay.Result, error) {
	f.startCalls++
	f.lastRaw = append([]byte(nil), raw...)
	return caidoreplay.Result{
		SessionID:   "session-1",
		EntryID:     "entry-1",
		RequestRaw:  append([]byte(nil), raw...),
		ResponseRaw: append([]byte(nil), f.responseRaw...),
		StatusCode:  200,
		RoundtripMS: 99,
		Length:      len(f.responseRaw),
	}, nil
}

func (f *fakeReplayExecutor) Continue(
	_ context.Context,
	sessionID string,
	entryID string,
	_ caidoreplay.Connection,
	raw []byte,
) (caidoreplay.Result, error) {
	f.continueCalls++
	f.lastSessionID = sessionID
	f.lastEntryID = entryID
	f.lastRaw = append([]byte(nil), raw...)
	return caidoreplay.Result{
		SessionID:   sessionID,
		EntryID:     "entry-2",
		RequestRaw:  append([]byte(nil), raw...),
		ResponseRaw: append([]byte(nil), f.responseRaw...),
		StatusCode:  200,
		RoundtripMS: 101,
		Length:      len(f.responseRaw),
	}, nil
}

func connectReplayClient(
	t *testing.T,
	reader caidoread.Reader,
	executor caidoreplay.Executor,
) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "test"}, nil)
	previews := RegisterAll(server, reader)
	RegisterActiveReplay(server, reader, executor, previews)
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

func previewReplay(
	t *testing.T,
	client *mcp.ClientSession,
	requestID string,
	expected ExpectedRequestIdentity,
	mutation map[string]any,
) (string, map[string]any) {
	t.Helper()
	arguments := map[string]any{
		"projectId": testProjectID,
		"scopeId":   replayScopeID,
		"requestId": requestID,
		"expected": map[string]any{
			"method": expected.Method,
			"host":   expected.Host,
			"path":   expected.Path,
		},
		"mutations": []map[string]any{mutation},
	}
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "caido_preview_replay",
		Arguments: arguments,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("preview tool error: %s", toolErrorText(result))
	}
	output, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("preview structured content type = %T", result.StructuredContent)
	}
	token, ok := output["previewToken"].(string)
	if !ok || token == "" || output["previewExpiresAt"] == "" {
		t.Fatalf("preview grant missing: %#v", output)
	}
	source, ok := output["source"].(map[string]any)
	if !ok {
		t.Fatalf("preview source type = %T", output["source"])
	}
	fingerprint, ok := source["sourceFingerprint"].(string)
	if !ok || fingerprint == "" {
		t.Fatalf("preview fingerprint missing: %#v", source)
	}
	return token, map[string]any{
		"method":      expected.Method,
		"host":        expected.Host,
		"path":        expected.Path,
		"fingerprint": fingerprint,
	}
}

func TestHypothesisUsesHistoricalBaselineAndOneActiveSend(t *testing.T) {
	responseRaw := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n" +
		`{"id_cart_unit":"612679217","quantity":5}`)
	executor := &fakeReplayExecutor{responseRaw: responseRaw}
	client := connectReplayClient(t, replayReader(), executor)
	mutation := map[string]any{
		"type":   "change_path_segment",
		"target": "4",
		"from":   "612633976",
		"to":     "612679217",
	}
	previewToken, confirmed := previewReplay(
		t, client, "889", expectedReplayIdentity(), mutation,
	)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "caido_test_hypothesis",
		Arguments: map[string]any{
			"projectId":          testProjectID,
			"scopeId":            replayScopeID,
			"requestId":          "889",
			"previewToken":       previewToken,
			"expected":           confirmed,
			"hypothesis":         "A second object ID is accepted in the same authenticated context",
			"mutation":           mutation,
			"confirmExecution":   true,
			"allowStateChanging": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool error: %s", toolErrorText(result))
	}
	if executor.startCalls != 1 || executor.continueCalls != 0 {
		t.Fatalf("historical baseline send pattern changed: start=%d continue=%d", executor.startCalls, executor.continueCalls)
	}
	if !strings.Contains(string(executor.lastRaw), "/612679217?quantity=5") ||
		!strings.Contains(string(executor.lastRaw), "session=secret") {
		t.Fatalf("test request did not preserve auth and isolate the mutation: %s", executor.lastRaw)
	}
	output, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structured content type = %T", result.StructuredContent)
	}
	if output["baselineSource"] != "historical" {
		t.Fatalf("baselineSource = %v", output["baselineSource"])
	}
	if _, subjective := output["vulnerable"]; subjective {
		t.Fatal("tool emitted a vulnerability verdict")
	}
	if _, obsolete := output["requestBudget"]; obsolete {
		t.Fatal("tool emitted the removed requestBudget field")
	}
}

func TestHypothesisUsesOneReplaySessionForLiveBaselineAndTest(t *testing.T) {
	reader := replayReader()
	requestRaw := "GET /catalog?id=1 HTTP/1.1\r\n" +
		"Host: www.example.com\r\n" +
		"Cookie: session=secret\r\n\r\n"
	responseRaw := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n" +
		`{"id":2,"visible":true}`)
	reader.request = caidoread.RequestDetails{
		ID: "900", Method: "GET", Host: "www.example.com", Port: 443,
		Path: "/catalog", Query: "id=1", TLS: true,
		Raw: base64.StdEncoding.EncodeToString([]byte(requestRaw)),
		Response: &caidoread.ResponseDetails{
			StatusCode: 200,
			Raw: base64.StdEncoding.EncodeToString([]byte(
				"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n" +
					`{"id":1,"visible":true}`,
			)),
		},
	}
	expected := ExpectedRequestIdentity{
		Method: "GET", Host: "www.example.com", Path: "/catalog",
	}
	mutation := map[string]any{
		"type": "replace_query_parameter", "target": "id", "from": "1", "to": "2",
	}
	executor := &fakeReplayExecutor{responseRaw: responseRaw}
	client := connectReplayClient(t, reader, executor)
	previewToken, confirmed := previewReplay(t, client, "900", expected, mutation)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "caido_test_hypothesis",
		Arguments: map[string]any{
			"projectId":        testProjectID,
			"scopeId":          replayScopeID,
			"requestId":        "900",
			"previewToken":     previewToken,
			"expected":         confirmed,
			"hypothesis":       "A second catalog object produces a distinguishable response",
			"mutation":         mutation,
			"confirmExecution": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool error: %s", toolErrorText(result))
	}
	if executor.startCalls != 1 || executor.continueCalls != 1 ||
		executor.lastSessionID != "session-1" || executor.lastEntryID != "entry-1" {
		t.Fatalf("baseline/test session association failed: %+v", executor)
	}
	if !strings.Contains(string(executor.lastRaw), "/catalog?id=2") {
		t.Fatalf("test mutation was not sent: %s", executor.lastRaw)
	}
	output, ok := result.StructuredContent.(map[string]any)
	if !ok || output["baselineSource"] != "live_replay" {
		t.Fatalf("unexpected structured output: %#v", result.StructuredContent)
	}
}

func TestActiveReplayRejectsMissingPreviewFingerprintBeforeSend(t *testing.T) {
	executor := &fakeReplayExecutor{}
	client := connectReplayClient(t, replayReader(), executor)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "caido_replay_request",
		Arguments: map[string]any{
			"projectId": testProjectID,
			"scopeId":   replayScopeID,
			"requestId": "889",
			"expected": map[string]any{
				"method": "PATCH",
				"host":   "www.example.com",
				"path":   "/backend/cart/v1/unit/612633976",
			},
			"confirmExecution":   true,
			"allowStateChanging": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(toolErrorText(result), "fingerprint") {
		t.Fatalf("missing fingerprint error = %#v", result)
	}
	if executor.startCalls != 0 || executor.continueCalls != 0 {
		t.Fatalf("request was sent without preview fingerprint: %+v", executor)
	}
}

func TestActiveReplayRejectsRequestDifferentFromPreview(t *testing.T) {
	responseRaw := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{}")
	executor := &fakeReplayExecutor{responseRaw: responseRaw}
	client := connectReplayClient(t, replayReader(), executor)
	previewMutation := map[string]any{
		"type": "change_path_segment", "target": "4", "from": "612633976", "to": "612679217",
	}
	previewToken, confirmed := previewReplay(
		t, client, "889", expectedReplayIdentity(), previewMutation,
	)
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "caido_replay_request",
		Arguments: map[string]any{
			"projectId":    testProjectID,
			"scopeId":      replayScopeID,
			"requestId":    "889",
			"previewToken": previewToken,
			"expected":     confirmed,
			"mutations": []map[string]any{{
				"type": "change_path_segment", "target": "4", "from": "612633976", "to": "612600000",
			}},
			"confirmExecution":   true,
			"allowStateChanging": true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.Contains(toolErrorText(result), "does not match") {
		t.Fatalf("mismatched preview error = %#v", result)
	}
	if executor.startCalls != 0 {
		t.Fatalf("request changed after preview was sent: %+v", executor)
	}
}

func TestActiveReplayConsumesPreviewTokenOnce(t *testing.T) {
	responseRaw := []byte("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{}")
	executor := &fakeReplayExecutor{responseRaw: responseRaw}
	client := connectReplayClient(t, replayReader(), executor)
	mutation := map[string]any{
		"type": "change_path_segment", "target": "4", "from": "612633976", "to": "612679217",
	}
	previewToken, confirmed := previewReplay(
		t, client, "889", expectedReplayIdentity(), mutation,
	)
	arguments := map[string]any{
		"projectId":          testProjectID,
		"scopeId":            replayScopeID,
		"requestId":          "889",
		"previewToken":       previewToken,
		"expected":           confirmed,
		"mutations":          []map[string]any{mutation},
		"confirmExecution":   true,
		"allowStateChanging": true,
	}
	first, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "caido_replay_request", Arguments: arguments,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.IsError {
		t.Fatalf("first use failed: %s", toolErrorText(first))
	}
	second, err := client.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "caido_replay_request", Arguments: arguments,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.IsError || !strings.Contains(toolErrorText(second), "already used") {
		t.Fatalf("reused preview token error = %#v", second)
	}
	if executor.startCalls != 1 {
		t.Fatalf("preview token authorized %d sends; want 1", executor.startCalls)
	}
}

func TestActiveReplayToolsAreExplicitlyAnnotated(t *testing.T) {
	executor := &fakeReplayExecutor{}
	client := connectReplayClient(t, replayReader(), executor)
	listed, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tool := range listed.Tools {
		if tool.Name != "caido_replay_request" && tool.Name != "caido_test_hypothesis" {
			continue
		}
		seen++
		ann := tool.Annotations
		if ann == nil || ann.ReadOnlyHint || ann.IdempotentHint ||
			ann.DestructiveHint == nil || !*ann.DestructiveHint ||
			ann.OpenWorldHint == nil || !*ann.OpenWorldHint {
			t.Fatalf("active tool %q annotations = %+v", tool.Name, ann)
		}
	}
	if seen != ActiveToolCount {
		t.Fatalf("active tools seen = %d; want %d", seen, ActiveToolCount)
	}
}
