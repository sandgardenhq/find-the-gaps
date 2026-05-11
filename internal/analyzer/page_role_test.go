package analyzer

import "testing"

func TestRoleResolver_KnownURL_ReturnsRole(t *testing.T) {
	r := NewRoleResolver(map[string]string{
		"https://docs/x": "quickstart",
		"https://docs/y": "reference",
	})
	if got := r("https://docs/x"); got != "quickstart" {
		t.Errorf("r(x) = %q, want quickstart", got)
	}
	if got := r("https://docs/y"); got != "reference" {
		t.Errorf("r(y) = %q, want reference", got)
	}
}

func TestRoleResolver_UnknownURL_ReturnsOther(t *testing.T) {
	r := NewRoleResolver(map[string]string{"https://docs/x": "quickstart"})
	if got := r("https://docs/missing"); got != "other" {
		t.Errorf("unknown url = %q, want other", got)
	}
}

func TestRoleResolver_EmptyURL_ReturnsOther(t *testing.T) {
	r := NewRoleResolver(map[string]string{})
	if got := r(""); got != "other" {
		t.Errorf("empty url = %q, want other", got)
	}
}

func TestRoleResolver_EmptyStoredRole_ReturnsOther(t *testing.T) {
	// AnalyzePage skipped (token budget) → zero-value Role = "".
	// Resolver normalizes empty to "other".
	r := NewRoleResolver(map[string]string{"https://docs/skipped": ""})
	if got := r("https://docs/skipped"); got != "other" {
		t.Errorf("empty role = %q, want other", got)
	}
}

func TestRoleResolver_NilMap_ReturnsOther(t *testing.T) {
	r := NewRoleResolver(nil)
	if got := r("https://docs/any"); got != "other" {
		t.Errorf("nil map = %q, want other", got)
	}
}
