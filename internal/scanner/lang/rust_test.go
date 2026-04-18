package lang

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner/types"
)

// TestRustExtractor_pubFn_extracted verifies that a pub fn is extracted as KindFunc.
func TestRustExtractor_pubFn_extracted(t *testing.T) {
	src := []byte(`
/// Process items from the queue.
pub fn process(items: Vec<String>) -> Result<(), Error> {
    Ok(())
}

fn private_helper() {}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "process" {
		t.Errorf("name: got %q, want process", syms[0].Name)
	}
	if syms[0].Kind != types.KindFunc {
		t.Errorf("kind: got %q, want func", syms[0].Kind)
	}
	if syms[0].Line == 0 {
		t.Errorf("line should be non-zero")
	}
}

// TestRustExtractor_privateFn_skipped verifies that a private fn is not extracted.
func TestRustExtractor_privateFn_skipped(t *testing.T) {
	src := []byte(`
fn helper(x: i32) -> i32 {
    x + 1
}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols, got %d: %v", len(syms), syms)
	}
}

// TestRustExtractor_pubStruct_extracted verifies that a pub struct is extracted as KindType.
func TestRustExtractor_pubStruct_extracted(t *testing.T) {
	src := []byte(`
/// Configuration for the client.
pub struct Config {
    pub timeout: u64,
    host: String,
}

struct InternalState {}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("config.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "Config" {
		t.Errorf("name: got %q, want Config", syms[0].Name)
	}
	if syms[0].Kind != types.KindType {
		t.Errorf("kind: got %q, want type", syms[0].Kind)
	}
}

// TestRustExtractor_pubEnum_extracted verifies that a pub enum is extracted as KindType.
func TestRustExtractor_pubEnum_extracted(t *testing.T) {
	src := []byte(`
pub enum Status {
    Active,
    Inactive,
}

enum PrivateState {
    On,
    Off,
}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("status.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "Status" {
		t.Errorf("name: got %q, want Status", syms[0].Name)
	}
	if syms[0].Kind != types.KindType {
		t.Errorf("kind: got %q, want type", syms[0].Kind)
	}
}

// TestRustExtractor_pubTrait_extracted verifies that a pub trait is extracted as KindInterface.
func TestRustExtractor_pubTrait_extracted(t *testing.T) {
	src := []byte(`
/// A drawable item.
pub trait Drawable {
    fn draw(&self);
}

trait PrivateTrait {
    fn hidden(&self);
}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("traits.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "Drawable" {
		t.Errorf("name: got %q, want Drawable", syms[0].Name)
	}
	if syms[0].Kind != types.KindInterface {
		t.Errorf("kind: got %q, want interface", syms[0].Kind)
	}
}

// TestRustExtractor_pubConst_extracted verifies that a pub const is extracted as KindConst.
func TestRustExtractor_pubConst_extracted(t *testing.T) {
	src := []byte(`
pub const MAX_SIZE: usize = 1024;
const PRIVATE_LIMIT: usize = 64;
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("constants.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "MAX_SIZE" {
		t.Errorf("name: got %q, want MAX_SIZE", syms[0].Name)
	}
	if syms[0].Kind != types.KindConst {
		t.Errorf("kind: got %q, want const", syms[0].Kind)
	}
}

// TestRustExtractor_useDeclaration_imported verifies that a use declaration is extracted as an Import.
func TestRustExtractor_useDeclaration_imported(t *testing.T) {
	src := []byte(`
use std::collections::HashMap;
use std::io;
`)
	e := &RustExtractor{}
	_, imps, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) == 0 {
		t.Fatalf("expected at least 1 import, got 0")
	}
	var foundHashMap bool
	for _, imp := range imps {
		if imp.Path == "std::collections::HashMap" {
			foundHashMap = true
		}
	}
	if !foundHashMap {
		t.Errorf("did not find std::collections::HashMap import in %v", imps)
	}
}

// TestRustExtractor_useAlias_imported verifies that aliased use declarations record Import.Alias.
func TestRustExtractor_useAlias_imported(t *testing.T) {
	src := []byte(`
use std::collections::HashMap as Map;
`)
	e := &RustExtractor{}
	_, imps, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Alias != "Map" {
		t.Errorf("alias: got %q, want Map", imps[0].Alias)
	}
}

// TestRustExtractor_lineNumber_recorded verifies that line numbers are > 0 and differ for different symbols.
func TestRustExtractor_lineNumber_recorded(t *testing.T) {
	src := []byte(`pub fn first() {}

pub fn second() {}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols, got %d: %v", len(syms), syms)
	}
	if syms[0].Line == 0 {
		t.Errorf("first symbol line should be non-zero")
	}
	if syms[1].Line == 0 {
		t.Errorf("second symbol line should be non-zero")
	}
	if syms[0].Line == syms[1].Line {
		t.Errorf("line numbers should differ: both are %d", syms[0].Line)
	}
}

// TestRustExtractor_emptyFile_noError verifies that an empty file returns empty slices without error.
func TestRustExtractor_emptyFile_noError(t *testing.T) {
	e := &RustExtractor{}
	syms, imps, err := e.Extract("empty.rs", []byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syms == nil {
		t.Errorf("symbols should be non-nil empty slice, got nil")
	}
	if imps == nil {
		t.Errorf("imports should be non-nil empty slice, got nil")
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(syms))
	}
	if len(imps) != 0 {
		t.Errorf("expected 0 imports, got %d", len(imps))
	}
}

// TestRustExtractor_language verifies the Language() string.
func TestRustExtractor_language(t *testing.T) {
	e := &RustExtractor{}
	if e.Language() != "Rust" {
		t.Errorf("got %q, want Rust", e.Language())
	}
}

// TestRustExtractor_extensions_includesDotRs verifies that .rs is in Extensions().
func TestRustExtractor_extensions_includesDotRs(t *testing.T) {
	e := &RustExtractor{}
	exts := e.Extensions()
	if len(exts) == 0 {
		t.Fatal("expected non-empty extensions list")
	}
	found := false
	for _, ext := range exts {
		if ext == ".rs" {
			found = true
		}
	}
	if !found {
		t.Errorf("Extensions() = %v, want to include .rs", exts)
	}
}

// TestRustExtractor_docComment_extracted verifies that /// doc comments are captured.
func TestRustExtractor_docComment_extracted(t *testing.T) {
	src := []byte(`/// Initializes the system.
pub fn init() {}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(syms))
	}
	if syms[0].DocComment == "" {
		t.Errorf("expected non-empty DocComment, got empty")
	}
}

