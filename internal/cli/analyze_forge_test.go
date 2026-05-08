package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
	"github.com/sandgardenhq/find-the-gaps/internal/doctor"
	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

// gitInitWithRemote initializes a fresh git repo at dir, creates an empty
// initial commit, and adds origin = remoteURL. Used to construct fixtures
// for forge.Resolve's same-repo check without touching the network.
func gitInitWithRemote(t *testing.T, dir, remoteURL string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "-c", "user.email=a@b", "-c", "user.name=a", "commit", "--allow-empty", "-q", "-m", "init"},
		{"git", "remote", "add", "origin", remoteURL},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", c, err, out)
		}
	}
}

// fakeAnalyzeServer stands up a fake LLM endpoint that handles every schema
// the analyze pipeline can fire after the forge branch routes around the
// spider. It returns trivial-but-valid JSON for each schema so the run
// completes cleanly.
func fakeAnalyzeServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		s := string(body)

		respond := func(content string) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": content}},
				},
			})
		}

		switch {
		case strings.Contains(s, "synthesize_response"):
			respond(`{"description":"A test product.","features":["feature-one"]}`)
		case strings.Contains(s, "analyze_page_response"):
			respond(`{"summary":"Doc page.","features":["feature-one"],"is_docs":true}`)
		case strings.Contains(s, "screenshot_gaps_response"):
			respond(`{"gaps":[],"suppressed_by_image":[]}`)
		default:
			// Any unmodeled call should fail loudly so the test surfaces a
			// missing dispatch rather than silently returning empty content.
			t.Errorf("unexpected LLM request: %s", s)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// seedForgeFixture pre-caches the code feature, feature map, and docs
// feature map so the analyze run never reaches MapFeaturesToCode/Docs or
// ExtractFeaturesFromCode (all of which are large-tier LLM calls). The
// forge routing test only needs to exercise: spider bypass, page analysis,
// and synthesis.
func seedForgeFixture(t *testing.T, projectDir string) {
	t.Helper()
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codeFeatures := []analyzer.CodeFeature{
		{Name: "feature-one", Description: "f1", Layer: "cli", UserFacing: true},
	}
	scanForCache := &scanner.ProjectScan{Files: []scanner.ScannedFile{{Path: "README.md"}}}
	if err := saveCodeFeaturesCache(filepath.Join(projectDir, "codefeatures.json"), scanForCache, codeFeatures); err != nil {
		t.Fatal(err)
	}
	fmCache := analyzer.FeatureMap{
		{Feature: codeFeatures[0], Files: []string{"README.md"}, Symbols: []string{}},
	}
	if err := saveFeatureMapCache(filepath.Join(projectDir, "featuremap.json"), codeFeatures, fmCache); err != nil {
		t.Fatal(err)
	}
	docsFM := analyzer.DocsFeatureMap{{Feature: "feature-one", Pages: nil}}
	if err := saveDocsFeatureMapCache(
		filepath.Join(projectDir, "docsfeaturemap.json"),
		[]string{"feature-one"}, docsFM,
	); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyze_forgeURL_matchingRepo_skipsCrawl(t *testing.T) {
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "https://github.com/foo/bar.git")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "intro.md"), []byte("# Intro\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repo))
	projectDir := filepath.Join(cacheBase, projectName)
	seedForgeFixture(t, projectDir)

	srv := fakeAnalyzeServer(t)
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	cmd := newAnalyzeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"--repo", repo,
		"--cache-dir", cacheBase,
		"--docs", "https://github.com/foo/bar",
		"--llm-small", "ollama/test-model",
		"--llm-typical", "anthropic/claude-haiku-4-5",
		"--llm-large", "anthropic/claude-haiku-4-5",
		"--no-site",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	combined := out.String()
	if !strings.Contains(combined, "reading markdown from") {
		t.Fatalf("missing on-disk notice in output:\n%s", combined)
	}
	if strings.Contains(combined, "crawling https://github.com/foo/bar") {
		t.Fatalf("spider was invoked despite forge URL:\n%s", combined)
	}
}

func TestAnalyze_forgeURL_noRepoMatch_halts(t *testing.T) {
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "https://github.com/other/proj.git")

	cmd := newAnalyzeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"--repo", repo,
		"--cache-dir", filepath.Join(t.TempDir(), "cache"),
		"--docs", "https://github.com/foo/bar",
		"--llm-small", "ollama/test-model",
		"--llm-typical", "anthropic/claude-haiku-4-5",
		"--llm-large", "anthropic/claude-haiku-4-5",
		"--no-site",
	})

	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	err := cmd.ExecuteContext(context.Background())
	if err == nil {
		t.Fatalf("expected non-nil error from forge mismatch; output=%s", out.String())
	}
	if !strings.Contains(err.Error(), "Find the Gaps can't crawl source-control forges") {
		t.Fatalf("missing forge halt message in error: %v", err)
	}
	if !strings.Contains(err.Error(), "clone the repo locally") {
		t.Fatalf("missing clone-locally hint in error: %v", err)
	}
}

