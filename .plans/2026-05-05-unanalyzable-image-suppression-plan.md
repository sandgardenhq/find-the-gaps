# Unanalyzable Image Suppression Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** When the screenshot-gaps pass encounters an image we cannot reliably analyze (vision-unsupported format or any GIF), use HTML `width`/`height` attrs and a `Content-Length` HEAD probe to decide whether the image is plausibly the screenshot the surrounding prose is about. Reroute the corresponding "missing screenshot" finding to a new `## Possibly Covered` subsection in `screenshots.md` instead of emitting it as a gap.

**Architecture:** A pre-detection suppression decider classifies each suppression-eligible image as covering or not. Synthesized "matches"/"does not match" verdicts are merged into the existing verdict map and handed to the detection LLM, which already routes covered passages into its `suppressed_by_image` array via the verdict-enriched prompt. The suppressed items are then plumbed through `ScreenshotResult` and rendered as `## Possibly Covered`.

**Tech Stack:** Go 1.26+, `testing` stdlib + testify, `net/http` HEAD with mockable `RoundTripper`, existing Bifrost vision pipeline.

**Design reference:** `.plans/2026-05-05-unanalyzable-image-suppression-design.md`

---

## Conventions

- TDD per `CLAUDE.md`: every task is RED → GREEN → REFACTOR. Test files live next to the file under test (`screenshot_gaps_test.go` next to `screenshot_gaps.go`).
- Coverage gate: `go test -coverprofile=coverage.out ./internal/analyzer/... && go tool cover -func=coverage.out` after each task; do not move on until ≥90% statement coverage on `internal/analyzer/`.
- Lint gate after each task: `golangci-lint run ./...`.
- Commit message format from `CLAUDE.md` §9. Each task = one commit.
- Do NOT add the `// PROMPT:` comment to anything that isn't an actual LLM prompt string.

---

## Task 1: Parse `width`/`height` from HTML `<img>` attrs

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` — add fields to `imageRef`, add two regexes, populate in `extractImages`.
- Test: `internal/analyzer/screenshot_gaps_test.go` — extend table for `extractImages`.

**Step 1.1: Write the failing tests.**

Add to `screenshot_gaps_test.go`:

```go
func TestExtractImagesParsesWidthAndHeightAttrs(t *testing.T) {
	cases := []struct {
		name      string
		md        string
		wantW     int
		wantH     int
	}{
		{
			name:  "double-quoted width and height",
			md:    `<img src="a.png" width="800" height="600">`,
			wantW: 800, wantH: 600,
		},
		{
			name:  "single-quoted width only",
			md:    `<img src='a.png' width='400'>`,
			wantW: 400, wantH: 0,
		},
		{
			name:  "absent attrs",
			md:    `<img src="a.png">`,
			wantW: 0, wantH: 0,
		},
		{
			name:  "non-numeric width is ignored",
			md:    `<img src="a.png" width="auto" height="100">`,
			wantW: 0, wantH: 100,
		},
		{
			name:  "markdown image carries no dimensions",
			md:    `![alt](a.png)`,
			wantW: 0, wantH: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refs := extractImages(tc.md)
			if len(refs) != 1 {
				t.Fatalf("expected 1 ref, got %d", len(refs))
			}
			if refs[0].DeclaredWidth != tc.wantW {
				t.Errorf("DeclaredWidth: got %d, want %d", refs[0].DeclaredWidth, tc.wantW)
			}
			if refs[0].DeclaredHeight != tc.wantH {
				t.Errorf("DeclaredHeight: got %d, want %d", refs[0].DeclaredHeight, tc.wantH)
			}
		})
	}
}
```

**Step 1.2: Run the test, see it fail.**

```bash
go test ./internal/analyzer/ -run TestExtractImagesParsesWidthAndHeightAttrs
```

Expected: compile error — `imageRef has no field DeclaredWidth`.

**Step 1.3: Add fields and regexes to `screenshot_gaps.go`.**

In the `imageRef` struct, append:

```go
// DeclaredWidth and DeclaredHeight are the integer values of the HTML
// width / height attrs, if present and parseable; zero otherwise.
// Markdown ![]() syntax cannot carry dimensions, so refs from that
// syntax always have zero values here.
DeclaredWidth  int
DeclaredHeight int
```

Add regexes near the existing `htmlAttr*` ones:

```go
var htmlAttrWidthRe = regexp.MustCompile(`(?i)\bwidth\s*=\s*(?:"([^"]*)"|'([^']*)')`)
var htmlAttrHeightRe = regexp.MustCompile(`(?i)\bheight\s*=\s*(?:"([^"]*)"|'([^']*)')`)
```

In `extractImages`, inside the HTML-img loop (next to the `htmlAttrAltRe` block), parse width and height:

```go
w, h := 0, 0
if mm := htmlAttrWidthRe.FindStringSubmatch(attrs); mm != nil {
	v := mm[1]
	if v == "" {
		v = mm[2]
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		w = n
	}
}
if mm := htmlAttrHeightRe.FindStringSubmatch(attrs); mm != nil {
	v := mm[1]
	if v == "" {
		v = mm[2]
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
		h = n
	}
}
```

Add `w` and `h` to the appended `imageRef`:

```go
refs = append(refs, imageRef{
	AltText:        alt,
	Src:            src,
	DeclaredWidth:  w,
	DeclaredHeight: h,
	SectionHeading: currentHeading,
	ParagraphIndex: pIdx,
})
```

Add `"strconv"` to imports.

**Step 1.4: Run the test, see it pass.**

```bash
go test ./internal/analyzer/ -run TestExtractImagesParsesWidthAndHeightAttrs -v
```

Expected: PASS for all 5 sub-cases.

**Step 1.5: Run full package tests + lint.**

```bash
go test ./internal/analyzer/...
golangci-lint run ./internal/analyzer/...
```

Both must succeed.

**Step 1.6: Commit.**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): parse <img> width/height attrs in extractImages

- RED: TestExtractImagesParsesWidthAndHeightAttrs covers double/single-quoted, absent, non-numeric, and markdown-syntax cases
- GREEN: added DeclaredWidth/DeclaredHeight fields to imageRef + regex parsing in extractImages
- Status: 5/5 new sub-cases passing, full analyzer package green

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 2: Add `suppressionEligible` predicate

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` — new function below `visionUnsupportedDataMimes`.
- Test: `internal/analyzer/screenshot_gaps_test.go`.

