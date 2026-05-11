package analyzer

// RoleResolver resolves a docs page URL to its content-classified role.
// Built once per run from the per-page AnalyzePage cache; consumed by the
// drift judge and screenshot detection prompts as a prominence hint.
//
// Unknown URLs, empty URLs, and stored empty strings all resolve to "other"
// — matching the inclusive-by-default rule applied in AnalyzePage when a
// response is missing the role field (e.g. a token-budget skip or an old
// cached response).
type RoleResolver func(pageURL string) string

// NewRoleResolver builds a resolver from a URL→role map. Callers typically
// pass map[url]PageAnalysis.Role after the per-page analysis pass completes.
func NewRoleResolver(roles map[string]string) RoleResolver {
	return func(pageURL string) string {
		if pageURL == "" {
			return "other"
		}
		if roles == nil {
			return "other"
		}
		role, ok := roles[pageURL]
		if !ok || role == "" {
			return "other"
		}
		return role
	}
}

// Deprecated: superseded by RoleResolver. Will be removed in Task 6 once
// all callers have been migrated to RoleResolver.
//
// Kept temporarily so drift.go and screenshot_gaps.go still compile during
// the staged migration.
func pageRole(_ string) string { return "other" }
