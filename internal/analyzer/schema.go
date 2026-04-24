package analyzer

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// JSONSchema describes the expected shape of a structured LLM response.
// Name is used to identify the schema to the provider (as a tool name for
// Anthropic or as the json_schema name for OpenAI). Doc is a raw JSON Schema
// document whose root MUST be an object with a "type" field.
type JSONSchema struct {
	Name string
	Doc  json.RawMessage
}

// Validate checks that Name is non-empty and Doc parses as a JSON object
// carrying a top-level "type" field. It does not otherwise verify the schema's
// internal correctness — providers will surface richer errors if needed.
func (s JSONSchema) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("JSONSchema: name is required")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(s.Doc, &obj); err != nil {
		return fmt.Errorf("JSONSchema %q: doc must be a JSON object: %w", s.Name, err)
	}
	if _, ok := obj["type"]; !ok {
		return fmt.Errorf("JSONSchema %q: doc must have a 'type' field at the root", s.Name)
	}
	return nil
}

// ValidateResponse checks that raw parses as JSON and conforms to s.Doc. It
// exists because some providers' structured-output contracts are advisory
// (Anthropic's tool input_schema, for example) — the model usually obeys but
// isn't guaranteed to. Every CompleteJSON implementation must call this before
// returning so callers can rely on the payload matching the declared shape.
func (s JSONSchema) ValidateResponse(raw json.RawMessage) error {
	compiler := jsonschema.NewCompiler()
	const schemaURL = "inline://schema.json"
	if err := compiler.AddResource(schemaURL, bytes.NewReader(s.Doc)); err != nil {
		return fmt.Errorf("JSONSchema %q: add resource: %w", s.Name, err)
	}
	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		return fmt.Errorf("JSONSchema %q: compile: %w", s.Name, err)
	}

	var payload any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // jsonschema/v5 requires json.Number for numeric validation
	if err := dec.Decode(&payload); err != nil {
		return fmt.Errorf("JSONSchema %q: response is not valid JSON: %w", s.Name, err)
	}

	if err := compiled.Validate(payload); err != nil {
		return fmt.Errorf("JSONSchema %q: response does not conform to schema: %w", s.Name, err)
	}
	return nil
}