**Step 2.1: Write the failing test.**

```go
func TestSuppressionEligible(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"png is not eligible", "https://x.com/a.png", false},
		{"jpeg is not eligible", "https://x.com/a.jpg", false},
		{"webp is not eligible", "https://x.com/a.webp", false},
		{"gif is eligible", "https://x.com/a.gif", true},
		{"GIF uppercase is eligible", "https://x.com/a.GIF", true},
		{"svg is eligible", "https://x.com/a.svg", true},
		{"avif is eligible", "https://x.com/a.avif", true},
		{"image/gif data URI is eligible", "data:image/gif;base64,abc", true},
		{"image/svg+xml data URI is eligible", "data:image/svg+xml;base64,abc", true},
		{"image/png data URI is not eligible", "data:image/png;base64,abc", false},
		{"extensionless URL is not eligible (vision-supported by default)", "https://x.com/img/abc", false},
		{"empty src is not eligible", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := suppressionEligible(imageRef{Src: tc.src})
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}
```

**Step 2.2: Run the test, see it fail.**

```bash
go test ./internal/analyzer/ -run TestSuppressionEligible
```

Expected: compile error — `undefined: suppressionEligible`.

**Step 2.3: Implement.**

In `screenshot_gaps.go`, below `visionUnsupportedDataMimes`:

```go
// suppressionEligible reports whether a given imageRef should be routed
// through the unanalyzable-image suppression layer instead of the vision
// relevance pass. Eligible images are: vision-unsupported formats (per
// visionUnsupportedExts / visionUnsupportedDataMimes) and ALL GIFs.
// GIFs are eligible because every vision provider treats them as a single
// still (typically the first frame), which silently misleads on animated
// demos; the suppression layer's bytes-and-dimensions heuristic is more
// honest than a first-frame relevance verdict.
func suppressionEligible(r imageRef) bool {
	src := strings.TrimSpace(r.Src)
	if src == "" {
		return false
	}
	if strings.HasPrefix(src, "data:") {
		mimeEnd := strings.IndexAny(src[len("data:"):], ";,")
		if mimeEnd < 0 {
			return false
		}
		mime := strings.ToLower(src[len("data:") : len("data:")+mimeEnd])
		if mime == "image/gif" {
			return true
		}
		_, bad := visionUnsupportedDataMimes[mime]
		return bad
	}
	u, err := url.Parse(src)
	if err != nil {
		return false
	}
	ext := strings.ToLower(extOf(u.Path))
	if ext == ".gif" {
		return true
	}
	_, bad := visionUnsupportedExts[ext]
	return bad
}
```

If `extOf` does not already exist, find the equivalent inside `visionSupported` (line ~230) and either reuse it or extract it. From the existing code, `visionSupported` uses `path.Ext`. Use the same approach inline:

```go
import "path"
// inside suppressionEligible, replace extOf(u.Path) with path.Ext(u.Path)
```

Confirm `path` is imported (it likely is — `visionSupported` uses it). If not, add to imports.

**Step 2.4: Run the test.**

```bash
go test ./internal/analyzer/ -run TestSuppressionEligible -v
```

Expected: PASS for all 12 sub-cases.

**Step 2.5: Coverage + lint.**

```bash
go test -coverprofile=coverage.out ./internal/analyzer/... && go tool cover -func=coverage.out | grep -E "suppressionEligible|total"
golangci-lint run ./internal/analyzer/...
```

`suppressionEligible` should report 100%. Total package coverage must be ≥90%.

**Step 2.6: Commit.**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add suppressionEligible predicate

Identifies images we cannot reliably analyze through the vision pass:
all GIFs (vision providers see only the first frame, which silently
misleads on animated demos) plus the existing vision-unsupported
formats (svg, avif, ico, bmp, tiff, heic).

- RED: TestSuppressionEligible covers png/jpeg/webp/gif/svg/avif/data URIs/extensionless/empty
- GREEN: new suppressionEligible() function on imageRef
- Status: 12/12 sub-cases passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 3: Add `htmlAttrsSuggestScreenshot` predicate

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`.
- Test: `internal/analyzer/screenshot_gaps_test.go`.

**Step 3.1: Write the failing test.**

```go
func TestHTMLAttrsSuggestScreenshot(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want bool
	}{
		{"both zero", 0, 0, false},
		{"width below threshold", 399, 0, false},
		{"width at threshold", 400, 0, true},
		{"width above threshold", 800, 100, true},
		{"height at threshold, width below", 100, 400, true},
		{"both well below threshold (typical icon)", 24, 24, false},
		{"only width set, above threshold", 800, 0, true},
		{"only height set, above threshold", 0, 1200, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := imageRef{DeclaredWidth: tc.w, DeclaredHeight: tc.h}
			if got := htmlAttrsSuggestScreenshot(r); got != tc.want {
				t.Errorf("got %v, want %v (w=%d h=%d)", got, tc.want, tc.w, tc.h)
			}
		})
	}
}
```

**Step 3.2: Run, see it fail.**

```bash
go test ./internal/analyzer/ -run TestHTMLAttrsSuggestScreenshot
```

Expected: compile error — `undefined: htmlAttrsSuggestScreenshot`.

**Step 3.3: Implement.**

Add a constant near `ScreenshotPromptBudget`:

```go
// SuppressionMinDimension is the minimum max(width, height) in pixels of
// a declared HTML attr that suggests an image is plausibly a screenshot
// rather than an inline icon or thumbnail. 400px is the inflection point
// between "decoration" and "deliberate page real estate" on docs sites.
const SuppressionMinDimension = 400
```

Add the function below `suppressionEligible`:

```go
// htmlAttrsSuggestScreenshot reports whether an imageRef's declared
// width / height attrs cross the screenshot-shaped threshold. Either
// dimension alone is sufficient.
func htmlAttrsSuggestScreenshot(r imageRef) bool {
	larger := r.DeclaredWidth
	if r.DeclaredHeight > larger {
		larger = r.DeclaredHeight
	}
	return larger >= SuppressionMinDimension
}
```

**Step 3.4: Run, see it pass.**

```bash
go test ./internal/analyzer/ -run TestHTMLAttrsSuggestScreenshot -v
```

Expected: PASS for all 8 sub-cases.

**Step 3.5: Coverage + lint.**

```bash
go test ./internal/analyzer/...
golangci-lint run ./internal/analyzer/...
```

**Step 3.6: Commit.**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add htmlAttrsSuggestScreenshot predicate

- RED: TestHTMLAttrsSuggestScreenshot covers below/at/above-threshold cases for w-only, h-only, both
- GREEN: htmlAttrsSuggestScreenshot returns true iff max(w, h) >= 400
- Status: 8/8 sub-cases passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 4: Add `headSuggestsScreenshot` (HEAD probe with mockable transport)

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`.
- Test: `internal/analyzer/screenshot_gaps_test.go`.

