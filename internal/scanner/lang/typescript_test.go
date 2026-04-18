package lang

import (
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/scanner"
)

// --- exported function ---

func TestTypeScriptExtractor_exportedFunc_extracted(t *testing.T) {
	src := []byte(`/** Processes data. */
export function processData(x: string): number {
  return 0;
}

function internal() {}
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("mod.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "processData" {
		t.Errorf("name: got %q, want processData", syms[0].Name)
	}
	if syms[0].Kind != scanner.KindFunc {
		t.Errorf("kind: got %q, want func", syms[0].Kind)
	}
}

// --- exported class ---

func TestTypeScriptExtractor_exportedClass_extracted(t *testing.T) {
	src := []byte(`export class MyService {
  doSomething() {}
}
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("svc.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "MyService" || syms[0].Kind != scanner.KindClass {
		t.Errorf("got %+v", syms[0])
	}
}

// --- exported const ---

func TestTypeScriptExtractor_exportedConst_extracted(t *testing.T) {
	src := []byte(`export const MAX_RETRIES = 3;
const hidden = true;
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("config.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "MAX_RETRIES" || syms[0].Kind != scanner.KindConst {
		t.Errorf("got %+v", syms[0])
	}
}

// --- exported interface ---

func TestTypeScriptExtractor_exportedInterface_extracted(t *testing.T) {
	src := []byte(`export interface User {
  id: number;
  name: string;
}
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("types.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "User" || syms[0].Kind != scanner.KindInterface {
		t.Errorf("got %+v", syms[0])
	}
}

// --- exported type alias ---

func TestTypeScriptExtractor_exportedTypeAlias_extracted(t *testing.T) {
	src := []byte(`export type ID = string | number;
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("types.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "ID" || syms[0].Kind != scanner.KindType {
		t.Errorf("got %+v", syms[0])
	}
}

// --- default import ---

func TestTypeScriptExtractor_defaultImport_extracted(t *testing.T) {
	src := []byte(`import Foo from './foo';
`)
	e := &TypeScriptExtractor{}
	_, imps, err := e.Extract("mod.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Path != "./foo" {
		t.Errorf("path: got %q, want ./foo", imps[0].Path)
	}
}

// --- namespace import ---

func TestTypeScriptExtractor_namespaceImport_extracted(t *testing.T) {
	src := []byte(`import * as utils from './utils';
`)
	e := &TypeScriptExtractor{}
	_, imps, err := e.Extract("mod.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Path != "./utils" {
		t.Errorf("path: got %q, want ./utils", imps[0].Path)
	}
	if imps[0].Alias != "utils" {
		t.Errorf("alias: got %q, want utils", imps[0].Alias)
	}
}

// --- named imports ---

func TestTypeScriptExtractor_namedImports_extracted(t *testing.T) {
	src := []byte(`import { readFile, writeFile } from 'fs';
`)
	e := &TypeScriptExtractor{}
	_, imps, err := e.Extract("mod.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// Each named import is one entry sharing the same module path.
	if len(imps) < 1 {
		t.Fatalf("expected at least 1 import, got %d: %v", len(imps), imps)
	}
	// All should point to 'fs'.
	for _, imp := range imps {
		if imp.Path != "fs" {
			t.Errorf("path: got %q, want fs", imp.Path)
		}
	}
}

// --- aliased named import ---

func TestTypeScriptExtractor_namedImportAlias_extracted(t *testing.T) {
	src := []byte(`import { readFile as rf } from 'fs';
`)
	e := &TypeScriptExtractor{}
	_, imps, err := e.Extract("mod.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Alias != "rf" {
		t.Errorf("alias: got %q, want rf", imps[0].Alias)
	}
}

// --- JSDoc comment ---

func TestTypeScriptExtractor_jsdocComment_extracted(t *testing.T) {
	src := []byte(`/** Sends a request to the server. */
export function sendRequest(url: string): void {}
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("api.ts", src)
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

// --- empty file ---

func TestTypeScriptExtractor_emptyFile_noError(t *testing.T) {
	e := &TypeScriptExtractor{}
	syms, imps, err := e.Extract("empty.ts", []byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syms == nil {
		t.Errorf("expected non-nil symbols slice, got nil")
	}
	if imps == nil {
		t.Errorf("expected non-nil imports slice, got nil")
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(syms))
	}
	if len(imps) != 0 {
		t.Errorf("expected 0 imports, got %d", len(imps))
	}
}

// --- line number ---

func TestTypeScriptExtractor_lineNumber_recorded(t *testing.T) {
	src := []byte(`export function first(): void {}

export function second(): void {}
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("mod.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols, got %d: %v", len(syms), syms)
	}
	if syms[0].Line != 1 {
		t.Errorf("first line: got %d, want 1", syms[0].Line)
	}
	if syms[1].Line != 3 {
		t.Errorf("second line: got %d, want 3", syms[1].Line)
	}
}

// --- language + extensions ---

func TestTypeScriptExtractor_language(t *testing.T) {
	e := &TypeScriptExtractor{}
	if e.Language() != "TypeScript" {
		t.Errorf("got %q, want TypeScript", e.Language())
	}
}

func TestTypeScriptExtractor_extensions(t *testing.T) {
	e := &TypeScriptExtractor{}
	exts := e.Extensions()
	want := map[string]bool{".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true}
	for _, ext := range exts {
		delete(want, ext)
	}
	if len(want) > 0 {
		t.Errorf("missing extensions: %v", want)
	}
}

// --- JavaScript grammar used for .js ---

func TestTypeScriptExtractor_jsFile_exportedFunc(t *testing.T) {
	src := []byte(`export function greet(name) {
  return 'Hello, ' + name;
}
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("greet.js", src)
	if err != nil {
		t.Fatalf("Extract (.js): %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "greet" || syms[0].Kind != scanner.KindFunc {
		t.Errorf("got %+v", syms[0])
	}
}

// --- export let produces KindConst (const/let are both lexical_declaration) ---

func TestTypeScriptExtractor_exportedLet_extracted(t *testing.T) {
	src := []byte(`export let defaultTimeout = 5000;
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("mod.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), syms)
	}
	if syms[0].Name != "defaultTimeout" {
		t.Errorf("name: got %q", syms[0].Name)
	}
	if syms[0].Kind != scanner.KindConst {
		t.Errorf("kind: got %q, want const", syms[0].Kind)
	}
}

// --- side-effect import (import 'module') ---

func TestTypeScriptExtractor_sideEffectImport_extracted(t *testing.T) {
	src := []byte(`import './polyfills';
`)
	e := &TypeScriptExtractor{}
	_, imps, err := e.Extract("mod.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(imps) != 1 {
		t.Fatalf("expected 1 import, got %d: %v", len(imps), imps)
	}
	if imps[0].Path != "./polyfills" {
		t.Errorf("path: got %q, want ./polyfills", imps[0].Path)
	}
}

// --- signature populated ---

func TestTypeScriptExtractor_signature_recorded(t *testing.T) {
	src := []byte(`export function add(a: number, b: number): number {
  return a + b;
}
`)
	e := &TypeScriptExtractor{}
	syms, _, err := e.Extract("math.ts", src)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("expected 1 symbol")
	}
	if syms[0].Signature == "" {
		t.Errorf("expected non-empty signature, got empty")
	}
}
