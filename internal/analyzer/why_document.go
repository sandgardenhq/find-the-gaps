package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type whyDocumentResponse struct {
	Rationales []whyDocumentRationale `json:"rationales"`
}

type whyDocumentRationale struct {
	Name      string `json:"name"`
	Rationale string `json:"rationale"`
}

// PROMPT SCHEMA: output shape for WhyDocument.
var whyDocumentSchema = JSONSchema{
	Name: "why_document_response",
	Doc: json.RawMessage(`{
      "type": "object",
      "properties": {
        "rationales": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "name":      {"type": "string"},
              "rationale": {"type": "string"}
            },
            "required": ["name", "rationale"],
            "additionalProperties": false
          }
        }
      },
      "required": ["rationales"],
      "additionalProperties": false
    }`),
}

// WhyDocument asks the small tier to produce a short, feature-specific
// rationale explaining why each undocumented user-facing feature is worth
// documenting. The returned map is keyed by feature name; entries the model
// did not speak to are simply absent and callers should fall back to a
// generic blurb.
//
// An empty input list short-circuits without an LLM call — the
// undocumented set is often empty on a healthy run and the round-trip
// cost is unwarranted.
func WhyDocument(ctx context.Context, tiering LLMTiering, features []CodeFeature) (map[string]string, error) {
	if len(features) == 0 {
		return map[string]string{}, nil
	}
	client := tiering.Small()

	var sb strings.Builder
	for _, f := range features {
		fmt.Fprintf(&sb, "- name: %s\n", f.Name)
		if f.Description != "" {
			fmt.Fprintf(&sb, "  description: %s\n", f.Description)
		}
		if f.Layer != "" {
			fmt.Fprintf(&sb, "  layer: %s\n", f.Layer)
		}
	}

	// PROMPT: Generates a 1-2 sentence rationale per feature explaining why a maintainer should prioritize documenting it. The rationale should be concrete, grounded in the feature's likely user impact, and avoid generic platitudes.
	prompt := fmt.Sprintf(`You are advising a maintainer about which undocumented user-facing features they should write documentation for, and why.

For each feature below, produce ONE rationale of 1-2 sentences explaining the SPECIFIC consequence of leaving this feature undocumented. Tie the rationale to what the feature does — what users will struggle with, what tickets will get filed, what onboarding moments break — rather than restating the obvious ("users need docs").

Avoid:
- Generic statements that could apply to any feature ("users need documentation").
- Restating the feature description.
- Marketing language ("delight users", "unlock value").

Aim for: a sentence a maintainer would nod at because it names a real failure mode.

Features:
%s
Return one rationale object per feature, with the same "name" string used in the input.`, sb.String())

	raw, err := client.CompleteJSON(ctx, prompt, whyDocumentSchema)
	if err != nil {
		return nil, fmt.Errorf("WhyDocument: %w", err)
	}

	var resp whyDocumentResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("WhyDocument: invalid JSON response: %w", err)
	}

	out := make(map[string]string, len(resp.Rationales))
	for _, r := range resp.Rationales {
		if r.Name == "" || r.Rationale == "" {
			continue
		}
		out[r.Name] = r.Rationale
	}
	return out, nil
}
