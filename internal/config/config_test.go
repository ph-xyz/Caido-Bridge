package config

import "testing"

func TestValidateLocalURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "localhost", input: "http://localhost:8080/", want: "http://localhost:8080"},
		{name: "IPv4 loopback", input: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "IPv4 loopback range", input: "https://127.20.30.40:8443", want: "https://127.20.30.40:8443"},
		{name: "IPv6 loopback", input: "http://[::1]:8080", want: "http://[::1]:8080"},
		{name: "external host", input: "https://example.com", wantErr: true},
		{name: "private but not loopback", input: "http://192.168.1.10:8080", wantErr: true},
		{name: "userinfo", input: "http://user:pass@localhost:8080", wantErr: true},
		{name: "path", input: "http://localhost:8080/graphql", wantErr: true},
		{name: "query", input: "http://localhost:8080/?x=1", wantErr: true},
		{name: "wrong scheme", input: "file://localhost/tmp", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ValidateLocalURL(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ValidateLocalURL(%q) succeeded; want error", test.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateLocalURL(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("ValidateLocalURL(%q) = %q; want %q", test.input, got, test.want)
			}
		})
	}
}

func TestFromEnvNeverEmbedsTokenInError(t *testing.T) {
	t.Setenv("CAIDO_URL", "https://example.com")
	t.Setenv("CAIDO_ACCESS_TOKEN", "TOP-SECRET-TOKEN")
	_, err := FromEnv()
	if err == nil {
		t.Fatal("FromEnv succeeded; want error")
	}
	if got := err.Error(); got == "" || contains(got, "TOP-SECRET-TOKEN") {
		t.Fatalf("unsafe error: %q", got)
	}
}

func TestReplayRequiresExplicitTruthyOptIn(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "", want: false},
		{value: "0", want: false},
		{value: "false", want: false},
		{value: "1", want: true},
		{value: " true ", want: true},
		{value: "ON", want: true},
	}
	for _, test := range tests {
		if got := replayEnabled(test.value); got != test.want {
			t.Errorf("replayEnabled(%q) = %t; want %t", test.value, got, test.want)
		}
	}
}

func contains(value, substring string) bool {
	for i := 0; i+len(substring) <= len(value); i++ {
		if value[i:i+len(substring)] == substring {
			return true
		}
	}
	return false
}
