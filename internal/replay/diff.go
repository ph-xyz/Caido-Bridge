package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

type HeaderChange struct {
	Name     string
	Baseline string
	Test     string
}

type Diff struct {
	StatusChanged         bool
	BaselineStatus        int
	TestStatus            int
	SizeDelta             int
	ContentTypeChanged    bool
	BaselineContentType   string
	TestContentType       string
	RedirectChanged       bool
	BaselineRedirect      string
	TestRedirect          string
	RelevantHeaderChanges []HeaderChange
	BodyEqual             bool
	BaselineBodySHA256    string
	TestBodySHA256        string
	JSONCompared          bool
	JSONStructureChanged  bool
	ChangedJSONFields     []string
	AddedJSONFields       []string
	RemovedJSONFields     []string
}

func Compare(baseline, test *Response) Diff {
	diff := Diff{
		BaselineStatus:      baseline.StatusCode,
		TestStatus:          test.StatusCode,
		StatusChanged:       baseline.StatusCode != test.StatusCode,
		SizeDelta:           len(test.Body) - len(baseline.Body),
		BaselineContentType: baseline.HeaderValue("Content-Type"),
		TestContentType:     test.HeaderValue("Content-Type"),
		BaselineRedirect:    baseline.HeaderValue("Location"),
		TestRedirect:        test.HeaderValue("Location"),
		BodyEqual:           bytes.Equal(baseline.Body, test.Body),
		BaselineBodySHA256:  SHA256(baseline.Body),
		TestBodySHA256:      SHA256(test.Body),
	}
	diff.ContentTypeChanged = diff.BaselineContentType != diff.TestContentType
	diff.RedirectChanged = diff.BaselineRedirect != diff.TestRedirect
	diff.RelevantHeaderChanges = compareRelevantHeaders(baseline, test)

	baseJSON, baseOK := decodeJSON(baseline.Body)
	testJSON, testOK := decodeJSON(test.Body)
	if !baseOK || !testOK {
		return diff
	}
	diff.JSONCompared = true
	baseFields := map[string]jsonField{}
	testFields := map[string]jsonField{}
	flattenJSON("$", baseJSON, baseFields)
	flattenJSON("$", testJSON, testFields)
	for path, baseField := range baseFields {
		testField, exists := testFields[path]
		if !exists {
			diff.RemovedJSONFields = append(diff.RemovedJSONFields, path)
			continue
		}
		if baseField.Type != testField.Type {
			diff.JSONStructureChanged = true
			diff.ChangedJSONFields = append(diff.ChangedJSONFields, path)
			continue
		}
		if baseField.Value != testField.Value {
			diff.ChangedJSONFields = append(diff.ChangedJSONFields, path)
		}
	}
	for path := range testFields {
		if _, exists := baseFields[path]; !exists {
			diff.AddedJSONFields = append(diff.AddedJSONFields, path)
		}
	}
	if len(diff.AddedJSONFields) != 0 || len(diff.RemovedJSONFields) != 0 {
		diff.JSONStructureChanged = true
	}
	sort.Strings(diff.ChangedJSONFields)
	sort.Strings(diff.AddedJSONFields)
	sort.Strings(diff.RemovedJSONFields)
	return diff
}

func compareRelevantHeaders(baseline, test *Response) []HeaderChange {
	names := []string{
		"Cache-Control",
		"Content-Encoding",
		"ETag",
		"Last-Modified",
		"Location",
		"Retry-After",
		"Server",
		"WWW-Authenticate",
		"X-Powered-By",
	}
	var changes []HeaderChange
	for _, name := range names {
		baseValue := baseline.HeaderValue(name)
		testValue := test.HeaderValue(name)
		if baseValue != testValue {
			changes = append(changes, HeaderChange{
				Name:     name,
				Baseline: baseValue,
				Test:     testValue,
			})
		}
	}
	return changes
}

type jsonField struct {
	Type  string
	Value string
}

func decodeJSON(body []byte) (any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, false
	}
	return value, true
}

func flattenJSON(path string, value any, fields map[string]jsonField) {
	switch typed := value.(type) {
	case map[string]any:
		fields[path] = jsonField{Type: "object"}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			flattenJSON(path+"."+escapeJSONPathKey(key), typed[key], fields)
		}
	case []any:
		fields[path] = jsonField{Type: "array", Value: fmt.Sprintf("length:%d", len(typed))}
		for index, item := range typed {
			flattenJSON(fmt.Sprintf("%s[%d]", path, index), item, fields)
		}
	case nil:
		fields[path] = jsonField{Type: "null", Value: "null"}
	case string:
		fields[path] = jsonField{Type: "string", Value: typed}
	case json.Number:
		fields[path] = jsonField{Type: "number", Value: typed.String()}
	case bool:
		fields[path] = jsonField{Type: "boolean", Value: fmt.Sprintf("%t", typed)}
	default:
		encoded, _ := json.Marshal(typed)
		fields[path] = jsonField{Type: fmt.Sprintf("%T", typed), Value: string(encoded)}
	}
}

func escapeJSONPathKey(key string) string {
	if strings.ContainsAny(key, ".[]") {
		encoded, _ := json.Marshal(key)
		return "[" + string(encoded) + "]"
	}
	return key
}
