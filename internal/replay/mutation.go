package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

const (
	MutationChangeMethod          = "change_method"
	MutationReplacePath           = "replace_path"
	MutationChangePathSegment     = "change_path_segment"
	MutationAddQueryParameter     = "add_query_parameter"
	MutationReplaceQueryParameter = "replace_query_parameter"
	MutationRemoveQueryParameter  = "remove_query_parameter"
	MutationAddHeader             = "add_header"
	MutationReplaceHeader         = "replace_header"
	MutationRemoveHeader          = "remove_header"
	MutationReplaceCookie         = "replace_cookie"
	MutationRemoveCookie          = "remove_cookie"
	MutationReplaceJSONField      = "replace_json_field"
	MutationRemoveJSONField       = "remove_json_field"
	MutationReplaceBody           = "replace_body"
)

// Mutation describes exactly one primary variable change. Target is a
// parameter/header/cookie name, JSON field path, or zero-based path segment
// index depending on Type. Format is "string" (default) or "json" for a JSON
// field replacement.
type Mutation struct {
	Type   string
	Target string
	From   string
	To     string
	Format string
}

func Apply(original *Request, mutation Mutation) (*Request, error) {
	if original == nil {
		return nil, fmt.Errorf("original request is required")
	}
	request := original.Clone()
	mutation.Type = strings.TrimSpace(mutation.Type)
	mutation.Target = strings.TrimSpace(mutation.Target)

	var err error
	switch mutation.Type {
	case MutationChangeMethod:
		err = changeMethod(request, mutation)
	case MutationReplacePath:
		err = replacePath(request, mutation)
	case MutationChangePathSegment:
		err = changePathSegment(request, mutation)
	case MutationAddQueryParameter,
		MutationReplaceQueryParameter,
		MutationRemoveQueryParameter:
		err = mutateQuery(request, mutation)
	case MutationAddHeader, MutationReplaceHeader, MutationRemoveHeader:
		err = mutateHeader(request, mutation)
	case MutationReplaceCookie, MutationRemoveCookie:
		err = mutateCookie(request, mutation)
	case MutationReplaceJSONField, MutationRemoveJSONField:
		err = mutateJSON(request, mutation)
	case MutationReplaceBody:
		err = replaceBody(request, mutation)
	default:
		err = fmt.Errorf("unsupported mutation type %q", mutation.Type)
	}
	if err != nil {
		return nil, err
	}
	return request, nil
}

func changeMethod(request *Request, mutation Mutation) error {
	if mutation.From != "" && !strings.EqualFold(request.Method, mutation.From) {
		return fmt.Errorf("method is %q, not expected value %q", request.Method, mutation.From)
	}
	method := strings.ToUpper(strings.TrimSpace(mutation.To))
	if !validHTTPToken(method) {
		return fmt.Errorf("new method is invalid")
	}
	request.Method = method
	return nil
}

func replacePath(request *Request, mutation Mutation) error {
	path, query, err := request.PathAndQuery()
	if err != nil {
		return err
	}
	if mutation.From != "" && path != mutation.From {
		return fmt.Errorf("path is %q, not expected value %q", path, mutation.From)
	}
	if !strings.HasPrefix(mutation.To, "/") ||
		strings.ContainsAny(mutation.To, "\r\n?#") ||
		strings.ContainsFunc(mutation.To, func(character rune) bool {
			return character <= 0x20 || character == 0x7f
		}) {
		return fmt.Errorf("new path must be an origin-form path without query or fragment")
	}
	request.Target = mutation.To
	if query != "" {
		request.Target += "?" + query
	}
	return nil
}

func changePathSegment(request *Request, mutation Mutation) error {
	index, err := strconv.Atoi(mutation.Target)
	if err != nil || index < 0 {
		return fmt.Errorf("path segment target must be a zero-based integer")
	}
	path, query, err := request.PathAndQuery()
	if err != nil {
		return err
	}
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if index >= len(segments) {
		return fmt.Errorf("path segment %d does not exist", index)
	}
	decoded, err := url.PathUnescape(segments[index])
	if err != nil {
		return fmt.Errorf("decode path segment %d: %w", index, err)
	}
	if mutation.From != "" && decoded != mutation.From {
		return fmt.Errorf("path segment %d is %q, not expected value %q", index, decoded, mutation.From)
	}
	if mutation.To == "" || strings.ContainsAny(mutation.To, "/\r\n?#") {
		return fmt.Errorf("new path segment is invalid")
	}
	segments[index] = url.PathEscape(mutation.To)
	request.Target = "/" + strings.Join(segments, "/")
	if query != "" {
		request.Target += "?" + query
	}
	return nil
}

