package httputil

import (
	"encoding/base64"
	"testing"
)

func TestSensitiveHeadersAreAlwaysRedacted(t *testing.T) {
	raw := []byte("POST /login HTTP/1.1\r\n" +
		"Host: example.com\r\n" +
		"Authorization: Bearer secret\r\n" +
		"Cookie: sid=secret\r\n" +
		"Set-Cookie: sid=secret\r\n" +
		"Proxy-Authorization: Basic secret\r\n" +
		"X-Api-Key: secret\r\n" +
		"X-Auth-Token: secret\r\n" +
		"X-CSRF-Token: secret\r\n" +
		"X-XSRF-TOKEN: secret\r\n" +
		"X-Safe: visible\r\n\r\nbody")
	parsed := ParseRaw(raw, true, true, 0, DefaultBodyLimit)

	if len(parsed.Headers) != 10 {
		t.Fatalf("got %d headers; want 10", len(parsed.Headers))
	}
	for _, header := range parsed.Headers {
		if header.Name == "Host" || header.Name == "X-Safe" {
			if header.Value == redactedValue {
				t.Fatalf("non-sensitive header %q was redacted", header.Name)
			}
			continue
		}
		if header.Value != redactedValue {
			t.Fatalf("header %q = %q; want redaction", header.Name, header.Value)
		}
	}
}

func TestParseRawSupportsLFAndBodyLimit(t *testing.T) {
	parsed := ParseRaw(
		[]byte("HTTP/1.1 200 OK\nContent-Type: text/plain\n\nabcdefghij"),
		true,
		true,
		2,
		4,
	)
	if parsed.FirstLine != "HTTP/1.1 200 OK" {
		t.Fatalf("first line = %q", parsed.FirstLine)
	}
	if parsed.Body != "cdef" {
		t.Fatalf("body = %q; want cdef", parsed.Body)
	}
	if parsed.BodySize != 10 || parsed.BodyReturned != 4 || !parsed.Truncated {
		t.Fatalf("unexpected body metadata: %+v", parsed)
	}
	if parsed.NextBodyOffset == nil || *parsed.NextBodyOffset != 6 {
		t.Fatalf("next offset = %v; want 6", parsed.NextBodyOffset)
	}
}

func TestParseRawBinaryBodyUsesBase64(t *testing.T) {
	parsed := ParseRaw([]byte{'H', 'T', 'T', 'P', '/', '1', '.', '1', ' ', '2', '0', '0', '\r', '\n', '\r', '\n', 0xff, 0x00}, false, true, 0, 10)
	if parsed.BodyEncoding != "base64" {
		t.Fatalf("encoding = %q; want base64", parsed.BodyEncoding)
	}
	if parsed.Body != base64.StdEncoding.EncodeToString([]byte{0xff, 0x00}) {
		t.Fatalf("binary body = %q", parsed.Body)
	}
}

func TestParseBase64RejectsInvalidInput(t *testing.T) {
	if _, err := ParseBase64("not-base64", true, true, 0, 10); err == nil {
		t.Fatal("ParseBase64 succeeded; want error")
	}
}

func TestBuildURL(t *testing.T) {
	tests := map[string]string{
		BuildURL(true, "example.com", 443, "/api", "a=1"): "https://example.com/api?a=1",
		BuildURL(false, "example.com", 8080, "", ""):      "http://example.com:8080/",
		BuildURL(true, "::1", 8443, "/", ""):              "https://[::1]:8443/",
	}
	for got, want := range tests {
		if got != want {
			t.Fatalf("BuildURL = %q; want %q", got, want)
		}
	}
}