**Step 4.1: Write the failing test.**

```go
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHeadSuggestsScreenshot(t *testing.T) {
	t.Run("data URI uses inline length and short-circuits HEAD", func(t *testing.T) {
		// "image/png;base64," + 30KB of base64 padding = > 30720 bytes
		big := "data:image/png;base64," + strings.Repeat("A", 40000)
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HEAD should not be issued for data URI")
			return nil, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, big)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Errorf("expected true for >30KB data URI, got false")
		}
	})

	t.Run("small data URI returns false", func(t *testing.T) {
		small := "data:image/svg+xml,<svg></svg>"
		got, err := headSuggestsScreenshot(context.Background(), &http.Client{}, small)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Errorf("expected false for small data URI, got true")
		}
	})

	t.Run("HEAD 200 with Content-Length above threshold returns true", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodHead {
				t.Errorf("expected HEAD, got %s", req.Method)
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Length": []string{"50000"}},
				Body:       http.NoBody,
			}, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/a.svg")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Error("expected true for 50KB Content-Length")
		}
	})

	t.Run("HEAD 200 with Content-Length below threshold returns false", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Length": []string{"5000"}},
				Body:       http.NoBody,
			}, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/a.gif")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Error("expected false for 5KB Content-Length")
		}
	})

	t.Run("missing Content-Length returns false with no error (no signal)", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: http.NoBody}, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/a.gif")
		if err != nil {
			t.Errorf("missing Content-Length should not be an error: %v", err)
		}
		if got {
			t.Error("expected false when no Content-Length header")
		}
	})

	t.Run("non-2xx returns false with error", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 404, Header: http.Header{}, Body: http.NoBody}, nil
		})}
		got, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/missing.svg")
		if err == nil {
			t.Error("expected error on 404")
		}
		if got {
			t.Error("expected false on 404")
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})}
		_, err := headSuggestsScreenshot(context.Background(), client, "https://x.com/a.gif")
		if err == nil {
			t.Error("expected error from transport")
		}
	})
}
```

**Step 4.2: Run, see it fail.**

```bash
go test ./internal/analyzer/ -run TestHeadSuggestsScreenshot
```

Expected: compile error — `undefined: headSuggestsScreenshot`.

**Step 4.3: Implement.**

Add a constant:

```go
// SuppressionMinBytes is the minimum HEAD Content-Length (or inline data
// URI byte length) below which an image is assumed to be too small to be
// a screenshot. 30KB sits between "decoration" (icons, small logos) and
// "content" (UI screenshots typically 50-300KB).
const SuppressionMinBytes = 30 * 1024
```

Add the function:

```go
// headSuggestsScreenshot probes a single image URL with HEAD and reports
// whether its Content-Length crosses SuppressionMinBytes. Data URIs short-
// circuit and use the inline byte length without issuing a request.
//
// Failure semantics (matches the design's "no signal -> no suppression"
// rule): missing Content-Length on a 2xx response returns (false, nil) so
// the caller treats it as no signal. Transport errors and non-2xx responses
// return (false, err) so the caller can log them but still falls through
// to no-suppression — the orchestrator does not propagate the error.
func headSuggestsScreenshot(ctx context.Context, client *http.Client, src string) (bool, error) {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "data:") {
		// Inline byte length is a strict lower bound (base64 inflates by
		// ~33%, so the real payload is even smaller); for the suppression
		// threshold this is fine.
		return len(src) >= SuppressionMinBytes, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, src, nil)
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, fmt.Errorf("HEAD %s: status %d", src, resp.StatusCode)
	}
	cl := resp.Header.Get("Content-Length")
	if cl == "" {
		return false, nil
	}
	n, err := strconv.ParseInt(cl, 10, 64)
	if err != nil {
		return false, nil
	}
	return n >= SuppressionMinBytes, nil
}
```

Add `"net/http"` to imports if not already present.

**Step 4.4: Run, see it pass.**

```bash
go test ./internal/analyzer/ -run TestHeadSuggestsScreenshot -v
```

Expected: PASS on all 7 sub-cases.

**Step 4.5: Coverage + lint.**

```bash
go test -coverprofile=coverage.out ./internal/analyzer/... && go tool cover -func=coverage.out | grep -E "headSuggestsScreenshot|total"
golangci-lint run ./internal/analyzer/...
```

**Step 4.6: Commit.**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add HEAD probe for unanalyzable image bytes

- RED: TestHeadSuggestsScreenshot covers data URI short-circuit, 200+CL
  above/below threshold, missing CL, 404, and transport error
- GREEN: headSuggestsScreenshot uses HEAD Content-Length with
  no-signal-on-failure semantics; data URIs use inline len()
