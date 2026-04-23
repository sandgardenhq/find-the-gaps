package analyzer_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestJSONSchema_Validate_ObjectWithType_OK(t *testing.T) {
	s := analyzer.JSONSchema{
		Name: "foo",
		Doc:  json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`),
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
}

func TestJSONSchema_Validate_EmptyName_ReturnsError(t *testing.T) {
	s := analyzer.JSONSchema{
		Name: "",
		Doc:  json.RawMessage(`{"type":"object"}`),
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention 'name', got: %v", err)
	}
}

func TestJSONSchema_Validate_MalformedDoc_ReturnsError(t *testing.T) {
	s := analyzer.JSONSchema{
		Name: "foo",
		Doc:  json.RawMessage(`not json`),
	}
	if err := s.Validate(); err == nil {
		t.Fatal("expected error for malformed JSON doc")
	}
}

func TestJSONSchema_Validate_NonObjectDoc_ReturnsError(t *testing.T) {
	s := analyzer.JSONSchema{
		Name: "foo",
		Doc:  json.RawMessage(`[1,2,3]`),
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for non-object JSON doc")
	}
	if !strings.Contains(err.Error(), "object") {
		t.Errorf("error should mention 'object', got: %v", err)
	}
}

func TestJSONSchema_Validate_MissingTypeField_ReturnsError(t *testing.T) {
	s := analyzer.JSONSchema{
		Name: "foo",
		Doc:  json.RawMessage(`{"properties":{"x":{"type":"string"}}}`),
	}
	err := s.Validate()
	if err == nil {
		t.Fatal("expected error for missing 'type' field")
	}
	if !strings.Contains(err.Error(), "type") {
		t.Errorf("error should mention 'type', got: %v", err)
	}
}
