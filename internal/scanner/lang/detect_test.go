package lang

import "testing"

func TestDetect_goFile_returnsGoExtractor(t *testing.T) {
	e := Detect("internal/foo/bar.go")
	if e == nil {
		t.Fatal("expected non-nil extractor for .go")
	}
	if e.Language() != "Go" {
		t.Errorf("got %q, want Go", e.Language())
	}
}

func TestDetect_tsFile_returnsTypeScriptExtractor(t *testing.T) {
	e := Detect("src/index.ts")
	if e == nil || e.Language() != "TypeScript" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_jsFile_returnsTypeScriptExtractor(t *testing.T) {
	e := Detect("src/util.js")
	if e == nil || e.Language() != "TypeScript" {
		t.Errorf("expected TypeScript extractor for .js, got %v", e)
	}
}

func TestDetect_pyFile_returnsPythonExtractor(t *testing.T) {
	e := Detect("app/main.py")
	if e == nil || e.Language() != "Python" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_rsFile_returnsRustExtractor(t *testing.T) {
	e := Detect("src/lib.rs")
	if e == nil || e.Language() != "Rust" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_javaFile_returnsJavaExtractor(t *testing.T) {
	e := Detect("src/Main.java")
	if e == nil || e.Language() != "Java" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_csFile_returnsCSharpExtractor(t *testing.T) {
	e := Detect("src/Main.cs")
	if e == nil || e.Language() != "C#" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_ktFile_returnsKotlinExtractor(t *testing.T) {
	e := Detect("src/Main.kt")
	if e == nil || e.Language() != "Kotlin" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_ktsFile_returnsKotlinExtractor(t *testing.T) {
	e := Detect("build.gradle.kts")
	if e == nil || e.Language() != "Kotlin" {
		t.Errorf("got %v", e)
	}
}

func TestDetect_unknownExtension_returnsGeneric(t *testing.T) {
	e := Detect("Makefile")
	if e == nil {
		t.Fatal("expected non-nil extractor for unknown file")
	}
	if e.Language() != "Generic" {
		t.Errorf("got %q, want Generic", e.Language())
	}
}

func TestDetect_binaryExtension_returnsNil(t *testing.T) {
	for _, name := range []string{"image.png", "data.zip", "font.ttf"} {
		if e := Detect(name); e != nil {
			t.Errorf("Detect(%q): expected nil for binary, got %v", name, e.Language())
		}
	}
}