- Status: 7/7 sub-cases passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 5: Add `decisionForImageRef` orchestrator

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`.
- Test: `internal/analyzer/screenshot_gaps_test.go`.

**Step 5.1: Write the failing test.**

```go
func TestDecisionForImageRef(t *testing.T) {
	t.Run("HTML attrs sufficient: HEAD is not issued", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HEAD must not be issued when HTML attrs already cross threshold")
			return nil, nil
		})}
		ref := imageRef{Src: "https://x.com/a.gif", DeclaredWidth: 800, DeclaredHeight: 0}
		if !decisionForImageRef(context.Background(), client, ref) {
			t.Error("expected true: width=800 attr alone should suppress")
		}
	})

	t.Run("HTML attrs absent: falls through to HEAD", func(t *testing.T) {
		called := false
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Length": []string{"50000"}},
				Body:       http.NoBody,
			}, nil
		})}
		ref := imageRef{Src: "https://x.com/a.gif"}
		if !decisionForImageRef(context.Background(), client, ref) {
			t.Error("expected true via HEAD")
		}
		if !called {
			t.Error("expected HEAD to be issued when HTML attrs absent")
		}
	})

	t.Run("HTML attrs below threshold and HEAD says small: false", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Length": []string{"5000"}},
				Body:       http.NoBody,
			}, nil
		})}
		ref := imageRef{Src: "https://x.com/a.gif", DeclaredWidth: 100}
		if decisionForImageRef(context.Background(), client, ref) {
			t.Error("expected false: small attr + small bytes")
		}
	})

	t.Run("HEAD failure produces false (no signal)", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})}
		ref := imageRef{Src: "https://x.com/a.gif"}
		if decisionForImageRef(context.Background(), client, ref) {
			t.Error("expected false: HEAD failure means no signal -> no suppression")
		}
	})
}
```

**Step 5.2: Run, see it fail.**

```bash
go test ./internal/analyzer/ -run TestDecisionForImageRef
```

Expected: compile error — `undefined: decisionForImageRef`.

**Step 5.3: Implement.**

```go
// decisionForImageRef applies the design's signal precedence: HTML attrs
// win, HEAD as fallback, no signal -> no suppression. HEAD errors are
// swallowed (logged at debug) because the design's "no signal -> no
// suppression" rule means failure is operationally identical to absence.
func decisionForImageRef(ctx context.Context, client *http.Client, r imageRef) bool {
	if htmlAttrsSuggestScreenshot(r) {
		return true
	}
	ok, err := headSuggestsScreenshot(ctx, client, r.Src)
	if err != nil {
		log.Debugf("suppression: HEAD failed for %s: %v", r.Src, err)
		return false
	}
	return ok
}
```

**Step 5.4: Run, see it pass.**

```bash
go test ./internal/analyzer/ -run TestDecisionForImageRef -v
```

Expected: PASS on all 4 sub-cases.

**Step 5.5: Coverage + lint.**

```bash
go test ./internal/analyzer/...
golangci-lint run ./internal/analyzer/...
```

**Step 5.6: Commit.**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add decisionForImageRef orchestrator

Implements signal precedence per design: HTML attrs win, HEAD as fallback,
no signal -> no suppression. HEAD errors are logged at debug and treated
as no-signal (failure is operationally equivalent to absence).

- RED: TestDecisionForImageRef covers attrs-sufficient (HEAD skipped),
  attrs-absent (HEAD issued), both-below-threshold, HEAD-error-swallowed
- GREEN: decisionForImageRef wraps htmlAttrsSuggestScreenshot + headSuggestsScreenshot
- Status: 4/4 sub-cases passing

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 6: Add concurrent suppression decider with cache and concurrency cap

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`.
- Test: `internal/analyzer/screenshot_gaps_test.go`.

**Step 6.1: Write the failing tests.**

```go
func TestDecideAllSuppressionsDedupesByURL(t *testing.T) {
	var heads int32
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		atomic.AddInt32(&heads, 1)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Length": []string{"50000"}},
			Body:       http.NoBody,
		}, nil
	})}
	refs := []imageRef{
		{Src: "https://x.com/a.gif"},
		{Src: "https://x.com/a.gif"},
		{Src: "https://x.com/a.gif"},
	}
	decisions := decideAllSuppressions(context.Background(), client, refs, 8)
	if len(decisions) != 3 {
		t.Fatalf("expected 3 decisions, got %d", len(decisions))
	}
	for i, d := range decisions {
		if !d {
			t.Errorf("decision[%d] = false, want true", i)
		}
	}
	if atomic.LoadInt32(&heads) != 1 {
		t.Errorf("expected 1 HEAD (deduped), got %d", heads)
	}
}

func TestDecideAllSuppressionsRespectsConcurrencyCap(t *testing.T) {
	var inflight int32
	var maxInflight int32
	gate := make(chan struct{})
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		cur := atomic.AddInt32(&inflight, 1)
		for {
			peak := atomic.LoadInt32(&maxInflight)
			if cur <= peak || atomic.CompareAndSwapInt32(&maxInflight, peak, cur) {
				break
			}
		}
		<-gate
		atomic.AddInt32(&inflight, -1)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Length": []string{"100"}},
			Body:       http.NoBody,
		}, nil
	})}
	refs := make([]imageRef, 20)
	for i := range refs {
		refs[i] = imageRef{Src: fmt.Sprintf("https://x.com/img-%d.gif", i)}
	}
	done := make(chan struct{})
	go func() {
		decideAllSuppressions(context.Background(), client, refs, 4)
		close(done)
	}()
	// Give worker pool time to ramp up to its cap.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	<-done
	if maxInflight > 4 {
		t.Errorf("max in-flight HEADs = %d, want <= 4", maxInflight)
	}
}
```

Add `"sync/atomic"`, `"time"`, `"fmt"` to test imports if not already present.

**Step 6.2: Run, see it fail.**

```bash
go test ./internal/analyzer/ -run TestDecideAllSuppressions
```

Expected: compile error — `undefined: decideAllSuppressions`.

**Step 6.3: Implement.**

```go
// SuppressionConcurrencyCap is the maximum number of in-flight HEAD
// requests for the suppression decider. Image-heavy pages can have
// dozens of unanalyzable images; a small cap prevents fan-out storms.
const SuppressionConcurrencyCap = 8

// decideAllSuppressions runs decisionForImageRef for every input ref in
// parallel, deduplicating by absolute Src so one image referenced from
// N pages produces a single HEAD. The returned slice is index-aligned
// with refs. cap is the maximum in-flight HEAD count (0 -> default).
func decideAllSuppressions(ctx context.Context, client *http.Client, refs []imageRef, cap int) []bool {
	if cap <= 0 {
		cap = SuppressionConcurrencyCap
	}
	out := make([]bool, len(refs))
	if len(refs) == 0 {
		return out
	}
	type cached struct {
		done <-chan struct{}
		val  bool
	}
	cache := make(map[string]*cached)
	var mu sync.Mutex
	sem := make(chan struct{}, cap)
	var wg sync.WaitGroup
	for i, r := range refs {
		i, r := i, r
		mu.Lock()
		c, ok := cache[r.Src]
		if !ok {
			ch := make(chan struct{})
			c = &cached{done: ch}
			cache[r.Src] = c
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				v := decisionForImageRef(ctx, client, r)
				mu.Lock()
				c.val = v
				mu.Unlock()
				close(ch)
			}()
		} else {
			mu.Unlock()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-c.done
			mu.Lock()
			out[i] = c.val
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}
```

