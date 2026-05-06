package analyzer_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sandgardenhq/find-the-gaps/internal/analyzer"
)

func TestAnalyzePageSchema_IncludesIsDocsField(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(analyzer.AnalyzePageSchemaForTest().Doc, &doc); err != nil {
		t.Fatalf("schema doc must be valid JSON: %v", err)
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema must have properties object")
	}
	isDocs, ok := props["is_docs"].(map[string]any)
	if !ok {
		t.Fatal("schema must declare is_docs property")
	}
	if isDocs["type"] != "boolean" {
		t.Errorf("is_docs type: got %v, want boolean", isDocs["type"])
	}
	required, ok := doc["required"].([]any)
	if !ok {
		t.Fatal("schema must have required array")
	}
	found := false
	for _, r := range required {
		if r == "is_docs" {
			found = true
		}
	}
	if !found {
		t.Error("is_docs must be in required[]")
	}
}

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
	cleaned, err := s.ValidateResponse(raw)
	if err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
	if len(cleaned) == 0 {
		t.Fatal("cleaned payload must be non-empty")
	}
}

func TestJSONSchema_ValidateResponse_MissingRequired_ReturnsError(t *testing.T) {
	s := analyzeResponseSchema()
	raw := json.RawMessage(`{"summary":"ok"}`) // "features" missing
	_, err := s.ValidateResponse(raw)
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
	if _, err := s.ValidateResponse(raw); err == nil {
		t.Fatal("expected error for wrong type")
	}
}

// Anthropic's tool input_schema is advisory: the model occasionally hallucinates
// extra keys (e.g. `issue_dup_of` in drift.go's judge) even with
// additionalProperties:false. ValidateResponse must strip unknown top-level
// fields rather than fail the whole call, since the typed Go unmarshal that
// follows ignores unknown fields anyway.
func TestJSONSchema_ValidateResponse_StripsTopLevelExtraField(t *testing.T) {
	s := analyzeResponseSchema()
	raw := json.RawMessage(`{"summary":"ok","features":[],"extra":"nope"}`)
	cleaned, err := s.ValidateResponse(raw)
	if err != nil {
		t.Fatalf("expected strip-and-pass, got error: %v", err)
	}
	if strings.Contains(string(cleaned), "extra") {
		t.Errorf("expected 'extra' stripped from cleaned payload, got: %s", cleaned)
	}
	if !strings.Contains(string(cleaned), `"summary"`) || !strings.Contains(string(cleaned), `"features"`) {
		t.Errorf("expected summary+features preserved, got: %s", cleaned)
	}
}

// Mirrors the real failure: schema declares an items object with
// additionalProperties:false; the LLM returns one item with an unknown key.
// The cleaned payload must drop the unknown key in every array element.
func TestJSONSchema_ValidateResponse_StripsExtraFieldInArrayItems(t *testing.T) {
	s := analyzer.JSONSchema{
		Name: "drift_judge_issues",
		Doc: json.RawMessage(`{
			"type": "object",
			"properties": {
				"issues": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"page":  {"type": "string"},
							"issue": {"type": "string"}
						},
						"required": ["page", "issue"],
						"additionalProperties": false
					}
				}
			},
			"required": ["issues"],
			"additionalProperties": false
		}`),
	}
	raw := json.RawMessage(`{"issues":[
		{"page":"https://docs/x","issue":"docs claim X","issue_dup_of":2},
		{"page":"https://docs/y","issue":"docs claim Y","note":"merged"}
	]}`)
	cleaned, err := s.ValidateResponse(raw)
	if err != nil {
		t.Fatalf("expected strip-and-pass, got error: %v", err)
	}
	if strings.Contains(string(cleaned), "issue_dup_of") {
		t.Errorf("expected issue_dup_of stripped, got: %s", cleaned)
	}
	if strings.Contains(string(cleaned), `"note"`) {
		t.Errorf("expected note stripped, got: %s", cleaned)
	}
	// Required fields must survive the strip.
	if !strings.Contains(string(cleaned), "https://docs/x") || !strings.Contains(string(cleaned), "https://docs/y") {
		t.Errorf("expected both items preserved, got: %s", cleaned)
	}
}

func TestJSONSchema_ValidateResponse_MalformedJSON_ReturnsError(t *testing.T) {
	s := analyzeResponseSchema()
	raw := json.RawMessage(`not json at all`)
	_, err := s.ValidateResponse(raw)
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
	if _, err := s.ValidateResponse(raw); err == nil {
		t.Fatal("expected error when schema doc is malformed")
	}
}