type queryPart struct {
	rawName  string
	rawValue string
	hasValue bool
}

func mutateQuery(request *Request, mutation Mutation) error {
	if mutation.Target == "" {
		return fmt.Errorf("query parameter target is required")
	}
	path, rawQuery, err := request.PathAndQuery()
	if err != nil {
		return err
	}
	parts := parseQueryParts(rawQuery)
	var indexes []int
	for i, part := range parts {
		name, err := url.QueryUnescape(part.rawName)
		if err != nil {
			return fmt.Errorf("decode query parameter name: %w", err)
		}
		if name == mutation.Target {
			indexes = append(indexes, i)
		}
	}

	switch mutation.Type {
	case MutationAddQueryParameter:
		if len(indexes) != 0 {
			return fmt.Errorf("query parameter %q already exists", mutation.Target)
		}
		parts = append(parts, queryPart{
			rawName:  url.QueryEscape(mutation.Target),
			rawValue: url.QueryEscape(mutation.To),
			hasValue: true,
		})
	case MutationReplaceQueryParameter:
		if len(indexes) != 1 {
			return fmt.Errorf("query parameter %q must occur exactly once", mutation.Target)
		}
		current, err := url.QueryUnescape(parts[indexes[0]].rawValue)
		if err != nil {
			return fmt.Errorf("decode query parameter %q: %w", mutation.Target, err)
		}
		if mutation.From != "" && current != mutation.From {
			return fmt.Errorf("query parameter %q is %q, not expected value %q", mutation.Target, current, mutation.From)
		}
		parts[indexes[0]].rawValue = url.QueryEscape(mutation.To)
		parts[indexes[0]].hasValue = true
	case MutationRemoveQueryParameter:
		if len(indexes) != 1 {
			return fmt.Errorf("query parameter %q must occur exactly once", mutation.Target)
		}
		if mutation.From != "" {
			current, err := url.QueryUnescape(parts[indexes[0]].rawValue)
			if err != nil {
				return fmt.Errorf("decode query parameter %q: %w", mutation.Target, err)
			}
			if current != mutation.From {
				return fmt.Errorf("query parameter %q is %q, not expected value %q", mutation.Target, current, mutation.From)
			}
		}
		parts = append(parts[:indexes[0]], parts[indexes[0]+1:]...)
	}

	request.Target = path
	if encoded := encodeQueryParts(parts); encoded != "" {
		request.Target += "?" + encoded
	}
	return nil
}

func parseQueryParts(raw string) []queryPart {
	if raw == "" {
		return nil
	}
	items := strings.Split(raw, "&")
	parts := make([]queryPart, 0, len(items))
	for _, item := range items {
		name, value, found := strings.Cut(item, "=")
		parts = append(parts, queryPart{rawName: name, rawValue: value, hasValue: found})
	}
	return parts
}

func encodeQueryParts(parts []queryPart) string {
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := part.rawName
		if part.hasValue {
			item += "=" + part.rawValue
		}
		items = append(items, item)
	}
	return strings.Join(items, "&")
}

func mutateHeader(request *Request, mutation Mutation) error {
	name := mutation.Target
	if !validHTTPToken(name) {
		return fmt.Errorf("header target is invalid")
	}
	if strings.EqualFold(name, "Host") ||
		strings.EqualFold(name, "Content-Length") ||
		strings.EqualFold(name, "Transfer-Encoding") {
		return fmt.Errorf("header %q is protected by Replay safety guards", name)
	}
	if containsInvalidHeaderValueByte(mutation.To) {
		return fmt.Errorf("header value contains a line break")
	}

	switch mutation.Type {
	case MutationAddHeader:
		return request.addHeader(name, mutation.To)
	case MutationReplaceHeader:
		values := request.HeaderValues(name)
		if len(values) != 1 {
			return fmt.Errorf("header %q must occur exactly once", name)
		}
		if mutation.From != "" && values[0] != mutation.From {
			return fmt.Errorf("header %q does not have the expected value", name)
		}
		return request.replaceSingleHeader(name, mutation.To)
	case MutationRemoveHeader:
		values := request.HeaderValues(name)
		if len(values) != 1 {
			return fmt.Errorf("header %q must occur exactly once", name)
		}
		if mutation.From != "" && values[0] != mutation.From {
			return fmt.Errorf("header %q does not have the expected value", name)
		}
		return request.removeSingleHeader(name)
	}
	return nil
}