// recordingPrecheck swaps requireExternalTools for a stub that records the
// tools requested per precheck call and rejects any call that asks for
// `mdfetch`. It returns the recorder slice (mutex-protected) and a restore
// func.
func recordingPrecheck(t *testing.T) (*[]doctor.Precheck, func()) {
	t.Helper()
	var mu sync.Mutex
	var seen []doctor.Precheck
	prev := requireExternalTools
	requireExternalTools = func(_ context.Context, p doctor.Precheck) error {
		mu.Lock()
		seen = append(seen, p)
		mu.Unlock()
		if slices.Contains(p.Tools, "mdfetch") {
			return fmt.Errorf("recordingPrecheck: mdfetch should not be required, got tools=%v", p.Tools)
		}
		return nil
	}
	return &seen, func() { requireExternalTools = prev }
}

func TestAnalyze_forgeURL_matchingRepo_doesNotRequireMdfetch(t *testing.T) {
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "https://github.com/foo/bar.git")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "intro.md"), []byte("# Intro\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repo))
	projectDir := filepath.Join(cacheBase, projectName)
	seedForgeFixture(t, projectDir)

	srv := fakeAnalyzeServer(t)
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	seen, restore := recordingPrecheck(t)
	defer restore()

	cmd := newAnalyzeCmd()
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"--repo", repo,
		"--cache-dir", cacheBase,
		"--docs", "https://github.com/foo/bar",
		"--llm-small", "ollama/test-model",
		"--llm-typical", "anthropic/claude-haiku-4-5",
		"--llm-large", "anthropic/claude-haiku-4-5",
		"--no-site",
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute: %v\n%s", err, out.String())
	}

	for _, p := range *seen {
		for _, tool := range p.Tools {
			if tool == "mdfetch" {
				t.Fatalf("mdfetch was requested in precheck %+v despite on-disk mode", p)
			}
		}
	}
}

func TestAnalyze_forgeURL_onDiskCache_secondRunSkipsAnalyze(t *testing.T) {
	// Subtle case from task plan: in on-disk mode docsDir may not exist when
	// LoadIndex runs. Confirm the empty index loads cleanly AND that the
	// per-page analysis cache is honored across re-runs (so the second run
	// makes zero analyze_page LLM calls). This pins the option-(a) decision
	// from the plan: empty-index-on-first-run, cache-keyed by synthesized URL.
	repo := t.TempDir()
	gitInitWithRemote(t, repo, "https://github.com/foo/bar.git")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cacheBase := t.TempDir()
	projectName := filepath.Base(filepath.Clean(repo))
	projectDir := filepath.Join(cacheBase, projectName)
	seedForgeFixture(t, projectDir)

	var analyzeCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		s := string(buf)
		respond := func(content string) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": content}},
				},
			})
		}
		switch {
		case strings.Contains(s, "analyze_page_response"):
			analyzeCalls++
			respond(`{"summary":"Doc page.","features":["feature-one"],"is_docs":true}`)
		case strings.Contains(s, "synthesize_response"):
			respond(`{"description":"P","features":["feature-one"]}`)
		default:
			respond(`{}`)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OLLAMA_BASE_URL", srv.URL)
	t.Setenv("ANTHROPIC_API_KEY", "fake-key")

	args := []string{
		"--repo", repo,
		"--cache-dir", cacheBase,
		"--docs", "https://github.com/foo/bar",
		"--llm-small", "ollama/test-model",
		"--llm-typical", "anthropic/claude-haiku-4-5",
		"--llm-large", "anthropic/claude-haiku-4-5",
		"--no-site",
	}

	// First run: cache miss → AnalyzePage fires.
	cmd1 := newAnalyzeCmd()
	cmd1.SetOut(&bytes.Buffer{})
	cmd1.SetErr(&bytes.Buffer{})
	cmd1.SetArgs(args)
	if err := cmd1.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := analyzeCalls
	if first == 0 {
		t.Fatalf("expected at least one analyze_page call on first run, got 0")
	}

	// Second run: same forge URL + same repo → synthesized URLs identical →
	// idx.Analysis cache hit → no AnalyzePage calls.
	cmd2 := newAnalyzeCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetErr(&bytes.Buffer{})
	cmd2.SetArgs(args)
	if err := cmd2.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if analyzeCalls != first {
		t.Fatalf("re-run should not re-analyze cached pages: first=%d total=%d", first, analyzeCalls)
	}
}