Add `"sync"` to imports if missing.

**Step 6.4: Run, see it pass.**

```bash
go test ./internal/analyzer/ -run TestDecideAllSuppressions -v -count=1 -race
```

Expected: PASS, no data races.

**Step 6.5: Coverage + lint.**

```bash
go test -race ./internal/analyzer/...
golangci-lint run ./internal/analyzer/...
```

**Step 6.6: Commit.**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): concurrent suppression decider with URL cache

Worker pool with semaphore-bounded concurrency (default 8) and per-Src
cache so the same image referenced from N pages produces a single HEAD.

- RED: TestDecideAllSuppressionsDedupesByURL (3 refs same Src -> 1 HEAD),
  TestDecideAllSuppressionsRespectsConcurrencyCap (20 refs, cap=4)
- GREEN: decideAllSuppressions with chan-semaphore + per-URL cached chan
- Status: tests pass under -race

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 7: Plumb `SuppressedByImage` items through `detectionPass` and `ScreenshotResult`

**Background:** The detection LLM already emits `suppressed_by_image` items with the same shape as `gaps` (see `screenshot_gaps.go:454`), but the data is currently only counted (`detectionPass` returns `suppressed int`). To render `## Possibly Covered`, we need the actual items.

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` — `detectionPass` signature, `ScreenshotResult`.
- Test: `internal/analyzer/screenshot_gaps_test.go`.

**Step 7.1: Write the failing test.**

```go
func TestDetectionPassReturnsSuppressedItems(t *testing.T) {
	// Hand-rolled fake LLM client that returns a fixed JSON response
	// containing both gaps and suppressed_by_image.
	client := &fakeLLMClient{
		response: `{
			"gaps": [{
				"quoted_passage": "Click Save to continue.",
				"should_show": "save dialog",
				"suggested_alt": "save dialog",
				"insertion_hint": "after the click-save paragraph"
			}],
			"suppressed_by_image": [{
				"quoted_passage": "Watch the demo gif of the upload flow.",
				"should_show": "upload flow",
				"suggested_alt": "upload demo",
				"insertion_hint": "after the demo-gif paragraph"
			}]
		}`,
		caps: ModelCapabilities{Vision: true},
	}
	page := DocPage{URL: "https://x.com/p", Path: "p.md", Content: "# Hello\n\nClick Save."}
	gaps, suppressed, _, err := detectionPass(context.Background(), client, page, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gaps) != 1 {
		t.Errorf("expected 1 gap, got %d", len(gaps))
	}
	if len(suppressed) != 1 {
		t.Fatalf("expected 1 suppressed item, got %d", len(suppressed))
	}
	if suppressed[0].QuotedPassage != "Watch the demo gif of the upload flow." {
		t.Errorf("suppressed passage mismatch: %q", suppressed[0].QuotedPassage)
	}
	if suppressed[0].PageURL != page.URL {
		t.Errorf("suppressed PageURL = %q, want %q", suppressed[0].PageURL, page.URL)
	}
}
```

If `fakeLLMClient` does not yet exist, check `screenshot_gaps_test.go` for the existing pattern (the relevance-pass tests use one). Reuse it.

**Step 7.2: Run, see it fail.**

```bash
go test ./internal/analyzer/ -run TestDetectionPassReturnsSuppressedItems
```

Expected: type error — `cannot use suppressed (variable of type int) as []ScreenshotGap` (the current return is `int`).

**Step 7.3: Implement.**

Modify `detectionPass` signature and body:

- Change return type from `(gaps []ScreenshotGap, suppressed int, skipped bool, err error)` to `(gaps []ScreenshotGap, suppressed []ScreenshotGap, skipped bool, err error)`.
- After the existing `gaps = ...` loop, add a parallel loop that builds `suppressed []ScreenshotGap` from `resp.SuppressedByImage` (same field-by-field unescape pattern, with PageURL/PagePath set from the page).
- Update the `return` statements (currently three of them in the function body) to return `nil` for the suppressed slice on early-return paths.

Update `DetectScreenshotGaps` (the only caller) to accept the new shape — at this point just take `len(suppressed)` to keep `stats.MissingSuppressed` working unchanged. The next task wires the actual items through.

**Step 7.4: Run, see it pass.**

```bash
go test ./internal/analyzer/ -run TestDetectionPassReturnsSuppressedItems -v
go test ./internal/analyzer/...
```

Expected: PASS, full package green.

**Step 7.5: Coverage + lint.**

```bash
go test ./internal/analyzer/...
golangci-lint run ./...
```

**Step 7.6: Commit.**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
refactor(analyzer): return suppressed_by_image items from detectionPass

Previously detectionPass dropped suppressed_by_image content and only
returned its count (audit-only). The next task surfaces these items as
a ## Possibly Covered subsection in screenshots.md, so we need the
actual ScreenshotGap data, not just len.

- RED: TestDetectionPassReturnsSuppressedItems
- GREEN: detectionPass return signature now (gaps, suppressed, skipped, err)
  with suppressed as []ScreenshotGap; caller takes len() for stats unchanged
- Status: full package green

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 8: Add `PossiblyCovered` field to `ScreenshotResult` and `ScreenshotPageStats`

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go`.
- Test: `internal/analyzer/screenshot_gaps_test.go`.

**Step 8.1: Write the failing test.**

```go
func TestScreenshotResultHasPossiblyCovered(t *testing.T) {
	// Compile-time guarantee that the field exists with the expected type.
	var r ScreenshotResult
	r.PossiblyCovered = []ScreenshotGap{{PageURL: "https://x.com"}}
	if len(r.PossiblyCovered) != 1 {
		t.Fatal("PossiblyCovered field unusable")
	}
	if r.PossiblyCovered[0].PageURL != "https://x.com" {
		t.Fatal("ScreenshotGap shape on PossiblyCovered does not match")
	}
}

func TestScreenshotPageStatsHasPossiblyCovered(t *testing.T) {
	var s ScreenshotPageStats
	s.PossiblyCovered = 3
	if s.PossiblyCovered != 3 {
		t.Fatal("PossiblyCovered field unusable")
	}
}
```

