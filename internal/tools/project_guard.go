package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
)

func normalizeProjectID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", fmt.Errorf("projectId is required; call caido_get_current_project first")
	}
	parsed, err := uuid.Parse(id)
	if err != nil || !strings.EqualFold(id, parsed.String()) {
		return "", fmt.Errorf("projectId must be a canonical UUID")
	}
	return parsed.String(), nil
}

func requireExistingProject(
	ctx context.Context,
	reader caidoread.Reader,
	projectID string,
) (caidoread.Project, error) {
	projects, err := reader.ListProjects(ctx)
	if err != nil {
		return caidoread.Project{}, fmt.Errorf("list Caido projects: %w", err)
	}
	for _, project := range projects {
		if project.ID == projectID {
			return project, nil
		}
	}
	return caidoread.Project{}, fmt.Errorf("project %q does not exist", projectID)
}

func guardedProjectRead[T any](
	ctx context.Context,
	reader caidoread.Reader,
	expectedProjectID string,
	read func() (T, error),
) (T, caidoread.Project, error) {
	var zero T
	expectedProjectID, err := normalizeProjectID(expectedProjectID)
	if err != nil {
		return zero, caidoread.Project{}, err
	}
	expectedProject, err := requireExistingProject(ctx, reader, expectedProjectID)
	if err != nil {
		return zero, caidoread.Project{}, err
	}

	before, err := currentProject(ctx, reader)
	if err != nil {
		return zero, caidoread.Project{}, err
	}
	if before.ID != expectedProjectID {
		return zero, caidoread.Project{}, fmt.Errorf(
			"project mismatch: requested %q (%s), but MCP is on %q (%s); result blocked",
			expectedProjectID,
			expectedProject.Name,
			before.ID,
			before.Name,
		)
	}

	value, err := read()
	if err != nil {
		return zero, caidoread.Project{}, err
	}
	after, err := currentProject(ctx, reader)
	if err != nil {
		return zero, caidoread.Project{}, err
	}
	if after.ID != before.ID {
		return zero, caidoread.Project{}, fmt.Errorf(
			"active project changed during the read from %q (%s) to %q (%s); result discarded",
			before.ID,
			before.Name,
			after.ID,
			after.Name,
		)
	}
	return value, after, nil
}