func mutateCookie(request *Request, mutation Mutation) error {
	if !validHTTPToken(mutation.Target) {
		return fmt.Errorf("cookie target is invalid")
	}
	values := request.HeaderValues("Cookie")
	if len(values) != 1 {
		return fmt.Errorf("request must contain exactly one Cookie header")
	}
	parts := strings.Split(values[0], ";")
	index := -1
	current := ""
	for i, part := range parts {
		name, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && name == mutation.Target {
			if index >= 0 {
				return fmt.Errorf("cookie %q occurs more than once", mutation.Target)
			}
			index = i
			current = value
		}
	}
	if index < 0 {
		return fmt.Errorf("cookie %q does not exist", mutation.Target)
	}
	if mutation.From != "" && current != mutation.From {
		return fmt.Errorf("cookie %q does not have the expected value", mutation.Target)
	}
	if strings.ContainsAny(mutation.To, ";") || containsInvalidHeaderValueByte(mutation.To) {
		return fmt.Errorf("cookie value is invalid")
	}
	if mutation.Type == MutationRemoveCookie {
		parts = append(parts[:index], parts[index+1:]...)
	} else {
		leadingLength := len(parts[index]) - len(strings.TrimLeft(parts[index], " \t"))
		parts[index] = parts[index][:leadingLength] + mutation.Target + "=" + mutation.To
	}
	if len(parts) == 0 {
		return request.removeSingleHeader("Cookie")
	}
	return request.replaceSingleHeader("Cookie", strings.Join(parts, ";"))
}

func mutateJSON(request *Request, mutation Mutation) error {
	contentType := strings.ToLower(request.HeaderValue("Content-Type"))
	if !strings.Contains(contentType, "json") {
		return fmt.Errorf("JSON mutation requires a JSON Content-Type")
	}
	decoder := json.NewDecoder(bytes.NewReader(request.Body))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return fmt.Errorf("decode JSON body: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("JSON body contains trailing data")
	}
	segments, err := jsonPathSegments(mutation.Target)
	if err != nil {
		return err
	}
	parent, key, err := jsonParent(root, segments)
	if err != nil {
		return err
	}
	current, exists := parent[key]
	if !exists {
		return fmt.Errorf("JSON field %q does not exist", mutation.Target)
	}
	if mutation.From != "" && comparableJSONValue(current) != mutation.From {
		return fmt.Errorf("JSON field %q does not have the expected value", mutation.Target)
	}
	if mutation.Type == MutationRemoveJSONField {
		delete(parent, key)
	} else {
		value, err := mutationJSONValue(mutation)
		if err != nil {
			return err
		}
		parent[key] = value
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		return fmt.Errorf("encode mutated JSON body: %w", err)
	}
	request.Body = encoded
	return request.updateContentLength()
}

func jsonPathSegments(target string) ([]string, error) {
	if target == "" {
		return nil, fmt.Errorf("JSON field target is required")
	}
	if strings.HasPrefix(target, "/") {
		raw := strings.Split(strings.TrimPrefix(target, "/"), "/")
		for i := range raw {
			raw[i] = strings.ReplaceAll(strings.ReplaceAll(raw[i], "~1", "/"), "~0", "~")
			if raw[i] == "" {
				return nil, fmt.Errorf("JSON pointer contains an empty segment")
			}
		}
		return raw, nil
	}
	segments := strings.Split(target, ".")
	for _, segment := range segments {
		if segment == "" {
			return nil, fmt.Errorf("JSON field path contains an empty segment")
		}
	}
	return segments, nil
}

func jsonParent(root any, segments []string) (map[string]any, string, error) {
	if len(segments) == 0 {
		return nil, "", fmt.Errorf("JSON field path is empty")
	}
	current := root
	for _, segment := range segments[:len(segments)-1] {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("JSON field parent %q is not an object", segment)
		}
		next, exists := object[segment]
		if !exists {
			return nil, "", fmt.Errorf("JSON field parent %q does not exist", segment)
		}
		current = next
	}
	object, ok := current.(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("JSON field parent is not an object")
	}
	return object, segments[len(segments)-1], nil
}

func comparableJSONValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func mutationJSONValue(mutation Mutation) (any, error) {
	format := strings.ToLower(strings.TrimSpace(mutation.Format))
	if format == "" || format == "string" {
		return mutation.To, nil
	}
	if format != "json" {
		return nil, fmt.Errorf("JSON mutation format must be string or json")
	}
	decoder := json.NewDecoder(strings.NewReader(mutation.To))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode JSON mutation value: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("JSON mutation value contains trailing data")
	}
	return value, nil
}

func replaceBody(request *Request, mutation Mutation) error {
	if mutation.From != "" && string(request.Body) != mutation.From {
		return fmt.Errorf("body does not match the expected value")
	}
	request.Body = []byte(mutation.To)
	return request.updateContentLength()
}