**Step 8.2: Run, see it fail.**

```bash
go test ./internal/analyzer/ -run "TestScreenshotResultHasPossiblyCovered|TestScreenshotPageStatsHasPossiblyCovered"
```

Expected: compile error — `unknown field PossiblyCovered`.

**Step 8.3: Implement.**

Add the field to `ScreenshotResult`:

```go
type ScreenshotResult struct {
	MissingGaps     []ScreenshotGap
	PossiblyCovered []ScreenshotGap
	ImageIssues     []ImageIssue
	AuditStats      []ScreenshotPageStats
}
```

Add the count to `ScreenshotPageStats`:

```go
type ScreenshotPageStats struct {
	PageURL            string
	VisionEnabled      bool
	RelevanceBatches   int
	ImagesSeen         int
	ImageIssues        int
	MissingScreenshots int
	MissingSuppressed  int
	PossiblyCovered    int
	DetectionSkipped   bool
}
```

**Step 8.4: Run, see it pass.**

```bash
go test ./internal/analyzer/...
```

Expected: PASS.

**Step 8.5: Coverage + lint.**

```bash
golangci-lint run ./...
```

**Step 8.6: Commit.**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): add PossiblyCovered to ScreenshotResult + ScreenshotPageStats

Empty fields wired up here; population happens in the next task when
DetectScreenshotGaps starts emitting them.

- RED: compile-time field-presence tests
- GREEN: added PossiblyCovered fields ([]ScreenshotGap and int respectively)
- Status: full package green

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 9: Wire suppression into `DetectScreenshotGaps`

**Background:** This is the big integration step. The detection LLM already routes covered passages into `suppressed_by_image` based on the verdict-enriched prompt; we just need to feed it the right verdicts.

For each page:
1. Partition refs into `visionPathRefs` (vision-supported & not GIF) and `suppressionPathRefs` (vision-unsupported or GIF).
2. Run the existing vision relevance pass on `visionPathRefs` only.
3. Run `decideAllSuppressions` on `suppressionPathRefs`.
4. For each suppression-path ref, build a synthetic `ImageVerdict` (`Matches=true` if decided=true, `Matches=false` otherwise — but use `Matches=false` only when we have enough confidence to assert "does not match"; otherwise omit the verdict entirely so the prompt treats it as "unknown" and falls back to locality rules).
5. Merge synthetic verdicts with vision verdicts and pass to detection.
6. Detection now emits both `gaps` and `suppressed_by_image`. Append `gaps` to `MissingGaps` (unchanged) and `suppressed_by_image` to `PossiblyCovered`.

**Decision (locked from design):** A suppression-path ref with `decided=true` produces a synthetic `Matches=true` verdict. A suppression-path ref with `decided=false` produces NO synthetic verdict (so it shows up as "unknown" in the prompt and the locality rule applies). Rationale: `false` from our heuristic means "no signal," not "definitely not a screenshot." Asserting "does not match" would over-claim.

**Files:**
- Modify: `internal/analyzer/screenshot_gaps.go` — extend `DetectScreenshotGaps`; add `partitionRefsForVision` helper.
- Test: `internal/analyzer/screenshot_gaps_test.go`.

**Step 9.1: Write a partition test first.**

```go
func TestPartitionRefsForVision(t *testing.T) {
	refs := []imageRef{
		{Src: "https://x.com/a.png"},
		{Src: "https://x.com/b.gif"},
		{Src: "https://x.com/c.svg"},
		{Src: "https://x.com/d.webp"},
	}
	visionPath, suppressionPath := partitionRefsForVision(refs)
	if len(visionPath) != 2 {
		t.Errorf("vision path len = %d, want 2 (png, webp)", len(visionPath))
	}
	if len(suppressionPath) != 2 {
		t.Errorf("suppression path len = %d, want 2 (gif, svg)", len(suppressionPath))
	}
}
```

**Step 9.2: Run, see it fail.**

```bash
go test ./internal/analyzer/ -run TestPartitionRefsForVision
```

Expected: compile error — `undefined: partitionRefsForVision`.

**Step 9.3: Implement the partition helper.**

```go
// partitionRefsForVision splits image refs by whether they should go through
// the vision relevance pass (returned first) or the unanalyzable-image
// suppression layer (returned second). Same-order preservation is not
// guaranteed across the two slices but IS guaranteed within each.
func partitionRefsForVision(refs []imageRef) (visionPath, suppressionPath []imageRef) {
	for _, r := range refs {
		if suppressionEligible(r) {
			suppressionPath = append(suppressionPath, r)
		} else {
			visionPath = append(visionPath, r)
		}
	}
	return visionPath, suppressionPath
}
```

**Step 9.4: Run partition test, see it pass.**

```bash
go test ./internal/analyzer/ -run TestPartitionRefsForVision -v
```

**Step 9.5: Wire `DetectScreenshotGaps`.**

In `DetectScreenshotGaps`, replace the body of the per-page loop with the new flow. Schematic (read existing code at line 836+ for exact context):

```go
refs := extractImages(page.Content)
stats := ScreenshotPageStats{PageURL: page.URL, ImagesSeen: len(refs)}

visionPathRefs, suppressionPathRefs := partitionRefsForVision(refs)

var verdicts []ImageVerdict

if client.Capabilities().Vision && len(visionPathRefs) > 0 {
	stats.VisionEnabled = true
	visionRefs := resolveVisionRefs(page.URL, visionPathRefs)
	visionRefs = filterVisionSupportedImages(visionRefs)
	stats.RelevanceBatches = len(splitImageBatches(visionRefs, 5))
	issues, vs, err := relevancePass(ctx, client, page, visionRefs)
	if err != nil {
		return result, err
	}
	result.ImageIssues = append(result.ImageIssues, issues...)
	stats.ImageIssues = len(issues)
	verdicts = vs
}

// Suppression decisions for unanalyzable images. `httpClient` is a new
// constructor-injected field; for now use http.DefaultClient with a
// per-request timeout via context.
if len(suppressionPathRefs) > 0 {
	headCtx, cancel := context.WithTimeout(ctx, 5*time.Second*time.Duration(len(suppressionPathRefs)))
	defer cancel()
	decisions := decideAllSuppressions(headCtx, http.DefaultClient, suppressionPathRefs, SuppressionConcurrencyCap)
	for i, r := range suppressionPathRefs {
		if decisions[i] {
			verdicts = append(verdicts, ImageVerdict{
				Index:   fmt.Sprintf("img-%d", r.OriginalIndex),
				Matches: true,
			})
		}
	}
}

gaps, suppressed, skipped, err := detectionPass(ctx, client, page, refs, verdicts)
if err != nil {
	return result, err
}
stats.MissingScreenshots = len(gaps)
stats.MissingSuppressed = len(suppressed)
stats.PossiblyCovered = len(suppressed)
stats.DetectionSkipped = skipped
result.MissingGaps = append(result.MissingGaps, gaps...)
result.PossiblyCovered = append(result.PossiblyCovered, suppressed...)
result.AuditStats = append(result.AuditStats, stats)
```

