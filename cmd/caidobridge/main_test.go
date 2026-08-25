package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestReleaseVersion(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run([]string{"version"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "CaidoBridge v0.4.0" {
		t.Fatalf("version = %q; want CaidoBridge v0.4.0", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("version wrote stderr: %q", stderr.String())
	}
}

func TestReplayInstructionsHaveNoCumulativeRequestBudget(t *testing.T) {
	instructions := serverInstructions(true)
	if strings.Contains(instructions, "hard three-request budget") {
		t.Fatalf("obsolete budget instruction remains: %s", instructions)
	}
	if !strings.Contains(instructions, "no cumulative active-request budget") {
		t.Fatalf("unbounded sequential hunt instruction missing: %s", instructions)
	}
}

func TestDoctorPerformsOnlySafeLocalReadsAndHidesToken(t *testing.T) {
	const token = "DOCTOR-SECRET-TOKEN"
	var graphqlCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("health received Authorization = %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ready":true}`)
		case "/graphql":
			graphqlCalls.Add(1)
			if r.Method != http.MethodPost {
				t.Fatalf("GraphQL method = %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer "+token {
				t.Fatalf("GraphQL Authorization = %q", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/json")
			switch {
			case bytes.Contains(body, []byte("PHCaidoRuntime")):
				fmt.Fprint(w, `{"data":{"runtime":{"version":"0.99.0","platform":"windows"}}}`)
			case bytes.Contains(body, []byte("GetCurrentProject")):
				fmt.Fprint(w, `{"data":{"currentProject":{"project":{"id":"project-1","name":"Authorized Web Test","path":"project-1","size":1,"status":"READY","temporary":false,"createdAt":"2026-08-18T00:00:00Z","updatedAt":"2026-08-18T00:00:00Z","version":"1.0.0"},"readOnly":false}}}`)
			case bytes.Contains(body, []byte("ListRequests")):
				fmt.Fprint(w, `{"data":{"requests":{"edges":[],"pageInfo":{"hasNextPage":false,"hasPreviousPage":false,"startCursor":null,"endCursor":null},"count":{"value":0}}}}`)
			default:
				t.Fatalf("unexpected GraphQL query: %s", body)
			}
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("CAIDO_URL", server.URL)
	t.Setenv("CAIDO_ACCESS_TOKEN", token)
	var output bytes.Buffer
	if err := runDoctor(&output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), token) {
		t.Fatalf("doctor leaked token: %s", output.String())
	}
	if !strings.Contains(output.String(), "doctor: all checks passed") {
		t.Fatalf("unexpected doctor output: %s", output.String())
	}
	if graphqlCalls.Load() != 3 {
		t.Fatalf("GraphQL calls = %d; want 3", graphqlCalls.Load())
	}
}
