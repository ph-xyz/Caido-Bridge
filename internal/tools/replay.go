package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoreplay"
	replaycore "github.com/ph-xyz/Caido-Bridge/internal/replay"
)

const (
	maxHypothesisLength = 4_096
	maxPreviewBytes     = 64 * 1024
)

type ExpectedRequestIdentity struct {
	Method      string `json:"method" jsonschema:"Expected HTTP method of the selected History row"`
	Host        string `json:"host" jsonschema:"Expected host of the selected History row, without scheme"`
	Path        string `json:"path" jsonschema:"Expected path of the selected History row"`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"Optional sha256 fingerprint from an earlier preview"`
}

type ConfirmedRequestIdentity struct {
	Method      string `json:"method" jsonschema:"Expected HTTP method from caido_preview_replay"`
	Host        string `json:"host" jsonschema:"Expected host from caido_preview_replay, without scheme"`
	Path        string `json:"path" jsonschema:"Expected path from caido_preview_replay"`
	Fingerprint string `json:"fingerprint" jsonschema:"Required source SHA-256 fingerprint returned by caido_preview_replay"`
}

type ReplayMutationInput struct {
	Type   string `json:"type" jsonschema:"One structured mutation recipe: change_method, replace_path, change_path_segment, add_query_parameter, replace_query_parameter, remove_query_parameter, add_header, replace_header, remove_header, replace_cookie, remove_cookie, replace_json_field, remove_json_field, or replace_body"`
	Target string `json:"target,omitempty" jsonschema:"Parameter, header, cookie, JSON field path, or zero-based path segment index"`
	From   string `json:"from,omitempty" jsonschema:"Optional expected original value; mutation fails if it does not match"`
	To     string `json:"to,omitempty" jsonschema:"Replacement or added value"`
	Format string `json:"format,omitempty" jsonschema:"For replace_json_field only: string (default) or json"`
}

type PreviewReplayInput struct {
	ProjectID              string                  `json:"projectId" jsonschema:"Required current project ID from caido_get_current_project"`
	ScopeID                string                  `json:"scopeId" jsonschema:"Required exact scope ID from caido_list_scopes"`
	RequestID              string                  `json:"requestId" jsonschema:"Decimal ID shown in Caido HTTP History"`
	Expected               ExpectedRequestIdentity `json:"expected" jsonschema:"Required independent identity check for the selected History row"`
	Mutations              []ReplayMutationInput   `json:"mutations,omitempty" jsonschema:"Explicit structured mutations; zero mutations previews the original request"`
	AllowMultipleMutations bool                    `json:"allowMultipleMutations,omitempty" jsonschema:"Must be true when more than one primary mutation is intentionally requested"`
}

type ReplayRequestInput struct {
	ProjectID              string                   `json:"projectId" jsonschema:"Required current project ID from caido_get_current_project"`
	ScopeID                string                   `json:"scopeId" jsonschema:"Required exact scope ID used by caido_preview_replay"`
	RequestID              string                   `json:"requestId" jsonschema:"Decimal ID shown in Caido HTTP History"`
	PreviewToken           string                   `json:"previewToken" jsonschema:"Required one-use token returned by caido_preview_replay"`
	Expected               ConfirmedRequestIdentity `json:"expected" jsonschema:"Required identity and fingerprint copied from caido_preview_replay"`
	Mutations              []ReplayMutationInput    `json:"mutations,omitempty" jsonschema:"Explicit structured mutations; only these fields are changed"`
	AllowMultipleMutations bool                     `json:"allowMultipleMutations,omitempty" jsonschema:"Must be true when more than one primary mutation is intentionally requested"`
	ConfirmExecution       bool                     `json:"confirmExecution" jsonschema:"Must be true to send target traffic; call caido_preview_replay first"`
	AllowStateChanging     bool                     `json:"allowStateChanging,omitempty" jsonschema:"Must be true to send a method other than GET, HEAD, or OPTIONS"`
}

type TestHypothesisInput struct {
	ProjectID                  string                   `json:"projectId" jsonschema:"Required current project ID from caido_get_current_project"`
	ScopeID                    string                   `json:"scopeId" jsonschema:"Required exact scope ID used by caido_preview_replay"`
	RequestID                  string                   `json:"requestId" jsonschema:"Decimal ID shown in Caido HTTP History"`
	PreviewToken               string                   `json:"previewToken" jsonschema:"Required one-use token returned by caido_preview_replay"`
	Expected                   ConfirmedRequestIdentity `json:"expected" jsonschema:"Required identity and fingerprint copied from caido_preview_replay"`
	Hypothesis                 string                   `json:"hypothesis" jsonschema:"Specific hypothesis this single controlled mutation is intended to test"`
	Mutation                   ReplayMutationInput      `json:"mutation" jsonschema:"Exactly one primary controlled mutation"`
	BaselineSource             string                   `json:"baselineSource,omitempty" jsonschema:"auto (default), historical, or live_replay"`
	ConfirmExecution           bool                     `json:"confirmExecution" jsonschema:"Must be true to send target traffic; call caido_preview_replay first"`
	AllowStateChanging         bool                     `json:"allowStateChanging,omitempty" jsonschema:"Must be true when the test request may change state"`
	AllowStateChangingBaseline bool                     `json:"allowStateChangingBaseline,omitempty" jsonschema:"Must be true to live-replay a potentially state-changing baseline"`
}

type HeaderView struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type AuthenticationContext struct {
	HeaderNames     []string `json:"headerNames"`
	CookieNames     []string `json:"cookieNames"`
	MaterialPresent bool     `json:"materialPresent"`
	ValuesRedacted  bool     `json:"valuesRedacted"`
}

type RequestView struct {
	SourceRequestID            string                `json:"sourceRequestId"`
	Method                     string                `json:"method"`
	Scheme                     string                `json:"scheme"`
	Host                       string                `json:"host"`
	Port                       int                   `json:"port"`
	Path                       string                `json:"path"`
	Query                      string                `json:"query,omitempty"`
	Headers                    []HeaderView          `json:"headers"`
	Cookies                    []string              `json:"cookies"`
	BodySize                   int                   `json:"bodySize"`
	ContentType                string                `json:"contentType,omitempty"`
	OriginalResponseStatus     *int                  `json:"originalResponseStatus,omitempty"`
	OriginalResponseSize       *int                  `json:"originalResponseSize,omitempty"`
	SourceFingerprint          string                `json:"sourceFingerprint"`
	PreparedRequestFingerprint string                `json:"preparedRequestFingerprint"`
	ExpectedIdentityValidated  bool                  `json:"expectedIdentityValidated"`
	PotentiallyStateChanging   bool                  `json:"potentiallyStateChanging"`
	Authentication             AuthenticationContext `json:"authentication"`
	RawPreview                 string                `json:"rawPreview"`
	PreviewTruncated           bool                  `json:"previewTruncated"`
	SensitiveHeadersRedacted   bool                  `json:"sensitiveHeadersRedacted"`
	BodyRedactionApplied       bool                  `json:"bodyRedactionApplied"`
}

type ScopeDecision struct {
	InScope      bool            `json:"inScope"`
	MatchedScope *ScopeReference `json:"matchedScope,omitempty"`
	MatchedRule  string          `json:"matchedRule,omitempty"`
	Reason       string          `json:"reason"`
}

type PreviewReplayOutput struct {
	Project          ProjectContext        `json:"project"`
	Source           RequestView           `json:"source"`
	Prepared         RequestView           `json:"prepared"`
	Mutations        []ReplayMutationInput `json:"mutations"`
	Scope            ScopeDecision         `json:"scope"`
	PreviewToken     string                `json:"previewToken"`
	PreviewExpiresAt string                `json:"previewExpiresAt"`
	WillSend         bool                  `json:"willSend"`
}

type ResponseEvidence struct {
	Source          string       `json:"source"`
	Status          int          `json:"status"`
	BodySize        int          `json:"bodySize"`
	RecordedSize    int          `json:"recordedSize"`
	RoundtripMS     int          `json:"roundtripMs,omitempty"`
	ContentType     string       `json:"contentType,omitempty"`
	Redirect        string       `json:"redirect,omitempty"`
	Headers         []HeaderView `json:"headers"`
	Body            string       `json:"body,omitempty"`
	BodyEncoding    string       `json:"bodyEncoding,omitempty"`
	BodyTruncated   bool         `json:"bodyTruncated"`
	BodySHA256      string       `json:"bodySha256"`
	ReplaySessionID string       `json:"replaySessionId,omitempty"`
	ReplayEntryID   string       `json:"replayEntryId,omitempty"`
}

type ReplayEvidence struct {
	Request  RequestView      `json:"request"`
	Response ResponseEvidence `json:"response"`
}

type ReplayRequestOutput struct {
	Project         ProjectContext        `json:"project"`
	Timestamp       string                `json:"timestamp"`
	SourceRequestID string                `json:"sourceRequestId"`
	Mutations       []ReplayMutationInput `json:"mutations"`
	Test            ReplayEvidence        `json:"test"`
}

type HeaderDifference struct {
	Name     string `json:"name"`
	Baseline string `json:"baseline,omitempty"`
	Test     string `json:"test,omitempty"`
}

type ResponseDiff struct {
	StatusChanged         bool               `json:"statusChanged"`
	BaselineStatus        int                `json:"baselineStatus"`
	TestStatus            int                `json:"testStatus"`
	SizeDelta             int                `json:"sizeDelta"`
	ContentTypeChanged    bool               `json:"contentTypeChanged"`
	BaselineContentType   string             `json:"baselineContentType,omitempty"`
	TestContentType       string             `json:"testContentType,omitempty"`
	RedirectChanged       bool               `json:"redirectChanged"`
	BaselineRedirect      string             `json:"baselineRedirect,omitempty"`
	TestRedirect          string             `json:"testRedirect,omitempty"`
	RelevantHeaderChanges []HeaderDifference `json:"relevantHeaderChanges"`
	BodyEqual             bool               `json:"bodyEqual"`
	BaselineBodySHA256    string             `json:"baselineBodySha256"`
	TestBodySHA256        string             `json:"testBodySha256"`
	JSONCompared          bool               `json:"jsonCompared"`
	JSONStructureChanged  bool               `json:"jsonStructureChanged"`
	ChangedJSONFields     []string           `json:"changedJsonFields"`
	AddedJSONFields       []string           `json:"addedJsonFields"`
	RemovedJSONFields     []string           `json:"removedJsonFields"`
}

type TestHypothesisOutput struct {
	Project         ProjectContext      `json:"project"`
	Timestamp       string              `json:"timestamp"`
	Hypothesis      string              `json:"hypothesis"`
	SourceRequestID string              `json:"sourceRequestId"`
	Mutation        ReplayMutationInput `json:"mutation"`
	BaselineSource  string              `json:"baselineSource"`
	Control         ReplayEvidence      `json:"control"`
	Test            ReplayEvidence      `json:"test"`
	Diff            ResponseDiff        `json:"diff"`
}

type replaySourceData struct {
	Record caidoread.RequestDetails
	Scopes []caidoread.Scope
}

type preparedReplay struct {
	Project      caidoread.Project
	Record       caidoread.RequestDetails
	Connection   caidoreplay.Connection
	Source       *replaycore.Request
	SourceRaw    []byte
	Prepared     *replaycore.Request
	PreparedRaw  []byte
	SourceView   RequestView
	PreparedView RequestView
	Scope        ScopeDecision
}

func registerPreviewReplay(
	server *mcp.Server,
	reader caidoread.Reader,
	previews *PreviewStore,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_preview_replay",
		Title:        "Preview one controlled Caido Replay",
		Description:  "Read and validate a selected HTTP History row, apply only explicit local mutations, enforce same-host and Caido scope guards, and show the redacted request that would be sent. This tool never sends target traffic.",
		InputSchema:  schemaFor[PreviewReplayInput](),
		OutputSchema: schemaFor[PreviewReplayOutput](),
		Annotations:  readOnlyAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input PreviewReplayInput,
	) (*mcp.CallToolResult, PreviewReplayOutput, error) {
		prepared, err := prepareReplay(
			ctx,
			reader,
			input.ProjectID,
			input.RequestID,
			input.ScopeID,
			input.Expected,
			input.Mutations,
			input.AllowMultipleMutations,
		)
		if err != nil {
			return nil, PreviewReplayOutput{}, err
		}
		previewToken, expiresAt, err := previews.issue(prepared, input.ScopeID)
		if err != nil {
			return nil, PreviewReplayOutput{}, err
		}
		return nil, PreviewReplayOutput{
			Project:          projectContext(prepared.Project),
			Source:           prepared.SourceView,
			Prepared:         prepared.PreparedView,
			Mutations:        input.Mutations,
			Scope:            prepared.Scope,
			PreviewToken:     previewToken,
			PreviewExpiresAt: expiresAt.Format(time.RFC3339Nano),
			WillSend:         false,
		}, nil
	})
}

// RegisterActiveReplay adds the only target-network tools. Callers must opt in
// explicitly; RegisterAll never registers them.
func RegisterActiveReplay(
	server *mcp.Server,
	reader caidoread.Reader,
	executor caidoreplay.Executor,
	previews *PreviewStore,
) {
	registerReplayRequest(server, reader, executor, previews)
	registerTestHypothesis(server, reader, executor, previews)
}

func registerReplayRequest(
	server *mcp.Server,
	reader caidoread.Reader,
	executor caidoreplay.Executor,
	previews *PreviewStore,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_replay_request",
		Title:        "Execute one controlled Caido Replay",
		Description:  "ACTIVE: validate one selected History row, apply only explicit mutations, enforce project/identity/scope/same-host guards, and send exactly one request through Caido Replay. Requires server-side Replay enablement and confirmExecution=true.",
		InputSchema:  schemaFor[ReplayRequestInput](),
		OutputSchema: schemaFor[ReplayRequestOutput](),
		Annotations:  activeReplayAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input ReplayRequestInput,
	) (*mcp.CallToolResult, ReplayRequestOutput, error) {
		if !input.ConfirmExecution {
			return nil, ReplayRequestOutput{}, fmt.Errorf("confirmExecution must be true; call caido_preview_replay first")
		}
		if strings.TrimSpace(input.Expected.Fingerprint) == "" {
			return nil, ReplayRequestOutput{}, fmt.Errorf("expected.fingerprint from caido_preview_replay is required")
		}
		prepared, err := prepareReplay(
			ctx,
			reader,
			input.ProjectID,
			input.RequestID,
			input.ScopeID,
			confirmedIdentity(input.Expected),
			input.Mutations,
			input.AllowMultipleMutations,
		)
		if err != nil {
			return nil, ReplayRequestOutput{}, err
		}
		if prepared.PreparedView.PotentiallyStateChanging && !input.AllowStateChanging {
			return nil, ReplayRequestOutput{}, fmt.Errorf("prepared method %s is potentially state-changing; allowStateChanging must be true", prepared.Prepared.Method)
		}
		if err := requireActiveProject(ctx, reader, prepared.Project.ID); err != nil {
			return nil, ReplayRequestOutput{}, err
		}
		if err := previews.consume(input.PreviewToken, prepared, input.ScopeID); err != nil {
			return nil, ReplayRequestOutput{}, err
		}
		result, err := executor.Start(ctx, prepared.Connection, prepared.PreparedRaw)
		if err != nil {
			return nil, ReplayRequestOutput{}, fmt.Errorf("execute Caido Replay: %w", err)
		}
		if err := requireActiveProject(ctx, reader, prepared.Project.ID); err != nil {
			return nil, ReplayRequestOutput{}, fmt.Errorf("Replay completed but project validation failed: %w", err)
		}
		evidence, _, err := liveEvidence(prepared, prepared.PreparedView, result)
		if err != nil {
			return nil, ReplayRequestOutput{}, err
		}
		return nil, ReplayRequestOutput{
			Project:         projectContext(prepared.Project),
			Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
			SourceRequestID: prepared.Record.ID,
			Mutations:       input.Mutations,
			Test:            evidence,
		}, nil
	})
}

func registerTestHypothesis(
	server *mcp.Server,
	reader caidoread.Reader,
	executor caidoreplay.Executor,
	previews *PreviewStore,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_test_hypothesis",
		Title:        "Test one HTTP hypothesis with control and evidence",
		Description:  "ACTIVE: execute one hypothesis, one primary mutation, and one objective comparison. A historical baseline requires one test send; live_replay performs one control send followed by one test send. There is no cumulative hunt or turn request budget. Returns facts without a vulnerability verdict.",
		InputSchema:  schemaFor[TestHypothesisInput](),
		OutputSchema: schemaFor[TestHypothesisOutput](),
		Annotations:  activeReplayAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input TestHypothesisInput,
	) (*mcp.CallToolResult, TestHypothesisOutput, error) {
		input.Hypothesis = strings.TrimSpace(input.Hypothesis)
		if input.Hypothesis == "" {
			return nil, TestHypothesisOutput{}, fmt.Errorf("hypothesis is required")
		}
		if len(input.Hypothesis) > maxHypothesisLength {
			return nil, TestHypothesisOutput{}, fmt.Errorf("hypothesis exceeds %d characters", maxHypothesisLength)
		}
		if !input.ConfirmExecution {
			return nil, TestHypothesisOutput{}, fmt.Errorf("confirmExecution must be true; call caido_preview_replay first")
		}
		if strings.TrimSpace(input.Expected.Fingerprint) == "" {
			return nil, TestHypothesisOutput{}, fmt.Errorf("expected.fingerprint from caido_preview_replay is required")
		}
		prepared, err := prepareReplay(
			ctx,
			reader,
			input.ProjectID,
			input.RequestID,
			input.ScopeID,
			confirmedIdentity(input.Expected),
			[]ReplayMutationInput{input.Mutation},
			false,
		)
		if err != nil {
			return nil, TestHypothesisOutput{}, err
		}
		if prepared.PreparedView.PotentiallyStateChanging && !input.AllowStateChanging {
			return nil, TestHypothesisOutput{}, fmt.Errorf("test method %s is potentially state-changing; allowStateChanging must be true", prepared.Prepared.Method)
		}

		baselineSource, err := normalizeBaselineSource(
			input.BaselineSource,
			potentiallyStateChanging(prepared.Source.Method),
		)
		if err != nil {
			return nil, TestHypothesisOutput{}, err
		}
		if baselineSource == "live_replay" &&
			potentiallyStateChanging(prepared.Source.Method) &&
			!input.AllowStateChangingBaseline {
			return nil, TestHypothesisOutput{}, fmt.Errorf("live baseline method %s is potentially state-changing; allowStateChangingBaseline must be true", prepared.Source.Method)
		}

		if err := requireActiveProject(ctx, reader, prepared.Project.ID); err != nil {
			return nil, TestHypothesisOutput{}, err
		}
		if err := previews.consume(input.PreviewToken, prepared, input.ScopeID); err != nil {
			return nil, TestHypothesisOutput{}, err
		}
		var control ReplayEvidence
		var baselineResponse *replaycore.Response
		var testResult caidoreplay.Result

		if baselineSource == "live_replay" {
			controlResult, err := executor.Start(ctx, prepared.Connection, prepared.SourceRaw)
			if err != nil {
				return nil, TestHypothesisOutput{}, fmt.Errorf("execute live baseline Replay: %w", err)
			}
			if err := requireActiveProject(ctx, reader, prepared.Project.ID); err != nil {
				return nil, TestHypothesisOutput{}, fmt.Errorf("baseline completed but project validation failed: %w", err)
			}
			control, baselineResponse, err = liveEvidence(prepared, prepared.SourceView, controlResult)
			if err != nil {
				return nil, TestHypothesisOutput{}, err
			}
			testResult, err = executor.Continue(
				ctx,
				controlResult.SessionID,
				controlResult.EntryID,
				prepared.Connection,
				prepared.PreparedRaw,
			)
			if err != nil {
				return nil, TestHypothesisOutput{}, fmt.Errorf("execute test Replay: %w", err)
			}
		} else {
			control, baselineResponse, err = historicalEvidence(prepared)
			if err != nil {
				return nil, TestHypothesisOutput{}, err
			}
			testResult, err = executor.Start(ctx, prepared.Connection, prepared.PreparedRaw)
			if err != nil {
				return nil, TestHypothesisOutput{}, fmt.Errorf("execute test Replay: %w", err)
			}
		}
		if err := requireActiveProject(ctx, reader, prepared.Project.ID); err != nil {
			return nil, TestHypothesisOutput{}, fmt.Errorf("test completed but project validation failed: %w", err)
		}
		testEvidence, testResponse, err := liveEvidence(prepared, prepared.PreparedView, testResult)
		if err != nil {
			return nil, TestHypothesisOutput{}, err
		}
		diff := responseDiff(replaycore.Compare(baselineResponse, testResponse))
		return nil, TestHypothesisOutput{
			Project:         projectContext(prepared.Project),
			Timestamp:       time.Now().UTC().Format(time.RFC3339Nano),
			Hypothesis:      input.Hypothesis,
			SourceRequestID: prepared.Record.ID,
			Mutation:        input.Mutation,
			BaselineSource:  baselineSource,
			Control:         control,
			Test:            testEvidence,
			Diff:            diff,
		}, nil
	})
}

func confirmedIdentity(input ConfirmedRequestIdentity) ExpectedRequestIdentity {
	return ExpectedRequestIdentity{
		Method:      input.Method,
		Host:        input.Host,
		Path:        input.Path,
		Fingerprint: input.Fingerprint,
	}
}

func prepareReplay(
	ctx context.Context,
	reader caidoread.Reader,
	projectID string,
	requestID string,
	scopeID string,
	expected ExpectedRequestIdentity,
	mutations []ReplayMutationInput,
	allowMultiple bool,
) (preparedReplay, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return preparedReplay{}, fmt.Errorf("requestId is required")
	}
	if len(mutations) > 1 && !allowMultiple {
		return preparedReplay{}, fmt.Errorf("one-mutation rule: allowMultipleMutations must be true for %d mutations", len(mutations))
	}

	data, project, err := guardedProjectRead(
		ctx,
		reader,
		projectID,
		func() (replaySourceData, error) {
			record, err := reader.GetRequest(ctx, requestID)
			if err != nil {
				return replaySourceData{}, err
			}
			scopes, err := reader.ListScopes(ctx)
			if err != nil {
				return replaySourceData{}, err
			}
			return replaySourceData{Record: record, Scopes: scopes}, nil
		},
	)
	if err != nil {
		if errors.Is(err, caidoread.ErrRequestNotFound) {
			return preparedReplay{}, fmt.Errorf("request %q not found", requestID)
		}
		return preparedReplay{}, fmt.Errorf("prepare Replay: %w", err)
	}
	if data.Record.ID != requestID {
		return preparedReplay{}, fmt.Errorf("ID inconsistency: Caido returned History row %q for requested row %q", data.Record.ID, requestID)
	}

	sourceRaw, err := decodeCapturedRequest(data.Record.Raw)
	if err != nil {
		return preparedReplay{}, err
	}
	source, err := replaycore.ParseRequest(sourceRaw)
	if err != nil {
		return preparedReplay{}, fmt.Errorf("parse selected History request: %w", err)
	}
	if err := validateCapturedIdentity(data.Record, source); err != nil {
		return preparedReplay{}, err
	}
	fingerprint := sourceFingerprint(data.Record, sourceRaw)
	if err := validateExpectedIdentity(data.Record, expected, fingerprint); err != nil {
		return preparedReplay{}, err
	}

	selectedScope, err := findScope(data.Scopes, scopeID)
	if err != nil {
		return preparedReplay{}, err
	}
	scopeResult := evaluateScope(normalizeHost(data.Record.Host), selectedScope)
	if !scopeResult.InScope {
		return preparedReplay{}, fmt.Errorf("host outside Caido scope: %s", scopeResult.Reason)
	}
	prepared := source.Clone()
	for index, input := range mutations {
		prepared, err = replaycore.Apply(prepared, coreMutation(input))
		if err != nil {
			return preparedReplay{}, fmt.Errorf("apply mutation %d (%s): %w", index+1, input.Type, err)
		}
	}
	if err := validateSameOrigin(data.Record, prepared); err != nil {
		return preparedReplay{}, err
	}
	preparedRaw := prepared.Raw()
	connection := caidoreplay.Connection{
		Host: data.Record.Host,
		Port: data.Record.Port,
		TLS:  data.Record.TLS,
	}
	scope := ScopeDecision{
		InScope:      true,
		MatchedScope: scopeResult.MatchedScope,
		MatchedRule:  scopeResult.MatchedRule,
		Reason:       scopeResult.Reason,
	}
	return preparedReplay{
		Project:      project,
		Record:       data.Record,
		Connection:   connection,
		Source:       source,
		SourceRaw:    sourceRaw,
		Prepared:     prepared,
		PreparedRaw:  preparedRaw,
		SourceView:   requestView(data.Record, source, sourceRaw, fingerprint),
		PreparedView: requestView(data.Record, prepared, preparedRaw, fingerprint),
		Scope:        scope,
	}, nil
}

func decodeCapturedRequest(encoded string) ([]byte, error) {
	if len(encoded) > base64.StdEncoding.EncodedLen(replaycore.MaxRequestBytes) {
		return nil, fmt.Errorf("captured request exceeds Replay size limit")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode selected History request: %w", err)
	}
	return raw, nil
}

func validateCapturedIdentity(record caidoread.RequestDetails, request *replaycore.Request) error {
	if !strings.EqualFold(request.Method, record.Method) {
		return fmt.Errorf("ID inconsistency: raw method %q does not match History metadata %q", request.Method, record.Method)
	}
	path, query, err := request.PathAndQuery()
	if err != nil {
		return fmt.Errorf("validate selected request target: %w", err)
	}
	if path != record.Path || query != record.Query {
		return fmt.Errorf("ID inconsistency: raw target %q does not match History path/query %q", request.Target, historyTarget(record))
	}
	return validateSameOrigin(record, request)
}

func validateSameOrigin(record caidoread.RequestDetails, request *replaycore.Request) error {
	values := request.HeaderValues("Host")
	if len(values) != 1 {
		return fmt.Errorf("selected request must contain exactly one Host header")
	}
	host, port, err := parseHostHeader(values[0], record.TLS)
	if err != nil {
		return err
	}
	if normalizeHost(host) != normalizeHost(record.Host) || port != record.Port {
		return fmt.Errorf("host/origin change blocked: request Host %q does not match History connection %s:%d", values[0], record.Host, record.Port)
	}
	return nil
}

func validateExpectedIdentity(
	record caidoread.RequestDetails,
	expected ExpectedRequestIdentity,
	fingerprint string,
) error {
	expected.Method = strings.TrimSpace(expected.Method)
	expected.Host = strings.TrimSpace(expected.Host)
	expected.Path = strings.TrimSpace(expected.Path)
	if expected.Method == "" || expected.Host == "" || expected.Path == "" {
		return fmt.Errorf("expected.method, expected.host, and expected.path are required")
	}
	if !strings.EqualFold(expected.Method, record.Method) ||
		normalizeHost(expected.Host) != normalizeHost(record.Host) ||
		expected.Path != record.Path {
		return fmt.Errorf("ID inconsistency: History row %s is %s %s%s, not expected %s %s%s", record.ID, record.Method, record.Host, record.Path, expected.Method, expected.Host, expected.Path)
	}
	if expected.Fingerprint != "" && expected.Fingerprint != fingerprint {
		return fmt.Errorf("request fingerprint mismatch: selected History row changed after preview")
	}
	return nil
}

func parseHostHeader(value string, tls bool) (string, int, error) {
	if strings.ContainsAny(value, "\r\n/@") {
		return "", 0, fmt.Errorf("Host header is malformed")
	}
	u, err := url.Parse("http://" + value)
	if err != nil || u.Hostname() == "" || u.Path != "" {
		return "", 0, fmt.Errorf("Host header is malformed")
	}
	port := 80
	if tls {
		port = 443
	}
	if u.Port() != "" {
		port, err = strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", 0, fmt.Errorf("Host header contains an invalid port")
		}
	}
	return u.Hostname(), port, nil
}

func normalizeHost(host string) string {
	return strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
}

func historyTarget(record caidoread.RequestDetails) string {
	target := record.Path
	if record.Query != "" {
		target += "?" + record.Query
	}
	return target
}

func sourceFingerprint(record caidoread.RequestDetails, raw []byte) string {
	prefix := fmt.Sprintf("%t\n%s\n%d\n", record.TLS, normalizeHost(record.Host), record.Port)
	return replaycore.SHA256(append([]byte(prefix), raw...))
}

func coreMutation(input ReplayMutationInput) replaycore.Mutation {
	return replaycore.Mutation{
		Type:   input.Type,
		Target: input.Target,
		From:   input.From,
		To:     input.To,
		Format: input.Format,
	}
}

func requestView(
	record caidoread.RequestDetails,
	request *replaycore.Request,
	raw []byte,
	sourceFingerprintValue string,
) RequestView {
	path, query, _ := request.PathAndQuery()
	headers := replaycore.RedactedHeaders(request.Headers)
	headerViews := make([]HeaderView, 0, len(headers))
	sensitiveHeadersRedacted := false
	for index, header := range headers {
		headerViews = append(headerViews, HeaderView{Name: header.Name, Value: header.Value})
		if header.Value != request.Headers[index].Value {
			sensitiveHeadersRedacted = true
		}
	}
	authenticationHeaderNames := request.AuthenticationHeaderNames()
	cookieNames := request.CookieNames()
	authenticationMaterialPresent := sensitiveHeadersRedacted
	preview := request.RedactedRaw()
	truncated := false
	if len(preview) > maxPreviewBytes {
		preview = preview[:maxPreviewBytes] + "\n[TRUNCATED]"
		truncated = true
	}
	scheme := "http"
	if record.TLS {
		scheme = "https"
	}
	var originalStatus *int
	var originalSize *int
	if record.Response != nil {
		status := record.Response.StatusCode
		size := record.Response.Length
		originalStatus = &status
		originalSize = &size
	}
	return RequestView{
		SourceRequestID:            record.ID,
		Method:                     request.Method,
		Scheme:                     scheme,
		Host:                       record.Host,
		Port:                       record.Port,
		Path:                       path,
		Query:                      query,
		Headers:                    headerViews,
		Cookies:                    cookieNames,
		BodySize:                   len(request.Body),
		ContentType:                request.HeaderValue("Content-Type"),
		OriginalResponseStatus:     originalStatus,
		OriginalResponseSize:       originalSize,
		SourceFingerprint:          sourceFingerprintValue,
		PreparedRequestFingerprint: replaycore.SHA256(raw),
		ExpectedIdentityValidated:  true,
		PotentiallyStateChanging:   potentiallyStateChanging(request.Method),
		Authentication: AuthenticationContext{
			HeaderNames:     authenticationHeaderNames,
			CookieNames:     cookieNames,
			MaterialPresent: authenticationMaterialPresent,
			ValuesRedacted:  authenticationMaterialPresent,
		},
		RawPreview:               preview,
		PreviewTruncated:         truncated,
		SensitiveHeadersRedacted: sensitiveHeadersRedacted,
		BodyRedactionApplied:     false,
	}
}

func potentiallyStateChanging(method string) bool {
	switch strings.ToUpper(method) {
	case "GET", "HEAD", "OPTIONS":
		return false
	default:
		return true
	}
}

func normalizeBaselineSource(raw string, sourceStateChanging bool) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "auto":
		if sourceStateChanging {
			return "historical", nil
		}
		return "live_replay", nil
	case "historical":
		return "historical", nil
	case "live_replay":
		return "live_replay", nil
	default:
		return "", fmt.Errorf("baselineSource must be auto, historical, or live_replay")
	}
}

func requireActiveProject(ctx context.Context, reader caidoread.Reader, expectedID string) error {
	project, err := currentProject(ctx, reader)
	if err != nil {
		return err
	}
	if project.ID != expectedID {
		return fmt.Errorf("project mismatch before/after active Replay: expected %q, current %q; operation blocked", expectedID, project.ID)
	}
	return nil
}

func liveEvidence(
	prepared preparedReplay,
	view RequestView,
	result caidoreplay.Result,
) (ReplayEvidence, *replaycore.Response, error) {
	response, err := replaycore.ParseResponse(result.ResponseRaw)
	if err != nil {
		return ReplayEvidence{}, nil, fmt.Errorf("parse Replay response: %w", err)
	}
	if response.StatusCode != result.StatusCode {
		return ReplayEvidence{}, nil, fmt.Errorf("Caido Replay response status metadata does not match raw response")
	}
	actualRequest, err := replaycore.ParseRequest(result.RequestRaw)
	if err != nil {
		return ReplayEvidence{}, nil, fmt.Errorf("parse Replay request evidence: %w", err)
	}
	if err := validateSameOrigin(prepared.Record, actualRequest); err != nil {
		return ReplayEvidence{}, nil, fmt.Errorf("Caido Replay request evidence failed origin validation: %w", err)
	}
	if actualFingerprint := replaycore.SHA256(result.RequestRaw); actualFingerprint != view.PreparedRequestFingerprint {
		return ReplayEvidence{}, nil, fmt.Errorf("Caido Replay request evidence fingerprint does not match the validated preview")
	}
	actualView := requestView(
		prepared.Record,
		actualRequest,
		result.RequestRaw,
		view.SourceFingerprint,
	)
	return ReplayEvidence{
		Request: actualView,
		Response: responseEvidence(
			"live_replay",
			response,
			result.Length,
			result.RoundtripMS,
			result.SessionID,
			result.EntryID,
		),
	}, response, nil
}

func historicalEvidence(
	prepared preparedReplay,
) (ReplayEvidence, *replaycore.Response, error) {
	if prepared.Record.Response == nil {
		return ReplayEvidence{}, nil, fmt.Errorf("selected History row has no response for a historical baseline")
	}
	encoded := prepared.Record.Response.Raw
	if len(encoded) > base64.StdEncoding.EncodedLen(8*1024*1024) {
		return ReplayEvidence{}, nil, fmt.Errorf("historical response exceeds evidence size limit")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ReplayEvidence{}, nil, fmt.Errorf("decode historical response: %w", err)
	}
	response, err := replaycore.ParseResponse(raw)
	if err != nil {
		return ReplayEvidence{}, nil, fmt.Errorf("parse historical response: %w", err)
	}
	if response.StatusCode != prepared.Record.Response.StatusCode {
		return ReplayEvidence{}, nil, fmt.Errorf("historical response status metadata does not match raw response")
	}
	return ReplayEvidence{
		Request: prepared.SourceView,
		Response: responseEvidence(
			"historical",
			response,
			prepared.Record.Response.Length,
			prepared.Record.Response.RoundtripMS,
			"",
			"",
		),
	}, response, nil
}

func responseEvidence(
	source string,
	response *replaycore.Response,
	recordedSize int,
	roundtripMS int,
	sessionID string,
	entryID string,
) ResponseEvidence {
	headers := replaycore.RedactedHeaders(response.Headers)
	headerViews := make([]HeaderView, 0, len(headers))
	for _, header := range headers {
		headerViews = append(headerViews, HeaderView{Name: header.Name, Value: header.Value})
	}
	body := response.Body
	truncated := false
	if len(body) > maxPreviewBytes {
		body = body[:maxPreviewBytes]
		truncated = true
	}
	bodyText := ""
	encoding := ""
	if len(body) != 0 {
		if utf8.Valid(body) {
			bodyText = string(body)
			encoding = "utf-8"
		} else {
			bodyText = base64.StdEncoding.EncodeToString(body)
			encoding = "base64"
		}
	}
	return ResponseEvidence{
		Source:          source,
		Status:          response.StatusCode,
		BodySize:        len(response.Body),
		RecordedSize:    recordedSize,
		RoundtripMS:     roundtripMS,
		ContentType:     response.HeaderValue("Content-Type"),
		Redirect:        response.HeaderValue("Location"),
		Headers:         headerViews,
		Body:            bodyText,
		BodyEncoding:    encoding,
		BodyTruncated:   truncated,
		BodySHA256:      replaycore.SHA256(response.Body),
		ReplaySessionID: sessionID,
		ReplayEntryID:   entryID,
	}
}

func responseDiff(diff replaycore.Diff) ResponseDiff {
	headers := make([]HeaderDifference, 0, len(diff.RelevantHeaderChanges))
	for _, change := range diff.RelevantHeaderChanges {
		headers = append(headers, HeaderDifference{
			Name:     change.Name,
			Baseline: change.Baseline,
			Test:     change.Test,
		})
	}
	return ResponseDiff{
		StatusChanged:         diff.StatusChanged,
		BaselineStatus:        diff.BaselineStatus,
		TestStatus:            diff.TestStatus,
		SizeDelta:             diff.SizeDelta,
		ContentTypeChanged:    diff.ContentTypeChanged,
		BaselineContentType:   diff.BaselineContentType,
		TestContentType:       diff.TestContentType,
		RedirectChanged:       diff.RedirectChanged,
		BaselineRedirect:      diff.BaselineRedirect,
		TestRedirect:          diff.TestRedirect,
		RelevantHeaderChanges: headers,
		BodyEqual:             diff.BodyEqual,
		BaselineBodySHA256:    diff.BaselineBodySHA256,
		TestBodySHA256:        diff.TestBodySHA256,
		JSONCompared:          diff.JSONCompared,
		JSONStructureChanged:  diff.JSONStructureChanged,
		ChangedJSONFields:     diff.ChangedJSONFields,
		AddedJSONFields:       diff.AddedJSONFields,
		RemovedJSONFields:     diff.RemovedJSONFields,
	}
}