Add `"net/http"` and `"time"` to imports if not already there.

**Step 9.6: Add an end-to-end-ish unit test using the existing fake LLM client pattern.**

```go
func TestDetectScreenshotGapsRoutesGifGapsToPossiblyCovered(t *testing.T) {
	page := DocPage{
		URL:  "https://x.com/p",
		Path: "p.md",
		Content: `# Demo
<img src="https://x.com/demo.gif" width="800">

This is a guided multi-step OAuth flow with several intermediate states the reader needs to see.
`,
	}
	client := &fakeLLMClient{
		caps: ModelCapabilities{Vision: true},
		// Return a gap that the detection LLM should have routed to
		// suppressed_by_image because the verdict map will mark img-1
		// (the gif) as matches=true.
		response: `{
			"gaps": [],
			"suppressed_by_image": [{
				"quoted_passage": "guided multi-step OAuth flow",
				"should_show": "OAuth steps",
				"suggested_alt": "OAuth flow",
				"insertion_hint": "after the OAuth paragraph"
			}]
		}`,
	}
	res, err := DetectScreenshotGaps(context.Background(), client, []DocPage{page}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.MissingGaps) != 0 {
		t.Errorf("MissingGaps = %d, want 0", len(res.MissingGaps))
	}
	if len(res.PossiblyCovered) != 1 {
		t.Fatalf("PossiblyCovered = %d, want 1", len(res.PossiblyCovered))
	}
}
```

**Step 9.7: Run, see all related tests pass.**

```bash
go test ./internal/analyzer/... -v
```

**Step 9.8: Coverage + lint.**

```bash
go test -race ./internal/analyzer/...
go test -coverprofile=coverage.out ./internal/analyzer/... && go tool cover -func=coverage.out | tail -1
golangci-lint run ./...
```

Coverage on `internal/analyzer/` must remain ≥90%.

**Step 9.9: Commit.**

```bash
git add internal/analyzer/screenshot_gaps.go internal/analyzer/screenshot_gaps_test.go
git commit -m "$(cat <<'EOF'
feat(analyzer): route gif/unsupported images through suppression layer

GIFs and vision-unsupported formats no longer go to the vision relevance
pass. Instead, decideAllSuppressions classifies each via HTML attrs +
HEAD Content-Length; passing refs get a synthetic matches=true verdict
that flows into the detection prompt. The detection LLM's existing
locality logic moves the corresponding passages into suppressed_by_image,
which DetectScreenshotGaps now surfaces as result.PossiblyCovered.

- RED: TestPartitionRefsForVision; TestDetectScreenshotGapsRoutesGifGapsToPossiblyCovered
- GREEN: partitionRefsForVision + DetectScreenshotGaps integration with decideAllSuppressions
- Status: package green under -race; coverage >= 90%

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 10: Render `## Possibly Covered` in `WriteScreenshots`

**Files:**
- Modify: `internal/reporter/reporter.go` — extend `WriteScreenshots`.
- Test: `internal/reporter/reporter_test.go`.

**Step 10.1: Write the failing test.**

```go
func TestWriteScreenshotsRendersPossiblyCovered(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{},
		PossiblyCovered: []analyzer.ScreenshotGap{{
			PageURL:       "https://x.com/p",
			QuotedPassage: "Watch the upload demo.",
			ShouldShow:    "upload flow",
			SuggestedAlt:  "upload demo",
			InsertionHint: "after the demo paragraph",
		}},
		AuditStats: []analyzer.ScreenshotPageStats{{PageURL: "https://x.com/p", VisionEnabled: true}},
	}
	if err := WriteScreenshots(dir, res); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(body)
	if !strings.Contains(got, "## Possibly Covered") {
		t.Error("expected '## Possibly Covered' header in screenshots.md")
	}
	if !strings.Contains(got, "Watch the upload demo.") {
		t.Error("expected the quoted passage in the rendered output")
	}
	// Ordering: ## Possibly Covered must come AFTER ## Missing Screenshots
	// and BEFORE ## Image Issues so the file reads top-down by severity.
	missingPos := strings.Index(got, "Missing Screenshots")
	pcPos := strings.Index(got, "Possibly Covered")
	imgPos := strings.Index(got, "Image Issues")
	if !(missingPos < pcPos && pcPos < imgPos) {
		t.Errorf("section ordering wrong: missing=%d possibly=%d issues=%d", missingPos, pcPos, imgPos)
	}
}

func TestWriteScreenshotsOmitsPossiblyCoveredWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	res := analyzer.ScreenshotResult{
		MissingGaps: []analyzer.ScreenshotGap{{PageURL: "https://x.com/p", QuotedPassage: "ok"}},
		AuditStats:  []analyzer.ScreenshotPageStats{{PageURL: "https://x.com/p"}},
	}
	if err := WriteScreenshots(dir, res); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "screenshots.md"))
	if strings.Contains(string(body), "Possibly Covered") {
		t.Error("Possibly Covered must not render when the slice is empty")
	}
}
```

**Step 10.2: Run, see it fail.**

```bash
go test ./internal/reporter/ -run TestWriteScreenshotsRendersPossiblyCovered
```

Expected: failure — header missing.

**Step 10.3: Implement.**

In `WriteScreenshots`, after the existing `## Missing Screenshots` block and BEFORE the `## Image Issues` block, add:

