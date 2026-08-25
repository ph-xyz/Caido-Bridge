package caidoread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	gql "github.com/Khan/genqlient/graphql"
)

type querySpy struct {
	queries []string
}

func (s *querySpy) MakeRequest(_ context.Context, req *gql.Request, _ *gql.Response) error {
	s.queries = append(s.queries, req.Query)
	return nil
}

func TestConnectDoesNotSendTokenToHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("path = %q; want /health", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("health request received Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ready":true}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "LOCAL-SECRET-TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeRequestsOnlyVersionAndPlatform(t *testing.T) {
	const token = "LOCAL-SECRET-TOKEN"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Fatalf("path = %q; want /graphql", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			t.Fatalf("Authorization = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), "logs") {
			t.Fatalf("runtime query requested logs: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"runtime":{"version":"0.99.0","platform":"windows"}}}`)
	}))
	defer server.Close()

	client, err := New(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := client.Runtime(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Version != "0.99.0" || runtime.Platform != "windows" {
		t.Fatalf("unexpected runtime: %+v", runtime)
	}
}

func TestConnectDoesNotFollowRedirects(t *testing.T) {
	var redirectedCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedCalls.Add(1)
	}))
	defer destination.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer origin.Close()

	client, err := New(origin.URL, "TOP-SECRET-TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	err = client.Connect(context.Background())
	if err == nil {
		t.Fatal("Connect followed redirect; want error")
	}
	if strings.Contains(err.Error(), "TOP-SECRET-TOKEN") {
		t.Fatalf("error leaked token: %v", err)
	}
	if redirectedCalls.Load() != 0 {
		t.Fatalf("redirect destination received %d calls", redirectedCalls.Load())
	}
}

