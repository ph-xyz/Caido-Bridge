// Package caidoread is the narrow, read-only boundary between MCP tools and
// the broader Caido SDK. Tool code depends only on Reader and therefore cannot
// call Replay, mutation, Intercept, Automate, or other active SDK methods.
package caidoread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	gql "github.com/Khan/genqlient/graphql"
	gen "github.com/caido-community/sdk-go/graphql"
	"github.com/caido-community/sdk-go/graphql/scalars"
	"github.com/ph-xyz/Caido-Bridge/internal/config"
)

var (
	// ErrRequestNotFound is returned when Caido has no request for an ID.
	ErrRequestNotFound = errors.New("request not found")
	// ErrInvalidRequestID is returned when an ID is not a decimal HTTP History
	// row ID of the kind displayed by Caido's traffic tables.
	ErrInvalidRequestID = errors.New("request ID must be a decimal Caido HTTP History row ID")
	// ErrNoCurrentProject is returned when the API caller has no selected project.
	ErrNoCurrentProject = errors.New("no current Caido project")
)

const listRequestsWithRowIDQuery = `query PHListRequestsWithRowID(
  $first: Int
  $after: String
  $filter: HTTPQLInput
) {
  requests(first: $first, after: $after, filter: $filter) {
    edges {
      cursor
      node {
        id
        metadata { id }
        method
        host
        port
        path
        query
        isTls
        response { statusCode }
      }
    }
    pageInfo { hasNextPage endCursor }
  }
}`

const getRequestByRowIDQuery = `query PHGetRequestByRowID($filter: HTTPQLInput) {
  requests(first: 2, filter: $filter) {
    edges {
      node {
        id
        metadata { id }
        method
        host
        port
        path
        query
        isTls
        raw
        createdAt
        length
        response {
          statusCode
          raw
          roundtripTime
          length
          createdAt
        }
      }
    }
  }
}`

const listSitemapDescendantsWithRowIDQuery = `query PHListSitemapDescendantsWithRowID(
  $parentId: ID!
  $depth: SitemapDescendantsDepth!
) {
  sitemapDescendantEntries(parentId: $parentId, depth: $depth) {
    edges {
      node {
        id
        label
        kind
        parentId
        hasDescendants
        request {
          id
          metadata { id }
          method
          path
          response { statusCode }
        }
      }
    }
  }
}`

type ListRequestsParams struct {
	HTTPQL string
	Limit  int
	After  string
}

type RequestSummary struct {
	ID         string
	Method     string
	Host       string
	Port       int
	Path       string
	Query      string
	TLS        bool
	StatusCode *int
}

type RequestPage struct {
	Requests   []RequestSummary
	HasMore    bool
	NextCursor string
}

type ResponseDetails struct {
	StatusCode  int
	Raw         string
	RoundtripMS int
	Length      int
	CreatedAtMS int64
}

type RequestDetails struct {
	ID          string
	Method      string
	Host        string
	Port        int
	Path        string
	Query       string
	TLS         bool
	Raw         string
	Length      int
	CreatedAtMS int64
	Response    *ResponseDetails
}

type SitemapEntry struct {
	ID             string
	Label          string
	Kind           string
	HasDescendants bool
	RequestID      *string
	Method         *string
	StatusCode     *int
}

type Scope struct {
	ID        string
	Name      string
	Allowlist []string
	Denylist  []string
	Indexed   bool
}

type Runtime struct {
	Version  string
	Platform string
}

type Project struct {
	ID       string
	Name     string
	Status   string
	Version  string
	ReadOnly bool
}

// Reader is the complete read-only Caido data surface available to MCP tools.
// It intentionally exposes no mutation or project-selection operation.
type Reader interface {
	CurrentProject(context.Context) (Project, error)
	ListProjects(context.Context) ([]Project, error)
	ListRequests(context.Context, ListRequestsParams) (RequestPage, error)
	GetRequest(context.Context, string) (RequestDetails, error)
	ListSitemap(context.Context, string) ([]SitemapEntry, error)
	ListScopes(context.Context) ([]Scope, error)
}

// Client adapts the community SDK while exposing only read operations here.
type Client struct {
	baseURL string
	http    *http.Client
	graphql gql.Client
}

