package main

import (
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestTenantIDIsDeterministicAndOpaque(t *testing.T) {
	got := tenantID("user@example.com")
	if got != tenantID("user@example.com") {
		t.Fatal("tenant ID must be deterministic")
	}
	if got == tenantID("other@example.com") {
		t.Fatal("different principals must not map to the same test tenant ID")
	}
	if len(got) != 26 {
		t.Fatalf("tenant ID length = %d, want 26", len(got))
	}
	if !regexp.MustCompile(`^[a-z2-7]{26}$`).MatchString(got) {
		t.Fatalf("tenant ID %q is outside the accepted alphabet", got)
	}
}

func TestPrincipalFromRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "https://nix.example.com/", nil)
	r.Header.Set("X-Auth-Request-Email", "  User@Example.COM  ")
	principal, ok := principalFromRequest(r, "X-Auth-Request-Email")
	if !ok || principal != "user@example.com" {
		t.Fatalf("principal = %q, %v", principal, ok)
	}

	r.Header.Set("X-Auth-Request-Email", "bad\nidentity")
	if _, ok := principalFromRequest(r, "X-Auth-Request-Email"); ok {
		t.Fatal("principal containing a newline must be rejected")
	}
}

func TestValidateConfig(t *testing.T) {
	staticDir := t.TempDir()
	valid := config{
		listen:             "127.0.0.1:8080",
		orchestratorSocket: "/run/webnix/orchestrator.sock",
		staticDir:          staticDir,
		identityHeader:     "X-Auth-Request-Email",
		publicOrigin:       "https://nix.example.com",
	}
	if err := validateConfig(valid); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}

	invalid := valid
	invalid.publicOrigin = "http://nix.example.com"
	if err := validateConfig(invalid); err == nil {
		t.Fatal("non-local HTTP origin must be rejected")
	}

	invalid = valid
	invalid.publicOrigin = "https://nix.example.com/path"
	if err := validateConfig(invalid); err == nil {
		t.Fatal("origin with path must be rejected")
	}

	invalid = valid
	invalid.identityHeader = "X-Bad Header"
	if err := validateConfig(invalid); err == nil {
		t.Fatal("invalid identity header must be rejected")
	}

	invalid = valid
	invalid.staticDir = staticDir + "/missing"
	if err := validateConfig(invalid); err == nil {
		t.Fatal("missing static directory must be rejected")
	}

	// localhost is intentionally usable over HTTP for local development.
	invalid = valid
	invalid.publicOrigin = "http://localhost:8080"
	if err := validateConfig(invalid); err != nil {
		t.Fatalf("localhost HTTP origin rejected: %v", err)
	}

}
