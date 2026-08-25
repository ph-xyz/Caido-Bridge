package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
)

type ProjectContext struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status,omitempty"`
	Version  string `json:"version,omitempty"`
	ReadOnly bool   `json:"readOnly"`
}

type GetCurrentProjectInput struct{}

type GetCurrentProjectOutput ProjectContext

type ListProjectsInput struct{}

type ProjectSummary struct {
	ProjectContext
	IsCurrent bool `json:"isCurrent"`
}

type ListProjectsOutput struct {
	Projects []ProjectSummary `json:"projects"`
}

func projectContext(project caidoread.Project) ProjectContext {
	return ProjectContext{
		ID:       project.ID,
		Name:     project.Name,
		Status:   project.Status,
		Version:  project.Version,
		ReadOnly: project.ReadOnly,
	}
}

func currentProject(ctx context.Context, reader caidoread.Reader) (caidoread.Project, error) {
	project, err := reader.CurrentProject(ctx)
	if err != nil {
		if errors.Is(err, caidoread.ErrNoCurrentProject) {
			return caidoread.Project{}, fmt.Errorf("no project is selected for the MCP API caller")
		}
		return caidoread.Project{}, fmt.Errorf("read current Caido project: %w", err)
	}
	return project, nil
}

func registerGetCurrentProject(server *mcp.Server, reader caidoread.Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_get_current_project",
		Title:        "Identify the active Caido project",
		Description:  "Read the exact Caido project currently selected for this MCP API caller. Call this before every project-scoped operation.",
		InputSchema:  schemaFor[GetCurrentProjectInput](),
		OutputSchema: schemaFor[GetCurrentProjectOutput](),
		Annotations:  readOnlyAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		_ GetCurrentProjectInput,
	) (*mcp.CallToolResult, GetCurrentProjectOutput, error) {
		project, err := currentProject(ctx, reader)
		if err != nil {
			return nil, GetCurrentProjectOutput{}, err
		}
		return nil, GetCurrentProjectOutput(projectContext(project)), nil
	})
}

func registerListProjects(server *mcp.Server, reader caidoread.Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_list_projects",
		Title:        "List Caido projects",
		Description:  "Read all Caido projects and mark the project currently selected for this MCP API caller.",
		InputSchema:  schemaFor[ListProjectsInput](),
		OutputSchema: schemaFor[ListProjectsOutput](),
		Annotations:  readOnlyAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		_ ListProjectsInput,
	) (*mcp.CallToolResult, ListProjectsOutput, error) {
		projects, err := reader.ListProjects(ctx)
		if err != nil {
			return nil, ListProjectsOutput{}, fmt.Errorf("list Caido projects: %w", err)
		}
		current, err := currentProject(ctx, reader)
		if err != nil {
			return nil, ListProjectsOutput{}, err
		}
		out := ListProjectsOutput{Projects: make([]ProjectSummary, 0, len(projects))}
		for _, project := range projects {
			out.Projects = append(out.Projects, ProjectSummary{
				ProjectContext: projectContext(project),
				IsCurrent:      project.ID == current.ID,
			})
		}
		return nil, out, nil
	})
}
