package replay

import "testing"

func TestCompareProducesObjectiveJSONAndHeaderDiff(t *testing.T) {
	baseline, err := ParseResponse([]byte(
		"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nETag: a\r\n\r\n" +
			`{"id":1,"name":"alpha","old":true}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	test, err := ParseResponse([]byte(
		"HTTP/1.1 201 Created\r\nContent-Type: application/json\r\nETag: b\r\n\r\n" +
			`{"id":2,"name":"alpha","new":true}`,
	))
	if err != nil {
		t.Fatal(err)
	}
	diff := Compare(baseline, test)
	if !diff.StatusChanged || diff.BaselineStatus != 200 || diff.TestStatus != 201 {
		t.Fatalf("unexpected status diff: %+v", diff)
	}
	if !diff.JSONCompared || !diff.JSONStructureChanged {
		t.Fatalf("JSON diff missing: %+v", diff)
	}
	if len(diff.ChangedJSONFields) != 1 || diff.ChangedJSONFields[0] != "$.id" {
		t.Fatalf("changed fields = %v", diff.ChangedJSONFields)
	}
	if len(diff.AddedJSONFields) != 1 || diff.AddedJSONFields[0] != "$.new" {
		t.Fatalf("added fields = %v", diff.AddedJSONFields)
	}
	if len(diff.RemovedJSONFields) != 1 || diff.RemovedJSONFields[0] != "$.old" {
		t.Fatalf("removed fields = %v", diff.RemovedJSONFields)
	}
	if len(diff.RelevantHeaderChanges) != 1 || diff.RelevantHeaderChanges[0].Name != "ETag" {
		t.Fatalf("header changes = %+v", diff.RelevantHeaderChanges)
	}
}

func TestCompareRejectsJSONWithTrailingData(t *testing.T) {
	baseline, err := ParseResponse([]byte("HTTP/1.1 200 OK\r\n\r\n{\"id\":1} trailing"))
	if err != nil {
		t.Fatal(err)
	}
	test, err := ParseResponse([]byte("HTTP/1.1 200 OK\r\n\r\n{\"id\":2}"))
	if err != nil {
		t.Fatal(err)
	}
	if diff := Compare(baseline, test); diff.JSONCompared {
		t.Fatalf("invalid baseline was compared as JSON: %+v", diff)
	}
}
