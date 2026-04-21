package analyzer

import "context"

// ExportedMapPageToFeatures exposes mapPageToFeatures for black-box tests.
// features and counter are accepted for API symmetry with callers but unused —
// mapPageToFeatures takes pre-serialized featuresJSON and uses the local tiktoken estimator.
func ExportedMapPageToFeatures(
	ctx context.Context,
	client LLMClient,
	_ TokenCounter,
	_ []string,
	featuresJSON []byte,
	featureTokens int,
	tokenBudget int,
	pageURL, content string,
) ([]string, error) {
	return mapPageToFeatures(ctx, client, featuresJSON, featureTokens, tokenBudget, pageURL, content)
}
