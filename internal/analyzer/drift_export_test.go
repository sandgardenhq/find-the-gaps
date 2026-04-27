package analyzer

// Test-only exports for analyzer_test (drift package). Identifiers defined in
// _test.go files are compiled only during test builds, so they reach
// package-external tests without leaking into production. See
// agent_loop_export_test.go for the broader rationale.

// InvestigateFeatureDrift is a test-only export of investigateFeatureDrift.
var InvestigateFeatureDrift = investigateFeatureDrift

// JudgeFeatureDrift is a test-only export of judgeFeatureDrift.
var JudgeFeatureDrift = judgeFeatureDrift

// DriftObservation is a test-only alias for the unexported driftObservation.
type DriftObservation = driftObservation
