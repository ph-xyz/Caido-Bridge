package replay

import (
	"bytes"
	"testing"
)

func sampleRequest(t *testing.T) *Request {
	t.Helper()
	raw := []byte("PATCH /backend/cart/v1/unit/612633976?quantity=5&keep=a%20b HTTP/1.1\r\n" +
		"Host: www.example.com\r\n" +
		"Content-Type: application/json\r\n" +
		"Cookie: session=abc; theme=dark\r\n" +
		"X-Test: original\r\n" +
		"Content-Length: 31\r\n\r\n" +
		`{"userId":"12","quantity":5}`)
	request, err := ParseRequest(raw)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestApplyDoesNotModifyOriginalAndChangesOnlyRequestedQueryValue(t *testing.T) {
	original := sampleRequest(t)
	before := original.Raw()
	mutated, err := Apply(original, Mutation{
		Type:   MutationReplaceQueryParameter,
		Target: "quantity",
		From:   "5",
		To:     "7",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original.Raw(), before) {
		t.Fatal("original request was modified")
	}
	if got := mutated.Target; got != "/backend/cart/v1/unit/612633976?quantity=7&keep=a%20b" {
		t.Fatalf("target = %q", got)
	}
	if got := mutated.HeaderValue("Cookie"); got != "session=abc; theme=dark" {
		t.Fatalf("Cookie changed unexpectedly: %q", got)
	}
}

func TestStructuredHeaderCookiePathAndJSONMutations(t *testing.T) {
	tests := []struct {
		name     string
		mutation Mutation
		check    func(*testing.T, *Request)
	}{
		{
			name:     "header",
			mutation: Mutation{Type: MutationReplaceHeader, Target: "X-Test", From: "original", To: "changed"},
			check: func(t *testing.T, request *Request) {
				if request.HeaderValue("X-Test") != "changed" {
					t.Fatal("header was not changed")
				}
			},
		},
		{
			name:     "cookie",
			mutation: Mutation{Type: MutationReplaceCookie, Target: "theme", From: "dark", To: "light"},
			check: func(t *testing.T, request *Request) {
				if request.HeaderValue("Cookie") != "session=abc; theme=light" {
					t.Fatalf("unexpected Cookie: %q", request.HeaderValue("Cookie"))
				}
			},
		},
		{
			name:     "path segment",
			mutation: Mutation{Type: MutationChangePathSegment, Target: "4", From: "612633976", To: "612679217"},
			check: func(t *testing.T, request *Request) {
				if request.Target != "/backend/cart/v1/unit/612679217?quantity=5&keep=a%20b" {
					t.Fatalf("unexpected target: %q", request.Target)
				}
			},
		},
		{
			name:     "JSON field",
			mutation: Mutation{Type: MutationReplaceJSONField, Target: "userId", From: "12", To: "13"},
			check: func(t *testing.T, request *Request) {
				if string(request.Body) != `{"quantity":5,"userId":"13"}` {
					t.Fatalf("unexpected body: %s", request.Body)
				}
				if request.HeaderValue("Content-Length") != "28" {
					t.Fatalf("Content-Length = %q", request.HeaderValue("Content-Length"))
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := Apply(sampleRequest(t), test.mutation)
			if err != nil {
				t.Fatal(err)
			}
			test.check(t, request)
		})
	}
}

func TestHostAndFramingHeadersCannotBeMutated(t *testing.T) {
	for _, name := range []string{"Host", "Content-Length", "Transfer-Encoding"} {
		_, err := Apply(sampleRequest(t), Mutation{
			Type:   MutationReplaceHeader,
			Target: name,
			To:     "attacker.example",
		})
		if err == nil {
			t.Fatalf("protected header %q was mutable", name)
		}
	}
}

func TestMutationFailsWhenExpectedOriginalValueDoesNotMatch(t *testing.T) {
	_, err := Apply(sampleRequest(t), Mutation{
		Type:   MutationReplaceQueryParameter,
		Target: "quantity",
		From:   "999",
		To:     "7",
	})
	if err == nil {
		t.Fatal("mutation succeeded with a mismatched from value")
	}
}

func TestMutationRejectsInvalidHTTPSyntax(t *testing.T) {
	tests := []Mutation{
		{Type: MutationChangeMethod, To: "GET\x00"},
		{Type: MutationReplacePath, To: "/unsafe path"},
		{Type: MutationAddHeader, Target: "Bad Header", To: "value"},
		{Type: MutationAddHeader, Target: "X-Test-2", To: "value\x00"},
		{Type: MutationReplaceCookie, Target: "bad;name", To: "value"},
		{Type: MutationReplaceCookie, Target: "theme", To: "light;admin=true"},
	}
	for _, mutation := range tests {
		if _, err := Apply(sampleRequest(t), mutation); err == nil {
			t.Fatalf("invalid mutation was accepted: %+v", mutation)
		}
	}
}

func TestCookieReplacementPreservesUnrelatedFormatting(t *testing.T) {
	request := sampleRequest(t)
	request.replaceSingleHeader("Cookie", "session=abc;\t theme=dark; flag=1")
	mutated, err := Apply(request, Mutation{
		Type: MutationReplaceCookie, Target: "theme", From: "dark", To: "light",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := mutated.HeaderValue("Cookie"); got != "session=abc;\t theme=light; flag=1" {
		t.Fatalf("unrelated cookie formatting changed: %q", got)
	}
}
