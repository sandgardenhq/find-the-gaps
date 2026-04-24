package lang

import (
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// --- public func ---

func TestSwiftExtractor_publicFunc_extracted(t *testing.T) {
	src := []byte(`public func greet() {}
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("Greeter.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var fn *types.Symbol
	for i := range syms {
		if syms[i].Name == "greet" {
			fn = &syms[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("expected a symbol named 'greet', got: %v", syms)
	}
	if fn.Kind != types.KindFunc {
		t.Errorf("kind: got %q, want func", fn.Kind)
	}
	if fn.Line != 1 {
		t.Errorf("line: got %d, want 1", fn.Line)
	}
}

// --- open func ---

func TestSwiftExtractor_openFunc_extracted(t *testing.T) {
	src := []byte(`open func greet() {}
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("Greeter.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var fn *types.Symbol
	for i := range syms {
		if syms[i].Name == "greet" {
			fn = &syms[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("expected a symbol named 'greet', got: %v", syms)
	}
	if fn.Kind != types.KindFunc {
		t.Errorf("kind: got %q, want func", fn.Kind)
	}
}

// --- internal/default skipped ---

func TestSwiftExtractor_internalSkipped(t *testing.T) {
	src := []byte(`public func visible() {}
internal func hidden() {}
func alsoHidden() {}
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("X.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["visible"] {
		t.Errorf("expected 'visible' to be emitted, got: %v", names)
	}
	for _, forbidden := range []string{"hidden", "alsoHidden"} {
		if names[forbidden] {
			t.Errorf("expected %q to be skipped, but it was emitted", forbidden)
		}
	}
}

// --- fileprivate skipped ---

func TestSwiftExtractor_fileprivateSkipped(t *testing.T) {
	src := []byte(`public func visible() {}
fileprivate func shielded() {}
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("X.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s.Name] = true
	}
	if !names["visible"] {
		t.Errorf("expected 'visible' to be emitted, got: %v", names)
	}
	if names["shielded"] {
		t.Errorf("expected 'shielded' to be skipped, but it was emitted")
	}
}

// --- class ---

func TestSwiftExtractor_class_extracted(t *testing.T) {
	src := []byte(`public class Greeter {}
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("Greeter.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var cls *types.Symbol
	for i := range syms {
		if syms[i].Name == "Greeter" {
			cls = &syms[i]
			break
		}
	}
	if cls == nil {
		t.Fatalf("expected a symbol named 'Greeter', got: %v", syms)
	}
	if cls.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", cls.Kind)
	}
}

// --- struct ---

func TestSwiftExtractor_struct_extracted(t *testing.T) {
	src := []byte(`public struct Point {}
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("Point.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var s *types.Symbol
	for i := range syms {
		if syms[i].Name == "Point" {
			s = &syms[i]
			break
		}
	}
	if s == nil {
		t.Fatalf("expected a symbol named 'Point', got: %v", syms)
	}
	if s.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class", s.Kind)
	}
}

// --- enum ---

func TestSwiftExtractor_enum_extracted(t *testing.T) {
	src := []byte(`public enum Status {
    case active
    case inactive
}
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("Status.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var s *types.Symbol
	for i := range syms {
		if syms[i].Name == "Status" {
			s = &syms[i]
			break
		}
	}
	if s == nil {
		t.Fatalf("expected a symbol named 'Status', got: %v", syms)
	}
	if s.Kind != types.KindClass {
		t.Errorf("kind: got %q, want class (per plan mapping)", s.Kind)
	}
}

// --- protocol ---

func TestSwiftExtractor_protocol_extracted(t *testing.T) {
	src := []byte(`public protocol Repo {}
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("Repo.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var s *types.Symbol
	for i := range syms {
		if syms[i].Name == "Repo" {
			s = &syms[i]
			break
		}
	}
	if s == nil {
		t.Fatalf("expected a symbol named 'Repo', got: %v", syms)
	}
	if s.Kind != types.KindInterface {
		t.Errorf("kind: got %q, want interface", s.Kind)
	}
}

// --- imports ---

func TestSwiftExtractor_imports_extracted(t *testing.T) {
	src := []byte(`import Foundation
import UIKit
`)
	e := &SwiftExtractor{}
	_, imps, err := e.Extract("X.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 2 {
		t.Fatalf("expected 2 imports, got %d: %v", len(imps), imps)
	}
	seen := map[string]bool{}
	for _, i := range imps {
		seen[i.Path] = true
	}
	if !seen["Foundation"] {
		t.Errorf("expected import path 'Foundation', got: %v", imps)
	}
	if !seen["UIKit"] {
		t.Errorf("expected import path 'UIKit', got: %v", imps)
	}
}

// --- /// triple-slash doc block ---

func TestSwiftExtractor_tripleSlashDoc_captured(t *testing.T) {
	src := []byte(`/// Adds two numbers.
/// Returns the sum.
/// Third line.
public func add(_ a: Int, _ b: Int) -> Int { return a + b }
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("Math.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var fn *types.Symbol
	for i := range syms {
		if syms[i].Name == "add" {
			fn = &syms[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("expected a symbol named 'add', got: %v", syms)
	}
	if fn.DocComment == "" {
		t.Errorf("expected non-empty DocComment, got empty")
	}
	if strings.Contains(fn.DocComment, "///") {
		t.Errorf("DocComment still contains `///` prefix: %q", fn.DocComment)
	}
	if !strings.Contains(fn.DocComment, "Adds two numbers.") {
		t.Errorf("DocComment missing first line: %q", fn.DocComment)
	}
	if !strings.Contains(fn.DocComment, "Returns the sum.") {
		t.Errorf("DocComment missing second line: %q", fn.DocComment)
	}
	if !strings.Contains(fn.DocComment, "Third line.") {
		t.Errorf("DocComment missing third line: %q", fn.DocComment)
	}
}

// --- public property → KindVar ---

func TestSwiftExtractor_property_extracted(t *testing.T) {
	src := []byte(`public var maxRetries: Int = 3
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("Config.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var v *types.Symbol
	for i := range syms {
		if syms[i].Name == "maxRetries" {
			v = &syms[i]
			break
		}
	}
	if v == nil {
		t.Fatalf("expected a symbol named 'maxRetries', got: %v", syms)
	}
	if v.Kind != types.KindVar {
		t.Errorf("kind: got %q, want var", v.Kind)
	}
}

// --- /** */ block doc ---

func TestSwiftExtractor_blockDoc_captured(t *testing.T) {
	src := []byte(`/** Subtracts two numbers. */
public func sub(_ a: Int, _ b: Int) -> Int { return a - b }
`)
	e := &SwiftExtractor{}
	syms, _, err := e.Extract("Math.swift", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	var fn *types.Symbol
	for i := range syms {
		if syms[i].Name == "sub" {
			fn = &syms[i]
			break
		}
	}
	if fn == nil {
		t.Fatalf("expected a symbol named 'sub', got: %v", syms)
	}
	if fn.DocComment == "" {
		t.Errorf("expected non-empty DocComment, got empty")
	}
	if strings.Contains(fn.DocComment, "/**") || strings.Contains(fn.DocComment, "*/") {
		t.Errorf("DocComment still contains `/**` or `*/` markers: %q", fn.DocComment)
	}
	if !strings.Contains(fn.DocComment, "Subtracts two numbers.") {
		t.Errorf("DocComment missing text: %q", fn.DocComment)
	}
}
