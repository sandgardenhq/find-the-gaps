package lang

import "testing"

func TestGenericExtractor_returnsEmptySlices_notNil(t *testing.T) {
	e := &GenericExtractor{}
	syms, imps, err := e.Extract("file.xyz", []byte("some content here"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if syms == nil {
		t.Errorf("expected non-nil (empty) symbols slice, got nil")
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(syms))
	}
	if imps == nil {
		t.Errorf("expected non-nil (empty) imports slice, got nil")
	}
	if len(imps) != 0 {
		t.Errorf("expected 0 imports, got %d", len(imps))
	}
}

func TestGenericExtractor_languageIsGeneric(t *testing.T) {
	e := &GenericExtractor{}
	if e.Language() != "Generic" {
		t.Errorf("got %q, want Generic", e.Language())
	}
}

func TestGenericExtractor_emptyContent_noError(t *testing.T) {
	e := &GenericExtractor{}
	syms, imps, err := e.Extract("Makefile", []byte{})
	if err != nil {
		t.Fatalf("unexpected error on empty content: %v", err)
	}
	if syms == nil {
		t.Errorf("expected non-nil (empty) symbols slice, got nil")
	}
	if imps == nil {
		t.Errorf("expected non-nil (empty) imports slice, got nil")
	}
}
