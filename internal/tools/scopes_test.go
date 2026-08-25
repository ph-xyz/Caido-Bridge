package tools

import (
	"testing"

	"github.com/ph-xyz/Caido-Bridge/internal/caidoread"
)

func TestHostMatchesGlob(t *testing.T) {
	tests := []struct {
		host    string
		pattern string
		want    bool
	}{
		{"example.com", "example.com", true},
		{"EXAMPLE.COM", "example.com", true},
		{"api.example.com", "*.example.com", true},
		{"a.b.example.com", "*.example.com", true},
		{"example.com", "*.example.com", false},
		{"evil-example.com", "*.example.com", false},
		{"api1.example.com", "api?.example.com", true},
		{"api12.example.com", "api?.example.com", false},
		{"anything.test", "*", true},
		{"example.com.evil.test", "example.com", false},
		{"example.com", "", false},
	}
	for _, test := range tests {
		if got := hostMatchesGlob(test.host, test.pattern); got != test.want {
			t.Errorf("hostMatchesGlob(%q, %q) = %v; want %v", test.host, test.pattern, got, test.want)
		}
	}
}

func TestParseTargetHost(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"example.com", "example.com", false},
		{"example.com:8443", "example.com", false},
		{"https://API.Example.com:8443/path?q=1", "api.example.com", false},
		{"[::1]:8080", "::1", false},
		{"", "", true},
		{"example.com/path", "", true},
		{"https://user:pass@example.com", "", true},
		{"*.example.com", "", true},
	}
	for _, test := range tests {
		got, err := parseTargetHost(test.input)
		if test.wantErr {
			if err == nil {
				t.Errorf("parseTargetHost(%q) succeeded; want error", test.input)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("parseTargetHost(%q) = %q, %v; want %q", test.input, got, err, test.want)
		}
	}
}

func TestEvaluateScopeAllowAndDeny(t *testing.T) {
	scope := caidoread.Scope{
		ID:        "scope-1",
		Name:      "Production",
		Allowlist: []string{"*.example.com"},
		Denylist:  []string{"admin.example.com"},
	}

	allowed := evaluateScope("api.example.com", scope)
	if !allowed.InScope || allowed.MatchedList != "allowlist" || allowed.MatchedRule != "*.example.com" {
		t.Fatalf("unexpected allow decision: %+v", allowed)
	}

	denied := evaluateScope("admin.example.com", scope)
	if denied.InScope || denied.MatchedList != "denylist" || denied.MatchedRule != "admin.example.com" {
		t.Fatalf("unexpected deny decision: %+v", denied)
	}

	outside := evaluateScope("other.test", scope)
	if outside.InScope || outside.MatchedScope == nil || outside.MatchedScope.ID != scope.ID {
		t.Fatalf("unexpected outside decision: %+v", outside)
	}
}

func TestEvaluateScopeEmptyAllowlistFailsClosed(t *testing.T) {
	decision := evaluateScope("anything.test", caidoread.Scope{ID: "closed", Name: "Closed"})
	if decision.InScope || decision.MatchedScope == nil || decision.MatchedScope.ID != "closed" {
		t.Fatalf("empty allowlist must fail closed: %+v", decision)
	}
}

func TestFindScopeRequiresExactSelection(t *testing.T) {
	scopes := []caidoread.Scope{
		{ID: "one", Name: "Denied", Allowlist: []string{"*.example.com"}, Denylist: []string{"api.example.com"}},
		{ID: "two", Name: "Allowed elsewhere", Allowlist: []string{"api.example.com"}},
	}
	selected, err := findScope(scopes, "one")
	if err != nil {
		t.Fatal(err)
	}
	decision := evaluateScope("api.example.com", selected)
	if decision.InScope || decision.MatchedScope == nil || decision.MatchedScope.ID != "one" {
		t.Fatalf("another preset must not override the selected scope: %+v", decision)
	}
	if _, err := findScope(scopes, "missing"); err == nil {
		t.Fatal("missing scope selection succeeded")
	}
	if _, err := findScope(scopes, " "); err == nil {
		t.Fatal("empty scope selection succeeded")
	}
}
