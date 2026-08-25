package tools

import (
	"strings"
	"testing"

	"github.com/ph-xyz/Caido-Bridge/internal/httputil"
)

func TestNormalizeListRequestsInput(t *testing.T) {
	got, err := normalizeListRequestsInput(ListRequestsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Limit != defaultRequestLimit {
		t.Fatalf("default limit = %d; want %d", got.Limit, defaultRequestLimit)
	}

	invalid := []ListRequestsInput{
		{Limit: -1},
		{Limit: maxRequestLimit + 1},
		{HTTPQL: strings.Repeat("x", maxHTTPQLLength+1)},
		{After: strings.Repeat("x", maxCursorLength+1)},
	}
	for _, input := range invalid {
		if _, err := normalizeListRequestsInput(input); err == nil {
			t.Fatalf("normalizeListRequestsInput(%+v) succeeded; want error", input)
		}
	}
}

func TestNormalizeGetRequestInput(t *testing.T) {
	got, err := normalizeGetRequestInput(GetRequestInput{ID: "  42  "})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "42" || got.BodyLimit != httputil.DefaultBodyLimit {
		t.Fatalf("unexpected normalization: %+v", got)
	}

	invalid := []GetRequestInput{
		{},
		{ID: "request-42"},
		{ID: "-1"},
		{ID: "4.2"},
		{ID: "42", BodyOffset: -1},
		{ID: "42", BodyLimit: -1},
		{ID: "42", BodyLimit: httputil.MaxBodyLimit + 1},
		{ID: "42", Include: []string{"raw"}},
		{ID: "42", Include: []string{"requestBody", "requestBody"}},
	}
	for _, input := range invalid {
		if _, err := normalizeGetRequestInput(input); err == nil {
			t.Fatalf("normalizeGetRequestInput(%+v) succeeded; want error", input)
		}
	}
}
