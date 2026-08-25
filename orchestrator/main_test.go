package main

import (
	"os"
	"testing"
)

func TestTenantPatternAndProjectName(t *testing.T) {
	valid := "abcdefghijklmnopqrstuvwxyz"
	if !tenantPattern.MatchString(valid) {
		t.Fatalf("expected %q to be a valid tenant ID", valid)
	}
	if got := projectName(valid); got != "wn-"+valid {
		t.Fatalf("projectName() = %q", got)
	}

	invalid := []string{
		"",
		"ABCDEF234567ABCDEF234567AB",
		"abcdefghijklmnopqrstuvwxy1",
		"../abcdefghijklmnopqrstuvw",
		"abcdefghijklmnopqrstuvwxy",
		"abcdefghijklmnopqrstuvwxyz2",
	}
	for _, value := range invalid {
		if tenantPattern.MatchString(value) {
			t.Fatalf("unexpected valid tenant ID: %q", value)
		}
	}
}

func TestBoundedInt(t *testing.T) {
	t.Setenv("WEBNIX_TEST_INT", "7")
	if got := boundedInt("WEBNIX_TEST_INT", 4, 1, 10); got != 7 {
		t.Fatalf("boundedInt valid = %d, want 7", got)
	}

	t.Setenv("WEBNIX_TEST_INT", "999")
	if got := boundedInt("WEBNIX_TEST_INT", 4, 1, 10); got != 4 {
		t.Fatalf("boundedInt high = %d, want fallback 4", got)
	}

	t.Setenv("WEBNIX_TEST_INT", "invalid")
	if got := boundedInt("WEBNIX_TEST_INT", 4, 1, 10); got != 4 {
		t.Fatalf("boundedInt invalid = %d, want fallback 4", got)
	}
}

func TestLoadConfigRejectsBadIdleByFallingBack(t *testing.T) {
	t.Setenv("WEBNIX_IDLE_TIMEOUT", "not-a-duration")
	cfg := loadConfig()
	if cfg.idleTimeout.String() != "20m0s" {
		t.Fatalf("idle fallback = %s, want 20m0s", cfg.idleTimeout)
	}
}

func TestTenantDenied(t *testing.T) {
	path := t.TempDir() + "/denylist"
	tenant := "abcdefghijklmnopqrstuvwxyz"
	if err := os.WriteFile(path, []byte("# suspended tenants\n"+tenant+" # abuse\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &manager{cfg: config{denylist: path}}
	denied, err := m.tenantDenied(tenant)
	if err != nil || !denied {
		t.Fatalf("tenantDenied() = %v, %v", denied, err)
	}
	denied, err = m.tenantDenied("bcdefghijklmnopqrstuvwxyza")
	if err != nil || denied {
		t.Fatalf("unexpected deny = %v, %v", denied, err)
	}
}
