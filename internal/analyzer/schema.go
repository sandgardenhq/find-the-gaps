package analyzer

import (
	"encoding/json"
	"fmt"
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
