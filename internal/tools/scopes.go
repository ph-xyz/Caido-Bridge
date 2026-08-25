package tools

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
)

const maxTargetLength = 2_048

type ListScopesInput struct {
	ProjectID string `json:"projectId" jsonschema:"Required current project ID from caido_get_current_project"`
}

type ScopeSummary struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Allowlist []string `json:"allowlist"`
	Denylist  []string `json:"denylist"`
	Indexed   bool     `json:"indexed"`
}

type ListScopesOutput struct {
	Project ProjectContext `json:"project"`
	Scopes  []ScopeSummary `json:"scopes"`
}

type IsInScopeInput struct {
	ProjectID string `json:"projectId" jsonschema:"Required current project ID from caido_get_current_project"`
	ScopeID   string `json:"scopeId" jsonschema:"Required exact scope ID from caido_list_scopes"`
	Target    string `json:"target" jsonschema:"Host or URL to check against the selected Caido scope"`
}

type ScopeReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type IsInScopeOutput struct {
	Project      ProjectContext  `json:"project"`
	Host         string          `json:"host"`
	InScope      bool            `json:"inScope"`
	MatchedScope *ScopeReference `json:"matchedScope,omitempty"`
	MatchedList  string          `json:"matchedList,omitempty"`
	MatchedRule  string          `json:"matchedRule,omitempty"`
	Reason       string          `json:"reason"`
}

func registerListScopes(server *mcp.Server, reader caidoread.Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_list_scopes",
		Title:        "List Caido scopes",
		Description:  "Read configured scopes only after projectId is a valid existing project and matches the MCP caller's current project. Project identity is included in the response.",
		InputSchema:  schemaFor[ListScopesInput](),
		OutputSchema: schemaFor[ListScopesOutput](),
		Annotations:  readOnlyAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input ListScopesInput,
	) (*mcp.CallToolResult, ListScopesOutput, error) {
		scopes, project, err := guardedProjectRead(
			ctx,
			reader,
			input.ProjectID,
			func() ([]caidoread.Scope, error) {
				return reader.ListScopes(ctx)
			},
		)
		if err != nil {
			return nil, ListScopesOutput{}, fmt.Errorf("list Caido scopes: %w", err)
		}
		out := ListScopesOutput{
			Project: projectContext(project),
			Scopes:  make([]ScopeSummary, 0, len(scopes)),
		}
		for _, scope := range scopes {
			out.Scopes = append(out.Scopes, ScopeSummary{
				ID:        scope.ID,
				Name:      scope.Name,
				Allowlist: scope.Allowlist,
				Denylist:  scope.Denylist,
				Indexed:   scope.Indexed,
			})
		}
		return nil, out, nil
	})
}

func registerIsInScope(server *mcp.Server, reader caidoread.Reader) {
	mcp.AddTool(server, &mcp.Tool{
		Name:         "caido_is_in_scope",
		Title:        "Check a host against Caido scopes",
		Description:  "After projectId is validated, evaluate a host or URL against exactly the scope selected by scopeId. An empty allowlist fails closed. This performs no target request.",
		InputSchema:  schemaFor[IsInScopeInput](),
		OutputSchema: schemaFor[IsInScopeOutput](),
		Annotations:  readOnlyAnnotations(),
	}, func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input IsInScopeInput,
	) (*mcp.CallToolResult, IsInScopeOutput, error) {
		if len(input.Target) > maxTargetLength {
			return nil, IsInScopeOutput{}, fmt.Errorf("target exceeds %d characters", maxTargetLength)
		}
		host, err := parseTargetHost(input.Target)
		if err != nil {
			return nil, IsInScopeOutput{}, err
		}
		scopes, project, err := guardedProjectRead(
			ctx,
			reader,
			input.ProjectID,
			func() ([]caidoread.Scope, error) {
				return reader.ListScopes(ctx)
			},
		)
		if err != nil {
			return nil, IsInScopeOutput{}, fmt.Errorf("list Caido scopes: %w", err)
		}
		scope, err := findScope(scopes, input.ScopeID)
		if err != nil {
			return nil, IsInScopeOutput{}, err
		}
		out := evaluateScope(host, scope)
		out.Project = projectContext(project)
		return nil, out, nil
	})
}

