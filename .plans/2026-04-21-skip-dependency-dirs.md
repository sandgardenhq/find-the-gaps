# Skip Dependency Directories Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Skip well-known language-specific dependency and build-artifact directories during repo walks so they are never scanned.

**Architecture:** Add a hardcoded `skippedDirs` map in `walker.go`; check `info.Name()` against it and return `filepath.SkipDir` for matches. No new abstractions needed — one map, one if-statement.

**Tech Stack:** Go stdlib (`path/filepath`, `os`)

---

## Context

`find-the-gaps` walks a repo via `internal/scanner/walker.go:Walk()`. The walker currently skips `.git/`, hidden dirs (names starting with `.`), and `.gitignore`-listed paths. It does not hardcode skips for well-known dependency/build-artifact directories that should never be scanned even when `.gitignore` omits them.

The check uses `info.Name()` (the leaf directory name), so it matches at any nesting depth automatically. No path-prefix logic is needed.

Directories to skip unconditionally:

| Name | Language |
|------|----------|
| `vendor` | Go |
| `node_modules` | JavaScript / TypeScript |
| `__pycache__` | Python bytecode cache |
| `venv` | Python virtual environments |
| `target` | Rust build artifacts |

---

### Task 1: Write failing tests

**Files:**
- Modify: `internal/scanner/walker_test.go`

**Step 1: Add the two new test functions**

Open `internal/scanner/walker_test.go`. Add both functions below the existing `TestWalk_gitignoreMatchedDir_skipped` test and before the `writeFile` helper (currently line 221). No new imports are needed — `os`, `path/filepath`, and `strings` are already present.

```go
func TestWalk_skipsKnownDependencyDirs(t *testing.T) {
	cases := []struct {
		name    string
		dirName string
	}{
		{"go vendor",      "vendor"},
		{"node_modules",   "node_modules"},
		{"python pycache", "__pycache__"},
		{"rust target",    "target"},
		{"python venv",    "venv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, tc.dirName), 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, dir, filepath.Join(tc.dirName, "source.go"), "")
			writeFile(t, dir, "main.go", "")

			var found []string
			if err := Walk(dir, func(path string, _ os.FileInfo) error {
				found = append(found, path)
				return nil
			}); err != nil {
				t.Fatalf("Walk: %v", err)
			}
			for _, f := range found {
				if strings.HasPrefix(f, tc.dirName+string(filepath.Separator)) || f == tc.dirName {
					t.Errorf("dependency dir %q should be skipped, found %q", tc.dirName, f)
				}
			}
			var hasMain bool
			for _, f := range found {
				if f == "main.go" {
					hasMain = true
				}
			}
			if !hasMain {
				t.Errorf("main.go should be found but was not; got: %v", found)
			}
		})
	}
}

func TestWalk_skipsNestedDependencyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "pkg/node_modules/lib.js", "")
	writeFile(t, dir, "pkg/index.ts", "")

	var found []string
	if err := Walk(dir, func(path string, _ os.FileInfo) error {
		found = append(found, path)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range found {
		if strings.Contains(f, "node_modules") {
			t.Errorf("nested node_modules should be skipped, found %q", f)
		}
	}
	var hasIndex bool
	for _, f := range found {
		if f == filepath.Join("pkg", "index.ts") {
			hasIndex = true
		}
	}
	if !hasIndex {
		t.Errorf("pkg/index.ts should be found but was not; got: %v", found)
	}
}
```

**Step 2: Run the tests to verify RED**

```bash
go test ./internal/scanner/ -run 'TestWalk_skipsKnownDependencyDirs|TestWalk_skipsNestedDependencyDir' -v
```

Expected: both tests FAIL. The subtests for `vendor`, `node_modules`, `__pycache__`, `target`, and `venv` will each report that `<dirName>/source.go` was found when it should have been skipped.

Note: `TestWalk_gitignoreMatchedDir_skipped` (existing test) covers `vendor/` skipped via `.gitignore` — it will continue to pass throughout. The new tests cover the no-`.gitignore` case.

**Step 3: Commit the failing tests**

