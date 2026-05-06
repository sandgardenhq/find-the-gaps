package analyzer

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/log"
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

// ValidateResponse checks that raw parses as JSON and conforms to s.Doc, and
// returns a (possibly cleaned) payload safe to hand to a typed json.Unmarshal.
//
// Anthropic's tool input_schema is documented as advisory — the model usually
// honors additionalProperties:false but occasionally hallucinates extra keys
// (we have seen drift's judge invent `issue_dup_of`). To avoid failing entire
// runs on a stray field, ValidateResponse strips unknown properties from any
// object whose schema has additionalProperties:false, then re-validates. All
// other failure modes (missing required, wrong type, bad enum, malformed JSON,
// invalid schema doc) still fail closed.
//
// The returned bytes match the input on a clean response and are a normalized
// re-encoding when stripping occurred.
func (s JSONSchema) ValidateResponse(raw json.RawMessage) (json.RawMessage, error) {
	compiler := jsonschema.NewCompiler()
	const schemaURL = "inline://schema.json"
	if err := compiler.AddResource(schemaURL, bytes.NewReader(s.Doc)); err != nil {
		return nil, fmt.Errorf("JSONSchema %q: add resource: %w", s.Name, err)
	}
	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("JSONSchema %q: compile: %w", s.Name, err)
	}

	var payload any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // jsonschema/v5 requires json.Number for numeric validation
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("JSONSchema %q: response is not valid JSON: %w", s.Name, err)
	}

	var schemaDoc map[string]any
	if err := json.Unmarshal(s.Doc, &schemaDoc); err != nil {
		return nil, fmt.Errorf("JSONSchema %q: re-parse schema: %w", s.Name, err)
	}
	stripped := stripUnknownProperties(schemaDoc, payload, s.Name)

	if err := compiled.Validate(stripped); err != nil {
		return nil, fmt.Errorf("JSONSchema %q: response does not conform to schema: %w", s.Name, err)
	}

	cleaned, err := json.Marshal(stripped)
	if err != nil {
		return nil, fmt.Errorf("JSONSchema %q: re-marshal cleaned payload: %w", s.Name, err)
	}
	return cleaned, nil
}

// stripUnknownProperties walks payload in lockstep with schemaNode and removes
// any object key not declared in the corresponding "properties" map when that
// schema node has additionalProperties:false. Anything else is returned
// untouched. Stripped keys are debug-logged so operators can spot prompt
// regressions without the run failing.
//
// The walker handles only the schema features we actually use: object with
// "properties" + "additionalProperties", and array with a single "items"
// schema. $ref, oneOf/anyOf/allOf, and tuple-style items are out of scope.
func stripUnknownProperties(schemaNode any, payload any, schemaName string) any {
	schemaMap, ok := schemaNode.(map[string]any)
	if !ok {
		return payload
	}

	switch schemaMap["type"] {
	case "object":
		obj, ok := payload.(map[string]any)
		if !ok {
			return payload
		}
		props, _ := schemaMap["properties"].(map[string]any)
		strict := false
		if v, present := schemaMap["additionalProperties"]; present {
			if b, isBool := v.(bool); isBool && !b {
				strict = true
			}
		}
		for key, val := range obj {
			propSchema, known := props[key]
			if !known {
				if strict {
					log.Debugf("JSONSchema %q: stripping unknown property %q from response", schemaName, key)
					delete(obj, key)
				}
				continue
			}
			obj[key] = stripUnknownProperties(propSchema, val, schemaName)
		}
		return obj
	case "array":
		arr, ok := payload.([]any)
		if !ok {
			return payload
		}
		items, ok := schemaMap["items"].(map[string]any)
		if !ok {
			return payload
		}
		for i, elem := range arr {
			arr[i] = stripUnknownProperties(items, elem, schemaName)
		}
		return arr
	default:
		return payload
	}
}