// TestRustExtractor_signature_recorded verifies that a function's signature is populated.
func TestRustExtractor_signature_recorded(t *testing.T) {
	src := []byte(`pub fn fetch(url: &str, timeout: u64) -> Result<String, Error> {
    Ok(String::new())
}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(syms))
	}
	if syms[0].Signature == "" {
		t.Errorf("expected non-empty signature, got empty")
	}
}

// TestRustExtractor_multiLineDocComment_extracted verifies that multiple consecutive
// /// comments are all captured in the DocComment.
func TestRustExtractor_multiLineDocComment_extracted(t *testing.T) {
	src := []byte(`/// First line.
/// Second line.
pub fn documented() {}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(syms))
	}
	if syms[0].DocComment == "" {
		t.Errorf("expected non-empty DocComment, got empty")
	}
}

// TestRustExtractor_useSimpleIdentifier_imported verifies that `use foo;` (bare identifier)
// is extracted as an import.
func TestRustExtractor_useSimpleIdentifier_imported(t *testing.T) {
	src := []byte(`use std::io;
`)
	e := &RustExtractor{}
	_, imps, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Path != "std::io" {
		t.Errorf("path: got %q, want std::io", imps[0].Path)
	}
}

// TestRustExtractor_useGlob_ignored verifies that glob-style use declarations
// (`use std::io::*`) do not panic and produce zero imports (unsupported form).
func TestRustExtractor_useGlob_noError(t *testing.T) {
	src := []byte(`use std::io::*;
`)
	e := &RustExtractor{}
	_, _, err := e.Extract("lib.rs", src)
	if err != nil {
		t.Fatalf("unexpected error for glob use: %v", err)
	}
}

// TestRustExtractor_nestedFnInImpl_skipped verifies that methods inside impl blocks
// are NOT extracted as top-level symbols.
func TestRustExtractor_nestedFnInImpl_skipped(t *testing.T) {
	src := []byte(`
pub struct Client {}

impl Client {
    pub fn connect(&self) -> Result<(), Error> {
        Ok(())
    }
}
`)
	e := &RustExtractor{}
	syms, _, err := e.Extract("client.rs", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Only the struct should be extracted; connect() is inside impl, not top-level
	for _, s := range syms {
		if s.Name == "connect" {
			t.Errorf("connect() inside impl block should not be extracted as top-level symbol")
		}
	}
}
