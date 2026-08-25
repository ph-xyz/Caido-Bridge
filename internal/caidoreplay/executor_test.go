package caidoreplay

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	gen "github.com/caido-community/sdk-go/graphql"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestLocalGraphQLTransportRestrictsOriginEndpointAndHidesMutation(t *testing.T) {
	origin, err := url.Parse("http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	const token = "SECRET-TOKEN"
	called := false
	transport := &localGraphQLTransport{
		origin: origin,
		token:  token,
		base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			called = true
			if request.Header.Get("Authorization") != "Bearer "+token {
				t.Fatalf("missing local auth header")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{}")),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		}),
	}
	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://127.0.0.1:8080/graphql",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("local GraphQL request was not forwarded")
	}
	if request.Header.Get("Authorization") != "" {
		t.Fatal("transport modified the caller's request with the token")
	}

	blocked := []struct {
		method string
		url    string
	}{
		{method: http.MethodPost, url: "https://attacker.example/graphql"},
		{method: http.MethodGet, url: "http://127.0.0.1:8080/graphql"},
		{method: http.MethodPost, url: "http://127.0.0.1:8080/health"},
	}
	for _, item := range blocked {
		request, err := http.NewRequest(item.method, item.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := transport.RoundTrip(request); err == nil {
			t.Fatalf("unsafe request was allowed: %s %s", item.method, item.url)
		}
	}
}

func TestDecodeMessageRejectsOversizedOrInvalidBase64(t *testing.T) {
	if _, err := decodeMessage("test", "not-base64"); err == nil {
		t.Fatal("invalid base64 was accepted")
	}
	if _, err := decodeMessage("test", strings.Repeat("A", maxEncodedMessage+1)); err == nil {
		t.Fatal("oversized message was accepted")
	}
}

func TestValidateStartedTaskRejectsPayloadErrorsAndMissingTask(t *testing.T) {
	if err := validateStartedTask(nil); err == nil {
		t.Fatal("nil response was accepted")
	}

	inProgressName := "TaskInProgressUserError"
	inProgress := gen.StartReplayTaskStartReplayTaskStartReplayTaskPayloadErrorStartReplayTaskError(
		&gen.StartReplayTaskStartReplayTaskStartReplayTaskPayloadErrorTaskInProgressUserError{
			Typename: &inProgressName,
		},
	)
	response := &gen.StartReplayTaskResponse{
		StartReplayTask: gen.StartReplayTaskStartReplayTaskStartReplayTaskPayload{
			Error: &inProgress,
		},
	}
	if err := validateStartedTask(response); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("task-in-progress error = %v", err)
	}

	unknownName := "UnknownIdUserError"
	unknown := gen.StartReplayTaskStartReplayTaskStartReplayTaskPayloadErrorStartReplayTaskError(
		&gen.StartReplayTaskStartReplayTaskStartReplayTaskPayloadErrorUnknownIdUserError{
			Typename: &unknownName,
		},
	)
	response.StartReplayTask.Error = &unknown
	if err := validateStartedTask(response); err == nil || !strings.Contains(err.Error(), unknownName) {
		t.Fatalf("unknown-ID error = %v", err)
	}

	response.StartReplayTask.Error = nil
	if err := validateStartedTask(response); err == nil || !strings.Contains(err.Error(), "neither task nor error") {
		t.Fatalf("missing-task error = %v", err)
	}
}
