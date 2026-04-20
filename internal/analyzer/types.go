package analyzer

// PageAnalysis is the LLM-extracted summary and feature list for one documentation page.
type PageAnalysis struct {
	URL      string
	Summary  string
	Features []string
}

// ProductSummary is the synthesized product description and deduplicated feature list.
type ProductSummary struct {
	Description string
	Features    []string
}

// FeatureEntry maps one product feature to the code files and symbols that implement it.
type FeatureEntry struct {
	Feature string
	Files   []string
	Symbols []string
}

// FeatureMap is the complete feature-to-code mapping for a project.
type FeatureMap []FeatureEntry
