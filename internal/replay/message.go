// Package replay reconstructs captured HTTP messages, applies explicit
// structured mutations, and produces objective response diffs. It performs no
// network I/O.
package replay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const MaxRequestBytes = 2 * 1024 * 1024

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
	Name  string
	Value string
}

type Request struct {
	Method   string
	Target   string
	Protocol string
	Headers  []Header
	Body     []byte
}

type Response struct {
	Protocol   string
	StatusCode int
	Reason     string
	Headers    []Header
	Body       []byte
}

func ParseRequest(raw []byte) (*Request, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("captured request is empty")
	}
	if len(raw) > MaxRequestBytes {
		return nil, fmt.Errorf("captured request exceeds %d bytes", MaxRequestBytes)
	}
	headerPart, body := splitMessage(raw)
	lines := splitLines(headerPart)
	if len(lines) == 0 {
		return nil, fmt.Errorf("captured request has no request line")
	}
	parts := strings.Fields(string(lines[0]))
	if len(parts) != 3 || !validHTTPToken(parts[0]) || !strings.HasPrefix(parts[2], "HTTP/") {
		return nil, fmt.Errorf("captured request has a malformed request line")
	}
	headers, err := parseHeaders(lines[1:])
	if err != nil {
		return nil, err
	}
	return &Request{
		Method:   parts[0],
		Target:   parts[1],
		Protocol: parts[2],
		Headers:  headers,
		Body:     bytes.Clone(body),
	}, nil
}

func ParseResponse(raw []byte) (*Response, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("response is empty")
	}
	headerPart, body := splitMessage(raw)
	lines := splitLines(headerPart)
	if len(lines) == 0 {
		return nil, fmt.Errorf("response has no status line")
	}
	parts := strings.SplitN(strings.TrimSpace(string(lines[0])), " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "HTTP/") {
		return nil, fmt.Errorf("response has a malformed status line")
	}
	status, err := strconv.Atoi(parts[1])
	if err != nil || status < 100 || status > 999 {
		return nil, fmt.Errorf("response has an invalid status code")
	}
	reason := ""
	if len(parts) == 3 {
		reason = parts[2]
	}
	headers, err := parseHeaders(lines[1:])
	if err != nil {
		return nil, err
	}
	return &Response{
		Protocol:   parts[0],
		StatusCode: status,
		Reason:     reason,
		Headers:    headers,
		Body:       bytes.Clone(body),
	}, nil
}

func parseHeaders(lines [][]byte) ([]Header, error) {
	headers := make([]Header, 0, len(lines))
	for _, rawLine := range lines {
		line := string(rawLine)
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			return nil, fmt.Errorf("obsolete folded headers are not supported for Replay")
		}
		index := strings.IndexByte(line, ':')
		if index <= 0 {
			return nil, fmt.Errorf("captured message contains a malformed header")
		}
		name := strings.TrimSpace(line[:index])
		value := strings.TrimSpace(line[index+1:])
		if !validHTTPToken(name) {
			return nil, fmt.Errorf("captured message contains an invalid header name")
		}
		if containsInvalidHeaderValueByte(value) {
			return nil, fmt.Errorf("captured message contains an invalid header value")
		}
		headers = append(headers, Header{Name: name, Value: value})
	}
	return headers, nil
}

func validHTTPToken(value string) bool {
	if value == "" {
		return false
	}
	const separators = "()<>@,;:\\\"/[]?={} \t"
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character <= 0x20 || character >= 0x7f ||
			strings.ContainsRune(separators, rune(character)) {
			return false
		}
	}
	return true
}

func containsInvalidHeaderValueByte(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 0x20 && character != '\t') || character == 0x7f {
			return true
		}
	}
	return false
}

func (r *Request) Clone() *Request {
	clone := *r
	clone.Headers = append([]Header(nil), r.Headers...)
	clone.Body = bytes.Clone(r.Body)
	return &clone
}

func (r *Request) Raw() []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "%s %s %s\r\n", r.Method, r.Target, r.Protocol)
	for _, header := range r.Headers {
		fmt.Fprintf(&out, "%s: %s\r\n", header.Name, header.Value)
	}
	out.WriteString("\r\n")
	out.Write(r.Body)
	return out.Bytes()
}