func New(caidoURL, accessToken string) (*Client, error) {
	validatedURL, err := config.ValidateLocalURL(caidoURL)
	if err != nil {
		return nil, fmt.Errorf("validate local Caido URL: %w", err)
	}
	origin, err := url.Parse(validatedURL)
	if err != nil {
		return nil, fmt.Errorf("parse local Caido URL: %w", err)
	}
	transport := &sameOriginAuthTransport{
		base:   http.DefaultTransport,
		origin: origin,
		token:  accessToken,
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &Client{
		baseURL: validatedURL,
		http:    httpClient,
		graphql: gql.NewClient(validatedURL+"/graphql", httpClient),
	}, nil
}

// Connect performs Caido's local health/readiness check.
func (c *Client) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("health request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned HTTP %d", resp.StatusCode)
	}
	var health struct {
		Ready bool `json:"ready"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&health); err != nil {
		return fmt.Errorf("decode health response: %w", err)
	}
	if !health.Ready {
		return fmt.Errorf("Caido is not ready")
	}
	return nil
}

func (c *Client) Runtime(ctx context.Context) (Runtime, error) {
	data := &struct {
		Runtime struct {
			Version  string `json:"version"`
			Platform string `json:"platform"`
		} `json:"runtime"`
	}{}
	response := &gql.Response{Data: data}
	err := c.graphql.MakeRequest(ctx, &gql.Request{
		OpName: "PHCaidoRuntime",
		Query:  "query PHCaidoRuntime { runtime { version platform } }",
	}, response)
	if err != nil {
		return Runtime{}, err
	}
	return Runtime{
		Version:  data.Runtime.Version,
		Platform: data.Runtime.Platform,
	}, nil
}

func (c *Client) CurrentProject(ctx context.Context) (Project, error) {
	resp, err := gen.GetCurrentProject(ctx, c.graphql)
	if err != nil {
		return Project{}, err
	}
	if resp.CurrentProject == nil {
		return Project{}, ErrNoCurrentProject
	}
	p := resp.CurrentProject.Project
	return Project{
		ID:       p.Id,
		Name:     p.Name,
		Status:   string(p.Status),
		Version:  p.Version,
		ReadOnly: resp.CurrentProject.ReadOnly,
	}, nil
}

func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	resp, err := gen.ListProjects(ctx, c.graphql)
	if err != nil {
		return nil, err
	}
	projects := make([]Project, 0, len(resp.Projects))
	for _, p := range resp.Projects {
		projects = append(projects, Project{
			ID:      p.Id,
			Name:    p.Name,
			Status:  string(p.Status),
			Version: p.Version,
		})
	}
	return projects, nil
}

func (c *Client) ListRequests(
	ctx context.Context,
	params ListRequestsParams,
) (RequestPage, error) {
	limit := params.Limit
	var filter *scalars.HTTPQLInput
	if params.HTTPQL != "" {
		filter = &scalars.HTTPQLInput{Code: params.HTTPQL}
	}
	var after *string
	if params.After != "" {
		after = &params.After
	}

	data := &struct {
		Requests struct {
			Edges []struct {
				Cursor string `json:"cursor"`
				Node   struct {
					ID       string `json:"id"`
					Metadata struct {
						ID string `json:"id"`
					} `json:"metadata"`
					Method   string `json:"method"`
					Host     string `json:"host"`
					Port     int    `json:"port"`
					Path     string `json:"path"`
					Query    string `json:"query"`
					IsTLS    bool   `json:"isTls"`
					Response *struct {
						StatusCode int `json:"statusCode"`
					} `json:"response"`
				} `json:"node"`
			} `json:"edges"`
			PageInfo struct {
				HasNextPage bool    `json:"hasNextPage"`
				EndCursor   *string `json:"endCursor"`
			} `json:"pageInfo"`
		} `json:"requests"`
	}{}
	resp := &gql.Response{Data: data}
	err := c.graphql.MakeRequest(ctx, &gql.Request{
		OpName: "PHListRequestsWithRowID",
		Query:  listRequestsWithRowIDQuery,
		Variables: map[string]any{
			"first":  limit,
			"after":  after,
			"filter": filter,
		},
	}, resp)
	if err != nil {
		return RequestPage{}, err
	}

	page := RequestPage{
		Requests: make([]RequestSummary, 0, len(data.Requests.Edges)),
		HasMore:  data.Requests.PageInfo.HasNextPage,
	}
	if data.Requests.PageInfo.EndCursor != nil {
		page.NextCursor = *data.Requests.PageInfo.EndCursor
	}
	for _, edge := range data.Requests.Edges {
		r := edge.Node
		rowID, err := requestRowID(r.ID, r.Metadata.ID)
		if err != nil {
			return RequestPage{}, err
		}
		item := RequestSummary{
			ID:     rowID,
			Method: r.Method,
			Host:   r.Host,
			Port:   r.Port,
			Path:   r.Path,
			Query:  r.Query,
			TLS:    r.IsTLS,
		}
		if r.Response != nil {
			status := r.Response.StatusCode
			item.StatusCode = &status
		}
		page.Requests = append(page.Requests, item)
	}
	return page, nil
}

func (c *Client) GetRequest(
	ctx context.Context,
	id string,
) (RequestDetails, error) {
	if !isDecimalRowID(id) {
		return RequestDetails{}, ErrInvalidRequestID
	}

	data := &struct {
		Requests struct {
			Edges []struct {
				Node struct {
					ID       string `json:"id"`
					Metadata struct {
						ID string `json:"id"`
					} `json:"metadata"`
					Method    string `json:"method"`
					Host      string `json:"host"`
					Port      int    `json:"port"`
					Path      string `json:"path"`
					Query     string `json:"query"`
					IsTLS     bool   `json:"isTls"`
					Raw       string `json:"raw"`
					CreatedAt int64  `json:"createdAt"`
					Length    int    `json:"length"`
					Response  *struct {
						StatusCode    int    `json:"statusCode"`
						Raw           string `json:"raw"`
						RoundtripTime int    `json:"roundtripTime"`
						Length        int    `json:"length"`
						CreatedAt     int64  `json:"createdAt"`
					} `json:"response"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"requests"`
	}{}
	resp := &gql.Response{Data: data}
	err := c.graphql.MakeRequest(ctx, &gql.Request{
		OpName: "PHGetRequestByRowID",
		Query:  getRequestByRowIDQuery,
		Variables: map[string]any{
			"filter": &scalars.HTTPQLInput{Code: "row.id.eq:" + id},
		},
	}, resp)
	if err != nil {
		return RequestDetails{}, err
	}
	if len(data.Requests.Edges) == 0 {
		return RequestDetails{}, ErrRequestNotFound
	}
	if len(data.Requests.Edges) != 1 {
		return RequestDetails{}, fmt.Errorf("Caido returned multiple HTTP History rows for row ID %q", id)
	}

	r := data.Requests.Edges[0].Node
	rowID, err := requestRowID(r.ID, r.Metadata.ID)
	if err != nil {
		return RequestDetails{}, err
	}
	if rowID != id {
		return RequestDetails{}, fmt.Errorf(
			"Caido returned HTTP History row ID %q while %q was requested",
			rowID,
			id,
		)
	}
	out := RequestDetails{
		ID:          rowID,
		Method:      r.Method,
		Host:        r.Host,
		Port:        r.Port,
		Path:        r.Path,
		Query:       r.Query,
		TLS:         r.IsTLS,
		Raw:         r.Raw,
		Length:      r.Length,
		CreatedAtMS: r.CreatedAt,
	}
	if r.Response != nil {
		out.Response = &ResponseDetails{
			StatusCode:  r.Response.StatusCode,
			Raw:         r.Response.Raw,
			RoundtripMS: r.Response.RoundtripTime,
			Length:      r.Response.Length,
			CreatedAtMS: r.Response.CreatedAt,
		}
	}
	return out, nil
}

func (c *Client) ListSitemap(
	ctx context.Context,
	parentID string,
) ([]SitemapEntry, error) {
	if parentID == "" {
		resp, err := gen.ListSitemapRootEntries(ctx, c.graphql, nil)
		if err != nil {
			return nil, err
		}
		entries := make([]SitemapEntry, 0, len(resp.SitemapRootEntries.Edges))
		for _, edge := range resp.SitemapRootEntries.Edges {
			e := edge.Node
			entries = append(entries, SitemapEntry{
				ID:             e.Id,
				Label:          e.Label,
				Kind:           string(e.Kind),
				HasDescendants: e.HasDescendants,
			})
		}
		return entries, nil
	}

	data := &struct {
		SitemapDescendantEntries struct {
			Edges []struct {
				Node struct {
					ID             string `json:"id"`
					Label          string `json:"label"`
					Kind           string `json:"kind"`
					HasDescendants bool   `json:"hasDescendants"`
					Request        *struct {
						ID       string `json:"id"`
						Metadata struct {
							ID string `json:"id"`
						} `json:"metadata"`
						Method   string `json:"method"`
						Response *struct {
							StatusCode int `json:"statusCode"`
						} `json:"response"`
					} `json:"request"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"sitemapDescendantEntries"`
	}{}
	resp := &gql.Response{Data: data}
	err := c.graphql.MakeRequest(ctx, &gql.Request{
		OpName: "PHListSitemapDescendantsWithRowID",
		Query:  listSitemapDescendantsWithRowIDQuery,
		Variables: map[string]any{
			"parentId": parentID,
			"depth":    gen.SitemapDescendantsDepthDirect,
		},
	}, resp)
	if err != nil {
		return nil, err
	}
	entries := make([]SitemapEntry, 0, len(data.SitemapDescendantEntries.Edges))
	for _, edge := range data.SitemapDescendantEntries.Edges {
		e := edge.Node
		item := SitemapEntry{
			ID:             e.ID,
			Label:          e.Label,
			Kind:           e.Kind,
			HasDescendants: e.HasDescendants,
		}
		if e.Request != nil {
			requestID, err := requestRowID(e.Request.ID, e.Request.Metadata.ID)
			if err != nil {
				return nil, err
			}
			method := e.Request.Method
			item.RequestID = &requestID
			item.Method = &method
			if e.Request.Response != nil {
				status := e.Request.Response.StatusCode
				item.StatusCode = &status
			}
		}
		entries = append(entries, item)
	}
	return entries, nil
}

func requestRowID(internalID, rowID string) (string, error) {
	if !isDecimalRowID(rowID) {
		return "", fmt.Errorf(
			"Caido request %q has invalid HTTP History row ID %q",
			internalID,
			rowID,
		)
	}
	return rowID, nil
}

func isDecimalRowID(id string) bool {
	if id == "" {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

func (c *Client) ListScopes(ctx context.Context) ([]Scope, error) {
	resp, err := gen.ListScopes(ctx, c.graphql)
	if err != nil {
		return nil, err
	}
	scopes := make([]Scope, 0, len(resp.Scopes))
	for _, scope := range resp.Scopes {
		scopes = append(scopes, Scope{
			ID:        scope.Id,
			Name:      scope.Name,
			Allowlist: scope.Allowlist,
			Denylist:  scope.Denylist,
			Indexed:   scope.Indexed,
		})
	}
	return scopes, nil
}

// sameOriginAuthTransport is the final network boundary. It prevents both
// direct and redirected traffic from leaving the exact configured Caido
// origin, then injects the token only into the cloned local request.
type sameOriginAuthTransport struct {
	base   http.RoundTripper
	origin *url.URL
	token  string
}

func (t *sameOriginAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !sameOrigin(req.URL, t.origin) {
		return nil, fmt.Errorf("refusing request outside configured local Caido origin")
	}
	if !allowedEndpoint(req.Method, req.URL.Path) {
		return nil, fmt.Errorf("refusing unexpected local Caido endpoint or method")
	}
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	if cloned.URL.Path == "/graphql" {
		cloned.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(cloned)
}

func allowedEndpoint(method, path string) bool {
	return (method == http.MethodGet && path == "/health") ||
		(method == http.MethodPost && path == "/graphql")
}

func sameOrigin(candidate, origin *url.URL) bool {
	return strings.EqualFold(candidate.Scheme, origin.Scheme) &&
		strings.EqualFold(candidate.Host, origin.Host)
}