func TestAllExposedCaidoOperationsAreQueries(t *testing.T) {
	spy := &querySpy{}
	client := &Client{graphql: spy}
	ctx := context.Background()

	if _, err := client.Runtime(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CurrentProject(ctx); !errors.Is(err, ErrNoCurrentProject) {
		t.Fatalf("CurrentProject error = %v; want ErrNoCurrentProject", err)
	}
	if _, err := client.ListProjects(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListRequests(ctx, ListRequestsParams{Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetRequest(ctx, "1"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("GetRequest error = %v; want ErrRequestNotFound", err)
	}
	if _, err := client.ListSitemap(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListSitemap(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListScopes(ctx); err != nil {
		t.Fatal(err)
	}

	if len(spy.queries) != 8 {
		t.Fatalf("captured %d GraphQL operations; want 8", len(spy.queries))
	}
	for _, operation := range spy.queries {
		normalized := strings.ToLower(strings.TrimSpace(operation))
		if !strings.HasPrefix(normalized, "query ") || strings.Contains(normalized, "mutation") {
			t.Fatalf("non-query GraphQL operation registered: %s", operation)
		}
	}
}

type graphqlRequestBody struct {
	Query     string                     `json:"query"`
	Variables map[string]json.RawMessage `json:"variables"`
	OpName    string                     `json:"operationName"`
}

func TestHTTPHistoryUsesCaidoRowIDWithoutDerivation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" {
			t.Fatalf("path = %q; want /graphql", r.URL.Path)
		}
		var body graphqlRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch body.OpName {
		case "PHListRequestsWithRowID":
			if !strings.Contains(body.Query, "metadata { id }") {
				t.Fatalf("list query does not select metadata.id: %s", body.Query)
			}
			var first int
			if err := json.Unmarshal(body.Variables["first"], &first); err != nil {
				t.Fatal(err)
			}
			if first != 2 {
				t.Fatalf("first = %d; want 2", first)
			}
			var after string
			if err := json.Unmarshal(body.Variables["after"], &after); err != nil {
				t.Fatal(err)
			}
			if after != "previous-page" {
				t.Fatalf("after = %q; want previous-page", after)
			}
			var filter struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(body.Variables["filter"], &filter); err != nil {
				t.Fatal(err)
			}
			if filter.Code != `req.path.cont:"/resources/"` {
				t.Fatalf("HTTPQL = %q", filter.Code)
			}
			fmt.Fprint(w, `{"data":{"requests":{"edges":[`+
				`{"cursor":"cursor-119","node":{"id":"125","metadata":{"id":"119"},`+
				`"method":"GET","host":"example.test","port":443,`+
				`"path":"/resources/labheader/images/ps-lab-notsolved.svg",`+
				`"query":"","isTls":true,"response":{"statusCode":200}}},`+
				`{"cursor":"cursor-113","node":{"id":"119","metadata":{"id":"113"},`+
				`"method":"GET","host":"example.test","port":443,`+
				`"path":"/image","query":"filename=10.jpg","isTls":true,`+
				`"response":{"statusCode":200}}}],`+
				`"pageInfo":{"hasNextPage":false,"endCursor":"cursor-113"}}}}`)
		case "PHGetRequestByRowID":
			if strings.Contains(body.Query, "request(id:") {
				t.Fatalf("get query passed the UI row ID as an internal request ID: %s", body.Query)
			}
			if !strings.Contains(body.Query, "metadata { id }") {
				t.Fatalf("get query does not select metadata.id: %s", body.Query)
			}
			var filter struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(body.Variables["filter"], &filter); err != nil {
				t.Fatal(err)
			}
			switch filter.Code {
			case "row.id.eq:119":
				fmt.Fprint(w, `{"data":{"requests":{"edges":[{"node":{`+
					`"id":"125","metadata":{"id":"119"},"method":"GET",`+
					`"host":"example.test","port":443,`+
					`"path":"/resources/labheader/images/ps-lab-notsolved.svg",`+
					`"query":"","isTls":true,"raw":"",`+
					`"createdAt":1787029231611,"length":100,`+
					`"response":{"statusCode":200,"raw":"","roundtripTime":1075,`+
					`"length":200,"createdAt":1787029232686}}}]}}}`)
			case "row.id.eq:113":
				fmt.Fprint(w, `{"data":{"requests":{"edges":[{"node":{`+
					`"id":"119","metadata":{"id":"113"},"method":"GET",`+
					`"host":"example.test","port":443,"path":"/image",`+
					`"query":"filename=10.jpg","isTls":true,"raw":"",`+
					`"createdAt":1787029200000,"length":100,`+
					`"response":{"statusCode":200,"raw":"","roundtripTime":2218,`+
					`"length":200,"createdAt":1787029202218}}}]}}}`)
			default:
				t.Fatalf("unexpected row filter %q", filter.Code)
			}
		default:
			t.Fatalf("unexpected operation %q", body.OpName)
		}
	}))
	defer server.Close()

	client, err := New(server.URL, "TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ListRequests(context.Background(), ListRequestsParams{
		HTTPQL: `req.path.cont:"/resources/"`,
		Limit:  2,
		After:  "previous-page",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Requests) != 2 {
		t.Fatalf("got %d requests; want 2", len(page.Requests))
	}
	if page.Requests[0].ID != "119" || page.Requests[0].Path != "/resources/labheader/images/ps-lab-notsolved.svg" {
		t.Fatalf("first summary = %+v; want Caido UI row 119", page.Requests[0])
	}
	if page.Requests[1].ID != "113" || page.Requests[1].Query != "filename=10.jpg" {
		t.Fatalf("second summary = %+v; want Caido UI row 113", page.Requests[1])
	}
	if page.HasMore || page.NextCursor != "cursor-113" {
		t.Fatalf("pagination was not preserved: %+v", page)
	}

	request119, err := client.GetRequest(context.Background(), "119")
	if err != nil {
		t.Fatal(err)
	}
	if request119.ID != "119" || request119.Path != "/resources/labheader/images/ps-lab-notsolved.svg" {
		t.Fatalf("request 119 = %+v", request119)
	}
	if request119.Response == nil || request119.Response.RoundtripMS != 1075 {
		t.Fatalf("request 119 response = %+v; want 1075 ms", request119.Response)
	}

	request113, err := client.GetRequest(context.Background(), "113")
	if err != nil {
		t.Fatal(err)
	}
	if request113.ID != "113" || request113.Path != "/image" || request113.Query != "filename=10.jpg" {
		t.Fatalf("request 113 = %+v", request113)
	}
	if request113.Response == nil || request113.Response.RoundtripMS != 2218 {
		t.Fatalf("request 113 response = %+v; want 2218 ms", request113.Response)
	}
}

func TestGetRequestRejectsInvalidUIRowID(t *testing.T) {
	client := &Client{graphql: &querySpy{}}
	for _, id := range []string{"", "internal-125", "-1", "1.5", "1 OR 1"} {
		if _, err := client.GetRequest(context.Background(), id); !errors.Is(err, ErrInvalidRequestID) {
			t.Fatalf("GetRequest(%q) error = %v; want ErrInvalidRequestID", id, err)
		}
	}
}

func TestGetRequestDoesNotAcceptMismatchedRow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"requests":{"edges":[{"node":{`+
			`"id":"125","metadata":{"id":"120"},"method":"GET",`+
			`"host":"example.test","port":443,"path":"/wrong",`+
			`"query":"","isTls":true,"raw":"","createdAt":1,"length":1,`+
			`"response":null}}]}}}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetRequest(context.Background(), "119"); err == nil || !strings.Contains(err.Error(), `"120" while "119" was requested`) {
		t.Fatalf("GetRequest mismatch error = %v", err)
	}
}

func TestListRequestsNeverFallsBackToInternalID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"requests":{"edges":[{"node":{`+
			`"id":"125","metadata":{"id":""},"method":"GET",`+
			`"host":"example.test","port":443,"path":"/internal-only",`+
			`"query":"","isTls":true,"response":null}}],`+
			`"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListRequests(context.Background(), ListRequestsParams{Limit: 1}); err == nil || !strings.Contains(err.Error(), `request "125" has invalid HTTP History row ID`) {
		t.Fatalf("ListRequests error = %v; want refusal instead of internal ID fallback", err)
	}
}

func TestSitemapPublishesRequestRowID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body graphqlRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.OpName != "PHListSitemapDescendantsWithRowID" {
			t.Fatalf("operation = %q", body.OpName)
		}
		if !strings.Contains(body.Query, "metadata { id }") {
			t.Fatalf("sitemap query does not select metadata.id: %s", body.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":{"sitemapDescendantEntries":{"edges":[{"node":{`+
			`"id":"sitemap-entry","label":"ps-lab-notsolved.svg","kind":"REQUEST",`+
			`"parentId":"parent","hasDescendants":false,"request":{`+
			`"id":"125","metadata":{"id":"119"},"method":"GET",`+
			`"path":"/resources/labheader/images/ps-lab-notsolved.svg",`+
			`"response":{"statusCode":200}}}}]}}}`)
	}))
	defer server.Close()

	client, err := New(server.URL, "TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := client.ListSitemap(context.Background(), "parent")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].RequestID == nil || *entries[0].RequestID != "119" {
		t.Fatalf("entries = %+v; want requestId 119", entries)
	}
}
