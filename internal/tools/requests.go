package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
	"github.com/ph-xyz/Caido-Bridge/internal/httputil"
)

const (
	defaultRequestLimit = 20
	maxRequestLimit     = 100
	maxHTTPQLLength     = 10_000
	maxCursorLength     = 4_096
	maxRequestIDLength  = 256
)

type ListRequestsInput struct {
	ProjectID string `json:"projectId" jsonschema:"Required current project ID from caido_get_current_project"`
	HTTPQL    string `json:"httpql,omitempty" jsonschema:"Optional HTTPQL filter applied by Caido"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum results; default 20, maximum 100"`
	After     string `json:"after,omitempty" jsonschema:"Opaque nextCursor from a previous result"`
}

type RequestSummary struct {
	ID         string `json:"id"`
	Method     string `json:"method"`
	URL        string `json:"url"`
	StatusCode *int   `json:"statusCode,omitempty"`
}

type ListRequestsOutput struct {
	Project    ProjectContext   `json:"project"`
	Requests   []RequestSummary `json:"requests"`
	HasMore    bool             `json:"hasMore"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type GetRequestInput struct {
	ProjectID  string   `json:"projectId" jsonschema:"Required current project ID from caido_get_current_project"`
	ID         string   `json:"id" jsonschema:"Decimal ID shown in Caido's HTTP History ID column"`
	Include    []string `json:"include,omitempty" jsonschema:"Optional fields: requestHeaders, requestBody, responseHeaders, responseBody. Omit for metadata only"`
	BodyOffset int      `json:"bodyOffset,omitempty" jsonschema:"Body byte offset for chunked reading"`
	BodyLimit  int      `json:"bodyLimit,omitempty" jsonschema:"Maximum bytes returned per requested body; default 2048, maximum 65536"`
}

type GetRequestOutput struct {
	Project     ProjectContext          `json:"project"`
	ID          string                  `json:"id"`
	Method      string                  `json:"method"`
	URL         string                  `json:"url"`
	Host        string                  `json:"host"`
	Port        int                     `json:"port"`
	Path        string                  `json:"path"`
	Query       string                  `json:"query,omitempty"`
	TLS         bool                    `json:"tls"`
	Timestamp   string                  `json:"timestamp"`
	StatusCode  *int                    `json:"statusCode,omitempty"`
	RoundtripMS *int                    `json:"roundtripMs,omitempty"`
	Request     *httputil.ParsedMessage `json:"request,omitempty"`
	Response    *httputil.ParsedMessage `json:"response,omitempty"`
}

var allowedIncludes = map[string]struct{}{
	"requestHeaders":  {},
	"requestBody":     {},
	"responseHeaders": {},
	"responseBody":    {},
}

func normalizeListRequestsInput(input ListRequestsInput) (ListRequestsInput, error) {
	if len(input.HTTPQL) > maxHTTPQLLength {
		return input, fmt.Errorf("httpql exceeds %d characters", maxHTTPQLLength)
	}
	if len(input.After) > maxCursorLength {
		return input, fmt.Errorf("after cursor exceeds %d characters", maxCursorLength)
	}
	if input.Limit == 0 {
		input.Limit = defaultRequestLimit
	}
	if input.Limit < 1 || input.Limit > maxRequestLimit {
		return input, fmt.Errorf("limit must be between 1 and %d", maxRequestLimit)
	}
	return input, nil
}

func normalizeGetRequestInput(input GetRequestInput) (GetRequestInput, error) {
	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" {
		return input, fmt.Errorf("id is required")
	}
	if len(input.ID) > maxRequestIDLength {
		return input, fmt.Errorf("id exceeds %d characters", maxRequestIDLength)
	}
	for i := 0; i < len(input.ID); i++ {
		if input.ID[i] < '0' || input.ID[i] > '9' {
			return input, fmt.Errorf("id must be the decimal ID shown in Caido HTTP History")
		}
	}
	if input.BodyOffset < 0 {
		return input, fmt.Errorf("bodyOffset cannot be negative")
	}
	if input.BodyLimit == 0 {
		input.BodyLimit = httputil.DefaultBodyLimit
	}
	if input.BodyLimit < 1 || input.BodyLimit > httputil.MaxBodyLimit {
		return input, fmt.Errorf(
			"bodyLimit must be between 1 and %d bytes",
			httputil.MaxBodyLimit,
		)
	}
	seen := make(map[string]struct{}, len(input.Include))
	for _, field := range input.Include {
		if _, ok := allowedIncludes[field]; !ok {
			return input, fmt.Errorf("unsupported include field %q", field)
		}
		if _, duplicate := seen[field]; duplicate {
			return input, fmt.Errorf("duplicate include field %q", field)
		}
		seen[field] = struct{}{}
	}
	return input, nil
}

func hasInclude(include []string, field string) bool {
	for _, candidate := range include {
		if candidate == field {
			return true
		}
	}
	return false
}

func registerListRequests(server *mcp.Server, reader caidoread.Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_list_requests",
		Title:        "List captured Caido requests",
		Description:  "Read HTTP History only after projectId is a valid existing project and matches the MCP caller's current project. Returns project identity plus bounded request summaries.",
		InputSchema:  schemaFor[ListRequestsInput](),
		OutputSchema: schemaFor[ListRequestsOutput](),
		Annotations:  readOnlyAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input ListRequestsInput,
	) (*mcp.CallToolResult, ListRequestsOutput, error) {
		normalized, err := normalizeListRequestsInput(input)
		if err != nil {
			return nil, ListRequestsOutput{}, err
		}
		page, project, err := guardedProjectRead(
			ctx,
			reader,
			normalized.ProjectID,
			func() (caidoread.RequestPage, error) {
				return reader.ListRequests(ctx, caidoread.ListRequestsParams{
					HTTPQL: normalized.HTTPQL,
					Limit:  normalized.Limit,
					After:  normalized.After,
				})
			},
		)
		if err != nil {
			return nil, ListRequestsOutput{}, fmt.Errorf("list Caido requests: %w", err)
		}
		out := ListRequestsOutput{
			Project:    projectContext(project),
			Requests:   make([]RequestSummary, 0, len(page.Requests)),
			HasMore:    page.HasMore,
			NextCursor: page.NextCursor,
		}
		for _, item := range page.Requests {
			out.Requests = append(out.Requests, RequestSummary{
				ID:     item.ID,
				Method: item.Method,
				URL: httputil.BuildURL(
					item.TLS,
					item.Host,
					item.Port,
					item.Path,
					item.Query,
				),
				StatusCode: item.StatusCode,
			})
		}
		return nil, out, nil
	})
}

func registerGetRequest(server *mcp.Server, reader caidoread.Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_get_request",
		Title:        "Read a captured request and response",
		Description:  "Read one HTTP History item only after projectId is a valid existing project and matches the MCP caller's current project. Project identity is always returned; raw fields remain opt-in and sensitive headers remain redacted.",
		InputSchema:  schemaFor[GetRequestInput](),
		OutputSchema: schemaFor[GetRequestOutput](),
		Annotations:  readOnlyAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input GetRequestInput,
	) (*mcp.CallToolResult, GetRequestOutput, error) {
		normalized, err := normalizeGetRequestInput(input)
		if err != nil {
			return nil, GetRequestOutput{}, err
		}
		record, project, err := guardedProjectRead(
			ctx,
			reader,
			normalized.ProjectID,
			func() (caidoread.RequestDetails, error) {
				return reader.GetRequest(ctx, normalized.ID)
			},
		)
		if err != nil {
			if errors.Is(err, caidoread.ErrRequestNotFound) {
				return nil, GetRequestOutput{}, fmt.Errorf("request %q not found", normalized.ID)
			}
			return nil, GetRequestOutput{}, fmt.Errorf("get Caido request: %w", err)
		}

		out := GetRequestOutput{
			Project:   projectContext(project),
			ID:        record.ID,
			Method:    record.Method,
			URL:       httputil.BuildURL(record.TLS, record.Host, record.Port, record.Path, record.Query),
			Host:      record.Host,
			Port:      record.Port,
			Path:      record.Path,
			Query:     record.Query,
			TLS:       record.TLS,
			Timestamp: time.UnixMilli(record.CreatedAtMS).UTC().Format(time.RFC3339Nano),
		}

		requestHeaders := hasInclude(normalized.Include, "requestHeaders")
		requestBody := hasInclude(normalized.Include, "requestBody")
		if requestHeaders || requestBody {
			out.Request, err = httputil.ParseBase64(
				record.Raw,
				requestHeaders,
				requestBody,
				normalized.BodyOffset,
				normalized.BodyLimit,
			)
			if err != nil {
				return nil, GetRequestOutput{}, fmt.Errorf("parse captured request: %w", err)
			}
		}

		if record.Response != nil {
			status := record.Response.StatusCode
			roundtrip := record.Response.RoundtripMS
			out.StatusCode = &status
			out.RoundtripMS = &roundtrip

			responseHeaders := hasInclude(normalized.Include, "responseHeaders")
			responseBody := hasInclude(normalized.Include, "responseBody")
			if responseHeaders || responseBody {
				out.Response, err = httputil.ParseBase64(
					record.Response.Raw,
					responseHeaders,
					responseBody,
					normalized.BodyOffset,
					normalized.BodyLimit,
				)
				if err != nil {
					return nil, GetRequestOutput{}, fmt.Errorf("parse captured response: %w", err)
				}
			}
		}
		return nil, out, nil
	})
}
