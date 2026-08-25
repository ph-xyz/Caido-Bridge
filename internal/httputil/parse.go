// Package httputil contains the small amount of raw HTTP parsing required to
// present captured messages safely to an MCP client.
package httputil

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"unicode/utf8"
)

const (
	DefaultBodyLimit = 2 * 1024
	MaxBodyLimit     = 64 * 1024
)

const redactedValue = "[REDACTED]"

var sensitiveHeaders = map[string]struct{}{
	"authorization":       {},
	"cookie":              {},
	"set-cookie":          {},
	"proxy-authorization": {},
	"x-api-key":           {},
	"x-auth-token":        {},
	"x-csrf-token":        {},
	"x-xsrf-token":        {},
}

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ParsedMessage struct {
	FirstLine      string   `json:"firstLine,omitempty"`
	Headers        []Header `json:"headers,omitempty"`
	Body           string   `json:"body,omitempty"`
	BodyEncoding   string   `json:"bodyEncoding,omitempty"`
	BodySize       int      `json:"bodySize"`
	BodyOffset     int      `json:"bodyOffset,omitempty"`
	BodyReturned   int      `json:"bodyReturned,omitempty"`
	Truncated      bool     `json:"truncated,omitempty"`
	NextBodyOffset *int     `json:"nextBodyOffset,omitempty"`
}

func ParseBase64(
	raw string,
	includeHeaders bool,
	includeBody bool,
	bodyOffset int,
	bodyLimit int,
) (*ParsedMessage, error) {
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode captured HTTP message: %w", err)
	}
	return ParseRaw(decoded, includeHeaders, includeBody, bodyOffset, bodyLimit), nil
}

func ParseRaw(
	raw []byte,
	includeHeaders bool,
	includeBody bool,
	bodyOffset int,
	bodyLimit int,
) *ParsedMessage {
	headerPart, bodyPart := splitMessage(raw)
	result := &ParsedMessage{BodySize: len(bodyPart)}

	if includeHeaders {
		lines := splitLines(headerPart)
		if len(lines) > 0 {
			result.FirstLine = strings.TrimSpace(string(lines[0]))
		}
		for _, rawLine := range lines[1:] {
			line := strings.TrimSpace(string(rawLine))
			if line == "" {
				continue
			}
			idx := strings.IndexByte(line, ':')
			if idx <= 0 {
				continue
			}
			name := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			if _, sensitive := sensitiveHeaders[strings.ToLower(name)]; sensitive {
				value = redactedValue
			}
			result.Headers = append(result.Headers, Header{Name: name, Value: value})
		}
	}

	if !includeBody || len(bodyPart) == 0 {
		return result
	}
	if bodyOffset > len(bodyPart) {
		bodyOffset = len(bodyPart)
	}
	end := len(bodyPart)
	if bodyLimit > 0 && bodyOffset+bodyLimit < end {
		end = bodyOffset + bodyLimit
	}
	chunk := bodyPart[bodyOffset:end]
	result.BodyOffset = bodyOffset
	result.BodyReturned = len(chunk)
	result.Truncated = end < len(bodyPart)
	if result.Truncated {
		next := end
		result.NextBodyOffset = &next
	}
	if utf8.Valid(chunk) {
		result.Body = string(chunk)
		result.BodyEncoding = "utf-8"
	} else {
		result.Body = base64.StdEncoding.EncodeToString(chunk)
		result.BodyEncoding = "base64"
	}
	return result
}

func splitMessage(raw []byte) ([]byte, []byte) {
	if idx := bytes.Index(raw, []byte("\r\n\r\n")); idx >= 0 {
		return raw[:idx], raw[idx+4:]
	}
	if idx := bytes.Index(raw, []byte("\n\n")); idx >= 0 {
		return raw[:idx], raw[idx+2:]
	}
	return raw, nil
}

func splitLines(raw []byte) [][]byte {
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	return bytes.Split(raw, []byte("\n"))
}

func BuildURL(tls bool, host string, port int, path, query string) string {
	scheme := "http"
	defaultPort := 80
	if tls {
		scheme = "https"
		defaultPort = 443
	}

	hostPart := host
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil && strings.Contains(host, ":") {
		hostPart = "[" + strings.Trim(host, "[]") + "]"
	}
	if port > 0 && port != defaultPort {
		hostPart = fmt.Sprintf("%s:%d", hostPart, port)
	}
	if path == "" {
		path = "/"
	}
	result := scheme + "://" + hostPart + path
	if query != "" {
		result += "?" + query
	}
	return result
}
