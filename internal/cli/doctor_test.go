package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_Doctor_AllPresent_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	writeFakeBin(t, dir, "hugo", "hugo v0.154.5+extended darwin/arm64")
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor"})
	if code != 0 {
		t.Errorf("code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "mdfetch 1.2.3") {
		t.Errorf("stdout missing mdfetch version; got %q", stdout.String())
	}
}

func TestRun_Doctor_Missing_ReturnsOne(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor"})
	if code != 1 {
		t.Errorf("code = %d, want 1; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "mdfetch") {
		t.Errorf("stderr should mention mdfetch, got %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "Error:") {
		t.Errorf("ExitCodeError should not print 'Error:' preamble, got %q", stderr.String())
	}
}

func TestDoctor_PrintsResolvedCapabilitiesPerTier(t *testing.T) {
	// All required external tools present so doctor exits 0 and we can
	// inspect the tier capability lines printed after the standard checks.
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	writeFakeBin(t, dir, "hugo", "hugo v0.154.5+extended darwin/arm64")
	t.Setenv("PATH", dir)
	// Defeat any pre-existing tier env vars so defaults are used.
	t.Setenv("FIND_THE_GAPS_LLM_SMALL", "")
	t.Setenv("FIND_THE_GAPS_LLM_TYPICAL", "")
	t.Setenv("FIND_THE_GAPS_LLM_LARGE", "")
	// Pin the Anthropic-key path so tierFallbacks() returns the Anthropic
	// defaults regardless of the test runner's ambient environment.
	t.Setenv("ANTHROPIC_API_KEY", "fake")
	t.Setenv("OPENAI_API_KEY", "")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor"})
	if code != 0 {
		t.Fatalf("code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	got := stdout.String()
	wantLines := []string{
		"small: " + defaultSmallTier + " (tool_use=true vision=true)",
		"typical: " + defaultTypicalTier + " (tool_use=true vision=true)",
		"large: " + defaultLargeTier + " (tool_use=true vision=true)",
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Errorf("stdout missing tier capability line %q; got %q", want, got)
		}
	}
}

// TestDoctor_PrintsOpenAIDefaultsWhenOnlyOpenAIKeySet pins the contract that
// `ftg doctor` (with no --llm-* flags) reports the same tier defaults the
// next `ftg analyze` would resolve to. tierFallbacks() flips to the OpenAI
// lineup when only OPENAI_API_KEY is set; doctor must mirror that or it
// misleads OpenAI-only users.
func TestDoctor_PrintsOpenAIDefaultsWhenOnlyOpenAIKeySet(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	writeFakeBin(t, dir, "hugo", "hugo v0.154.5+extended darwin/arm64")
	t.Setenv("PATH", dir)
	t.Setenv("FIND_THE_GAPS_LLM_SMALL", "")
	t.Setenv("FIND_THE_GAPS_LLM_TYPICAL", "")
	t.Setenv("FIND_THE_GAPS_LLM_LARGE", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "fake")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{"doctor"})
	if code != 0 {
		t.Fatalf("code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	got := stdout.String()
	for _, want := range []string{
		"small: " + defaultSmallTierOpenAI,
		"typical: " + defaultTypicalTierOpenAI,
		"large: " + defaultLargeTierOpenAI,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stdout missing OpenAI default %q; got %q", want, got)
		}
	}
	// Must not show the Anthropic defaults when only OPENAI_API_KEY is set.
	for _, unwanted := range []string{
		"small: " + defaultSmallTier,
		"typical: " + defaultTypicalTier,
		"large: " + defaultLargeTier,
	} {
		if strings.Contains(got, unwanted) {
			t.Errorf("stdout must not show Anthropic default %q when only OPENAI_API_KEY is set; got %q", unwanted, got)
		}
	}
}

func TestDoctor_PrintsResolvedCapabilitiesFromFlags(t *testing.T) {
	dir := t.TempDir()
	writeFakeBin(t, dir, "mdfetch", "mdfetch 1.2.3")
	writeFakeBin(t, dir, "hugo", "hugo v0.154.5+extended darwin/arm64")
	t.Setenv("PATH", dir)
	t.Setenv("FIND_THE_GAPS_LLM_SMALL", "")
	t.Setenv("FIND_THE_GAPS_LLM_TYPICAL", "")
	t.Setenv("FIND_THE_GAPS_LLM_LARGE", "")

	var stdout, stderr bytes.Buffer
	code := run(&stdout, &stderr, []string{
		"doctor",
		"--llm-small", "openai/gpt-5-mini",
		"--llm-typical", "openai/gpt-5",
		"--llm-large", "anthropic/claude-opus-4-7",
	})
	if code != 0 {
		t.Fatalf("code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	got := stdout.String()
	wantLines := []string{
		"small: openai/gpt-5-mini (tool_use=true vision=true)",
		"typical: openai/gpt-5 (tool_use=true vision=true)",
		"large: anthropic/claude-opus-4-7 (tool_use=true vision=true)",
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Errorf("stdout missing tier capability line %q; got %q", want, got)
		}
	}
}

func TestPrintTierCapabilities_InvalidTierString(t *testing.T) {
	// Pin tierFallbacks() to the Anthropic path so the defaulted tiers
	// render predictably.
	t.Setenv("ANTHROPIC_API_KEY", "fake")
	t.Setenv("OPENAI_API_KEY", "")

	var buf bytes.Buffer
	// "openai/" is parseable as provider+missing-model and triggers the
	// parseTierString error branch.
	printTierCapabilities(&buf, "openai/", "", "")
	got := buf.String()
	if !strings.Contains(got, "small: openai/ (invalid:") {
		t.Errorf("stdout should report invalid tier; got %q", got)
	}
	// The other tiers fall back to defaults and should still render.
	if !strings.Contains(got, "typical: "+defaultTypicalTier+" (tool_use=true vision=true)") {
		t.Errorf("stdout should still render defaulted typical tier; got %q", got)
	}
}

func TestPrintTierCapabilities_UnknownProvider(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "fake")
	t.Setenv("OPENAI_API_KEY", "")

	var buf bytes.Buffer
	printTierCapabilities(&buf, "bogus/some-model", "", "")
	got := buf.String()
	if !strings.Contains(got, "small: bogus/some-model (unknown provider)") {
		t.Errorf("stdout should report unknown provider; got %q", got)
	}
}

func writeFakeBin(t *testing.T, dir, name, versionLine string) {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\necho \"" + versionLine + "\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
