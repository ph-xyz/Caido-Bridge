// Package config loads and validates the deliberately small server
// configuration surface.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// Config contains the local Caido endpoint and its access token. Token must
// never be logged, serialized, or returned from an MCP tool.
type Config struct {
	URL           string
	Token         string
	ReplayEnabled bool
}

// FromEnv loads configuration from the only two supported environment
// variables. No file-based or command-line token input is supported.
func FromEnv() (Config, error) {
	rawURL := strings.TrimSpace(os.Getenv("CAIDO_URL"))
	if rawURL == "" {
		return Config{}, fmt.Errorf("CAIDO_URL is required")
	}

	caidoURL, err := ValidateLocalURL(rawURL)
	if err != nil {
		return Config{}, fmt.Errorf("invalid CAIDO_URL: %w", err)
	}

	token := os.Getenv("CAIDO_ACCESS_TOKEN")
	if strings.TrimSpace(token) == "" {
		return Config{}, fmt.Errorf("CAIDO_ACCESS_TOKEN is required")
	}

	return Config{
		URL:           caidoURL,
		Token:         token,
		ReplayEnabled: replayEnabled(os.Getenv("CAIDO_ENABLE_REPLAY")),
	}, nil
}

func replayEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// ValidateLocalURL accepts only loopback HTTP(S) endpoints. This makes the
// MCP tools' openWorldHint=false claim enforceable instead of documentary.
func ValidateLocalURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("scheme must be http or https")
	}
	if u.User != nil {
		return "", fmt.Errorf("userinfo is not allowed")
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("host is required")
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("path is not allowed")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("query and fragment are not allowed")
	}

	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	local := host == "localhost"
	if ip := net.ParseIP(host); ip != nil {
		local = ip.IsLoopback()
	}
	if !local {
		return "", fmt.Errorf("host must be loopback (localhost, 127.0.0.0/8, or ::1)")
	}

	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""
	u.ForceQuery = false
	u.Fragment = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}
