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

// --- ValidateResponse tests ---

func analyzeResponseSchema() analyzer.JSONSchema {
	return analyzer.JSONSchema{
		Name: "analyze_response",
		Doc: json.RawMessage(`{
			"type": "object",
			"properties": {
				"summary":  {"type": "string"},
				"features": {"type": "array", "items": {"type": "string"}}
			},
			"required": ["summary", "features"],
			"additionalProperties": false
		}`),
	}
}

func TestJSONSchema_ValidateResponse_ConformingPayload_OK(t *testing.T) {
	s := analyzeResponseSchema()
	raw := json.RawMessage(`{"summary":"ok","features":["a","b"]}`)
	if err := s.ValidateResponse(raw); err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
}

func TestJSONSchema_ValidateResponse_MissingRequired_ReturnsError(t *testing.T) {
	s := analyzeResponseSchema()
	raw := json.RawMessage(`{"summary":"ok"}`) // "features" missing
	err := s.ValidateResponse(raw)
	if err == nil {
		t.Fatal("expected error for missing required field")
	}
	if !strings.Contains(err.Error(), "analyze_response") {
		t.Errorf("error should identify schema by name, got: %v", err)
	}
}

func TestJSONSchema_ValidateResponse_WrongType_ReturnsError(t *testing.T) {
	s := analyzeResponseSchema()
	raw := json.RawMessage(`{"summary":"ok","features":"not-an-array"}`)
	if err := s.ValidateResponse(raw); err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestJSONSchema_ValidateResponse_AdditionalProperties_ReturnsError(t *testing.T) {
	s := analyzeResponseSchema()
	raw := json.RawMessage(`{"summary":"ok","features":[],"extra":"nope"}`)
	if err := s.ValidateResponse(raw); err == nil {
		t.Fatal("expected error when additionalProperties=false is violated")
	}
}

func TestJSONSchema_ValidateResponse_MalformedJSON_ReturnsError(t *testing.T) {
	s := analyzeResponseSchema()
	raw := json.RawMessage(`not json at all`)
	err := s.ValidateResponse(raw)
	if err == nil {
		t.Fatal("expected error for malformed JSON payload")
	}
}

func TestJSONSchema_ValidateResponse_InvalidSchemaDoc_ReturnsError(t *testing.T) {
	// Schema.Doc itself is not a valid JSON Schema — ValidateResponse must surface
	// this clearly instead of silently passing.
	s := analyzer.JSONSchema{
		Name: "bad",
		Doc:  json.RawMessage(`{"type":"object","properties":"this-should-be-an-object"}`),
	}
	raw := json.RawMessage(`{}`)
	if err := s.ValidateResponse(raw); err == nil {
		t.Fatal("expected error when schema doc is malformed")
	}
}