func parseTargetHost(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("target is required")
	}
	if strings.ContainsFunc(raw, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	}) {
		return "", fmt.Errorf("target contains whitespace or control characters")
	}

	var host string
	if strings.Contains(raw, "://") {
		u, err := url.ParseRequestURI(raw)
		if err != nil || u.Hostname() == "" {
			return "", fmt.Errorf("target must be a valid host or URL")
		}
		if u.User != nil {
			return "", fmt.Errorf("target URL userinfo is not allowed")
		}
		host = u.Hostname()
	} else {
		if strings.ContainsAny(raw, "/?#@") {
			return "", fmt.Errorf("bare target must contain only a host and optional port")
		}
		if ip := net.ParseIP(strings.Trim(raw, "[]")); ip != nil {
			host = strings.Trim(raw, "[]")
		} else {
			u, err := url.Parse("http://" + raw)
			if err != nil || u.Hostname() == "" || u.Path != "" {
				return "", fmt.Errorf("target must be a valid host or URL")
			}
			host = u.Hostname()
		}
	}

	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || strings.Contains(host, "*") {
		return "", fmt.Errorf("target must resolve to a concrete host")
	}
	return host, nil
}

// hostMatchesGlob implements Caido-style host globs: '*' matches zero or
// more characters and '?' matches exactly one character. Matching is
// case-insensitive and anchored to the whole hostname.
func hostMatchesGlob(host, pattern string) bool {
	host = strings.ToLower(host)
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	if pattern == "" {
		return false
	}

	hostIndex := 0
	patternIndex := 0
	starIndex := -1
	starHostIndex := 0
	for hostIndex < len(host) {
		switch {
		case patternIndex < len(pattern) &&
			(pattern[patternIndex] == host[hostIndex] || pattern[patternIndex] == '?'):
			patternIndex++
			hostIndex++
		case patternIndex < len(pattern) && pattern[patternIndex] == '*':
			starIndex = patternIndex
			patternIndex++
			starHostIndex = hostIndex
		case starIndex >= 0:
			patternIndex = starIndex + 1
			starHostIndex++
			hostIndex = starHostIndex
		default:
			return false
		}
	}
	for patternIndex < len(pattern) && pattern[patternIndex] == '*' {
		patternIndex++
	}
	return patternIndex == len(pattern)
}

func firstMatch(host string, patterns []string) (string, bool) {
	for _, pattern := range patterns {
		if hostMatchesGlob(host, pattern) {
			return pattern, true
		}
	}
	return "", false
}

func findScope(scopes []caidoread.Scope, scopeID string) (caidoread.Scope, error) {
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" {
		return caidoread.Scope{}, fmt.Errorf("scopeId is required")
	}
	for _, scope := range scopes {
		if scope.ID == scopeID {
			return scope, nil
		}
	}
	return caidoread.Scope{}, fmt.Errorf("scope %q was not found in the current project", scopeID)
}

func evaluateScope(host string, scope caidoread.Scope) IsInScopeOutput {
	ref := &ScopeReference{ID: scope.ID, Name: scope.Name}
	if len(scope.Allowlist) == 0 {
		return IsInScopeOutput{
			Host:         host,
			InScope:      false,
			MatchedScope: ref,
			Reason: fmt.Sprintf(
				"scope %q has an empty allowlist; refusing to treat any host as in scope",
				scope.Name,
			),
		}
	}

	allowRule, allowed := firstMatch(host, scope.Allowlist)
	if !allowed {
		return IsInScopeOutput{
			Host:         host,
			InScope:      false,
			MatchedScope: ref,
			Reason: fmt.Sprintf(
				"host %q does not match the allowlist for scope %q",
				host,
				scope.Name,
			),
		}
	}

	if denyRule, denied := firstMatch(host, scope.Denylist); denied {
		return IsInScopeOutput{
			Host:         host,
			InScope:      false,
			MatchedScope: ref,
			MatchedList:  "denylist",
			MatchedRule:  denyRule,
			Reason: fmt.Sprintf(
				"host %q matches scope %q but is excluded by denylist rule %q",
				host,
				scope.Name,
				denyRule,
			),
		}
	}

	return IsInScopeOutput{
		Host:         host,
		InScope:      true,
		MatchedScope: ref,
		MatchedList:  "allowlist",
		MatchedRule:  allowRule,
		Reason: fmt.Sprintf(
			"host %q matches allowlist rule %q in scope %q and no denylist rule",
			host,
			allowRule,
			scope.Name,
		),
	}
}