```bash
git add internal/scanner/walker_test.go
git commit -m "$(cat <<'EOF'
test(scanner): add failing tests for dependency dir exclusion

- RED: TestWalk_skipsKnownDependencyDirs (vendor, node_modules, __pycache__, target, venv)
- RED: TestWalk_skipsNestedDependencyDir (pkg/node_modules)

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

### Task 2: Implement the skip

**Files:**
- Modify: `internal/scanner/walker.go`

**Step 1: Add the `skippedDirs` map**

In `internal/scanner/walker.go`, add the following package-level variable immediately before the `Walk` function declaration (after the imports block). No new imports are required.

```go
// skippedDirs lists well-known dependency and build-artifact directories that
// are never useful to scan, regardless of .gitignore configuration.
// Keys are directory base-names (info.Name()), matched at any depth.
var skippedDirs = map[string]bool{
	"vendor":       true, // Go
	"node_modules": true, // JavaScript / TypeScript
	"__pycache__":  true, // Python
	"venv":         true, // Python virtual environments
	"target":       true, // Rust
}
```

**Step 2: Add the check in the Walk closure**

In the `Walk` closure, add the dependency-dir check immediately after the hidden-dir check and before the gitignore check. The exact location is after this existing block:

```go
// Skip hidden directories (but allow hidden files in root, like .gitignore itself).
if info.IsDir() && strings.HasPrefix(info.Name(), ".") {
    return filepath.SkipDir
}
```

Insert:

```go
// Skip well-known dependency and build-artifact directories.
if info.IsDir() && skippedDirs[info.Name()] {
    return filepath.SkipDir
}
```

After the change the guard-clause order in the closure is:
1. Error propagation
2. Rel computation and root skip
3. Skip `.git` (by rel path)
4. Skip hidden dirs (name starts with `.`)
5. **Skip known dependency dirs (name in map) ← NEW**
6. Skip gitignored paths
7. Skip non-leaf dirs (return nil to continue descent)
8. Call `fn`

**Step 3: Run the new tests to verify GREEN**

```bash
go test ./internal/scanner/ -run 'TestWalk_skipsKnownDependencyDirs|TestWalk_skipsNestedDependencyDir' -v
```

Expected: all subtests PASS.

**Step 4: Run the full scanner package to confirm no regressions**

```bash
go test ./internal/scanner/ -v
```

Expected: all tests PASS (including pre-existing gitignore, hidden-dir, and nested-dir tests).

**Step 5: Run the full suite and check coverage**

```bash
go test -cover ./...
```

Expected: `internal/scanner` coverage stays at or above 94% (currently 94.2%). All packages green.

**Step 6: Run the linter**

```bash
golangci-lint run
```

Expected: 0 issues.

**Step 7: Commit**

```bash
git add internal/scanner/walker.go
git commit -m "$(cat <<'EOF'
feat(scanner): skip well-known dependency and build-artifact dirs

- RED: tests written first (Task 1 commit)
- GREEN: skippedDirs map + filepath.SkipDir check in Walk closure
- Skips: vendor (Go), node_modules (JS/TS), __pycache__ (Python),
         venv (Python), target (Rust) — at any nesting depth
- Status: all tests passing, lint clean

Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
EOF
)"
```

---

## Design Notes

- **Why `info.Name()` not `rel`?** The leaf-name check fires at any depth with zero path-prefix logic. `rel` would require `filepath.Base` or prefix matching.
- **Why a `map[string]bool` not a slice?** O(1) lookup; inline comments document the language for each entry.
- **Why hardcode instead of config?** These directories are universal noise — they should never appear in gap analysis output.
- **`vendor/` + existing gitignore test:** `TestWalk_gitignoreMatchedDir_skipped` creates a `.gitignore` listing `vendor/` and verifies it is skipped. After this change, `vendor/` is skipped by the new hardcoded check *before* the gitignore check evaluates — the test still passes because the observable result is the same.
- **`target/` caveat:** A non-Rust directory named `target/` at any depth would also be skipped. This is an acceptable trade-off; such naming collisions are rare, and the directory can be renamed if needed.