func (r *Request) PathAndQuery() (string, string, error) {
	if r.Target == "*" {
		return "*", "", nil
	}
	if !strings.HasPrefix(r.Target, "/") {
		return "", "", fmt.Errorf("Replay only supports origin-form request targets")
	}
	path, query, found := strings.Cut(r.Target, "?")
	if !found {
		query = ""
	}
	if path == "" {
		path = "/"
	}
	return path, query, nil
}

func (r *Request) HeaderValues(name string) []string {
	var values []string
	for _, header := range r.Headers {
		if strings.EqualFold(header.Name, name) {
			values = append(values, header.Value)
		}
	}
	return values
}

func (r *Request) HeaderValue(name string) string {
	values := r.HeaderValues(name)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (r *Request) replaceSingleHeader(name, value string) error {
	index := -1
	for i, header := range r.Headers {
		if strings.EqualFold(header.Name, name) {
			if index >= 0 {
				return fmt.Errorf("header %q occurs more than once", name)
			}
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("header %q does not exist", name)
	}
	r.Headers[index].Value = value
	return nil
}

func (r *Request) removeSingleHeader(name string) error {
	index := -1
	for i, header := range r.Headers {
		if strings.EqualFold(header.Name, name) {
			if index >= 0 {
				return fmt.Errorf("header %q occurs more than once", name)
			}
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("header %q does not exist", name)
	}
	r.Headers = append(r.Headers[:index], r.Headers[index+1:]...)
	return nil
}

func (r *Request) addHeader(name, value string) error {
	if len(r.HeaderValues(name)) != 0 {
		return fmt.Errorf("header %q already exists", name)
	}
	r.Headers = append(r.Headers, Header{Name: name, Value: value})
	return nil
}

func (r *Request) updateContentLength() error {
	if len(r.HeaderValues("Transfer-Encoding")) != 0 {
		return fmt.Errorf("body mutation is not supported with Transfer-Encoding")
	}
	values := r.HeaderValues("Content-Length")
	if len(values) == 0 {
		if len(r.Body) > 0 {
			r.Headers = append(r.Headers, Header{
				Name:  "Content-Length",
				Value: strconv.Itoa(len(r.Body)),
			})
		}
		return nil
	}
	if len(values) != 1 {
		return fmt.Errorf("multiple Content-Length headers are not safe to mutate")
	}
	return r.replaceSingleHeader("Content-Length", strconv.Itoa(len(r.Body)))
}

func (r *Response) HeaderValue(name string) string {
	for _, header := range r.Headers {
		if strings.EqualFold(header.Name, name) {
			return header.Value
		}
	}
	return ""
}

func RedactedHeaders(headers []Header) []Header {
	out := make([]Header, 0, len(headers))
	for _, header := range headers {
		value := header.Value
		if _, sensitive := sensitiveHeaders[strings.ToLower(header.Name)]; sensitive {
			value = "[REDACTED: preserved]"
		}
		out = append(out, Header{Name: header.Name, Value: value})
	}
	return out
}

func (r *Request) RedactedRaw() string {
	clone := r.Clone()
	clone.Headers = RedactedHeaders(clone.Headers)
	return string(clone.Raw())
}

func (r *Request) CookieNames() []string {
	var names []string
	for _, cookieHeader := range r.HeaderValues("Cookie") {
		for _, part := range strings.Split(cookieHeader, ";") {
			name, _, found := strings.Cut(strings.TrimSpace(part), "=")
			if found && name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func (r *Request) AuthenticationHeaderNames() []string {
	var names []string
	for _, header := range r.Headers {
		lower := strings.ToLower(header.Name)
		if _, sensitive := sensitiveHeaders[lower]; sensitive && lower != "cookie" {
			names = append(names, header.Name)
		}
	}
	return names
}

func SHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func splitMessage(raw []byte) ([]byte, []byte) {
	if index := bytes.Index(raw, []byte("\r\n\r\n")); index >= 0 {
		return raw[:index], raw[index+4:]
	}
	if index := bytes.Index(raw, []byte("\n\n")); index >= 0 {
		return raw[:index], raw[index+2:]
	}
	return raw, nil
}

func splitLines(raw []byte) [][]byte {
	normalized := bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	return bytes.Split(normalized, []byte("\n"))
}