```go
if len(res.PossiblyCovered) > 0 {
	sb.WriteString("\n## Possibly Covered\n\n")
	sb.WriteString("Suppressed because an unanalyzable but plausibly screenshot-shaped image is already on the page. Quick visual check is enough to confirm or override.\n\n")
	seen := map[string]bool{}
	var order []string
	byPage := map[string][]analyzer.ScreenshotGap{}
	for _, g := range res.PossiblyCovered {
		if !seen[g.PageURL] {
			seen[g.PageURL] = true
			order = append(order, g.PageURL)
		}
		byPage[g.PageURL] = append(byPage[g.PageURL], g)
	}
	for _, page := range order {
		fmt.Fprintf(&sb, "### %s\n\n", page)
		for _, g := range byPage[page] {
			fmt.Fprintf(&sb, "- **Passage:**\n\n")
			fmt.Fprintf(&sb, "%s\n\n", fencedCodeBlock(g.QuotedPassage))
			fmt.Fprintf(&sb, "  - **Would have suggested:** %s\n", g.ShouldShow)
			fmt.Fprintf(&sb, "  - **Insert (if uncovered):** %s\n\n", g.InsertionHint)
		}
	}
}
```

**Step 10.4: Run, see it pass.**

```bash
go test ./internal/reporter/ -v
```

**Step 10.5: Coverage + lint.**

```bash
go test ./...
golangci-lint run ./...
```

**Step 10.6: Commit.**

```bash
git add internal/reporter/reporter.go internal/reporter/reporter_test.go
git commit -m "$(cat <<'EOF'
feat(reporter): render ## Possibly Covered in screenshots.md

Section appears between ## Missing Screenshots and ## Image Issues so the
file reads top-down by severity. Suppressed only when the slice is
non-empty (matches existing convention for ## Image Issues).

- RED: TestWriteScreenshotsRendersPossiblyCovered (header + content + ordering),
  TestWriteScreenshotsOmitsPossiblyCoveredWhenEmpty
- GREEN: new conditional block in WriteScreenshots
- Status: full reporter package green

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 11: Surface `PossiblyCovered` count in audit log

**Background:** The CLI prints a per-page audit line including counts like `images_seen=3 vision=on relevance_batches=1 image_issues=0 missing=2`. Adding `possibly_covered=N` here closes the visibility loop and lets verification confirm the route worked without inspecting the markdown.

**Files:**
- Modify: `internal/cli/analyze.go` — find the audit line formatting (search for `relevance_batches=`).
- Test: `internal/cli/analyze_vision_screenshot_test.go` (existing audit-line tests live here based on the file name pattern).

**Step 11.1: Locate the audit line formatter.**

```bash
grep -n "relevance_batches" internal/cli/analyze*.go
```

**Step 11.2: Write a failing test.**

Modify or extend an existing audit-line assertion to include `possibly_covered=N`. Match the existing test style — likely a table case asserting the formatted log line contains the substring.

**Step 11.3: Implement.**

Append `possibly_covered=%d` to the format string and pass `stats.PossiblyCovered`.

**Step 11.4: Run, see it pass.**

```bash
go test ./internal/cli/...
```

**Step 11.5: Coverage + lint.**

```bash
go test ./...
golangci-lint run ./...
```

**Step 11.6: Commit.**

```bash
git add internal/cli/analyze*.go
git commit -m "$(cat <<'EOF'
feat(cli): include possibly_covered in per-page audit log

- RED: extended existing audit-line assertion
- GREEN: appended possibly_covered=%d to the log format
- Status: full cli package green

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 12: Update `VERIFICATION_PLAN.md` Scenario 13

**Files:**
- Modify: `.plans/VERIFICATION_PLAN.md`.

**Step 12.1: Add a sub-case (d) to Scenario 13.**

After sub-case (c), append:

```markdown
4. **Sub-case (d) — Unanalyzable image suppression.** Pick a docs page on the same fixture site that contains either an animated GIF demo or a large SVG illustration in the same section as a multi-step UI flow. Re-run with `--llm-small=anthropic/claude-haiku-4-5 -v`. Inspect `<projectDir>/screenshots.md` and `<projectDir>/site/screenshots/`.

**Additional Success Criteria:**
- [ ] **(d)** `screenshots.md` renders a `## Possibly Covered` section listing the multi-step passage.
- [ ] **(d)** The corresponding passage does NOT appear under `## Missing Screenshots`.
- [ ] **(d)** `<projectDir>/site/screenshots/` renders the new section through Hextra (header visible in TOC, body rendered).
- [ ] **(d)** Audit log line for the page shows `possibly_covered >= 1`.
```

**Step 12.2: Commit.**

```bash
git add .plans/VERIFICATION_PLAN.md
git commit -m "$(cat <<'EOF'
docs(verification): add Scenario 13 sub-case (d) for suppression

Confirms the unanalyzable-image suppression layer routes a multi-step
passage to ## Possibly Covered when the section already has an animated
GIF or large SVG, both in screenshots.md and in the rendered Hugo site.

Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>
EOF
)"
```

---

## Task 13: Final coverage gate

**Step 13.1: Run the full test suite, race detector, lint.**

```bash
go test -race ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -20
golangci-lint run ./...
go build ./...
```

**Step 13.2: Confirm.**

- Every test green.
- `internal/analyzer/` and `internal/reporter/` package coverage ≥90%.
- No lint warnings.
- Build clean.

**Step 13.3: Update `PROGRESS.md`.**

Append a top-level entry for this feature with timestamp, completed tasks, and the coverage numbers from Step 13.1.

**Step 13.4: Commit `PROGRESS.md`.**

```bash
git add PROGRESS.md
git commit -m "docs(progress): unanalyzable image suppression complete"
```

---

## What is explicitly NOT in scope

- Changes to `internal/site/materialize.go` or the Hextra theme. The new subsection renders for free as plain markdown.
- Updates to `README.md` or other project documentation (the design locks scope to "A: generated Hugo site only").
- Range-GET fallback when `Content-Length` is missing. No-signal → no-suppression is the design's documented default.
- Animated-GIF detection. All GIFs go through suppression; we do not parse frame counts.
- A user-facing flag to disable suppression. If maintainers want every gap unconditionally, they can ignore the `## Possibly Covered` section.
