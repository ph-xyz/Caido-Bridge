package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
)

type sequenceReader struct {
	fakeReader
	current      []caidoread.Project
	available    []caidoread.Project
	currentCalls int
	listCalls    int
}

func (r *sequenceReader) CurrentProject(context.Context) (caidoread.Project, error) {
	index := r.currentCalls
	r.currentCalls++
	if index >= len(r.current) {
		index = len(r.current) - 1
	}
	return r.current[index], nil
}

func (r *sequenceReader) ListProjects(context.Context) ([]caidoread.Project, error) {
	r.listCalls++
	return r.available, nil
}

func project(id, name string) caidoread.Project {
	return caidoread.Project{ID: id, Name: name}
}

func TestGuardedProjectReadAllowsStableExpectedProject(t *testing.T) {
	target := project(testProjectID, testProjectName)
	reader := &sequenceReader{
		current:   []caidoread.Project{target, target},
		available: []caidoread.Project{target},
	}
	readCalled := false
	value, origin, err := guardedProjectRead(
		context.Background(),
		reader,
		testProjectID,
		func() (string, error) {
			readCalled = true
			return "safe-result", nil
		},
	)
	if err != nil || !readCalled || value != "safe-result" || origin.ID != testProjectID {
		t.Fatalf("unexpected guarded result: value=%q project=%+v called=%v err=%v", value, origin, readCalled, err)
	}
}

func TestGuardedProjectReadBlocksCrossProjectRead(t *testing.T) {
	target := project(testProjectID, testProjectName)
	other := project(otherProjectID, otherProjectName)
	reader := &sequenceReader{
		current:   []caidoread.Project{other},
		available: []caidoread.Project{target, other},
	}
	readCalled := false
	_, _, err := guardedProjectRead(
		context.Background(),
		reader,
		testProjectID,
		func() (string, error) {
			readCalled = true
			return "unsafe", nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "project mismatch") || readCalled {
		t.Fatalf("cross-project read was not blocked: called=%v err=%v", readCalled, err)
	}
}

func TestGuardedProjectReadDiscardsResultWhenProjectChanges(t *testing.T) {
	target := project(testProjectID, testProjectName)
	other := project(otherProjectID, otherProjectName)
	reader := &sequenceReader{
		current:   []caidoread.Project{target, other},
		available: []caidoread.Project{target, other},
	}
	value, _, err := guardedProjectRead(
		context.Background(),
		reader,
		testProjectID,
		func() (string, error) { return "must-not-escape", nil },
	)
	if err == nil || !strings.Contains(err.Error(), "changed during the read") || value != "" {
		t.Fatalf("changed-project result was not discarded: value=%q err=%v", value, err)
	}
}

func TestGuardedProjectReadRejectsInvalidUUIDBeforeReaderCalls(t *testing.T) {
	reader := &sequenceReader{}
	readCalled := false
	_, _, err := guardedProjectRead(
		context.Background(),
		reader,
		"not-a-uuid",
		func() (string, error) {
			readCalled = true
			return "unsafe", nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "canonical UUID") {
		t.Fatalf("invalid UUID error = %v", err)
	}
	if reader.listCalls != 0 || reader.currentCalls != 0 || readCalled {
		t.Fatalf("invalid UUID reached reader: list=%d current=%d read=%v", reader.listCalls, reader.currentCalls, readCalled)
	}
}

func TestGuardedProjectReadRejectsNonexistentProject(t *testing.T) {
	target := project(testProjectID, testProjectName)
	reader := &sequenceReader{
		current:   []caidoread.Project{target},
		available: []caidoread.Project{target},
	}
	readCalled := false
	_, _, err := guardedProjectRead(
		context.Background(),
		reader,
		missingProjectID,
		func() (string, error) {
			readCalled = true
			return "unsafe", nil
		},
	)
	if err == nil || !strings.Contains(err.Error(), "does not exist") || readCalled {
		t.Fatalf("nonexistent project was not rejected: called=%v err=%v", readCalled, err)
	}
	if reader.currentCalls != 0 {
		t.Fatalf("nonexistent project reached current-project read: %d", reader.currentCalls)
	}
}
