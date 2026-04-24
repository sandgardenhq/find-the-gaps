package analyzer

import "context"

// This file uses the Go _test.go export pattern: identifiers defined here are
// compiled only during test builds, so they are reachable from other packages'
// _test.go files (e.g. package analyzer_test) without leaking into production.
//
// The agent loop primitive is unexported. Production code goes through
// ToolLLMClient.CompleteWithTools, which is the only supported entry point. A
// handful of test stubs in package analyzer_test (drift_test.go's
// driftStubClient in particular) need to share the loop's exact dispatch
// semantics — handler invocation, unknown-tool feedback, max-rounds — rather
// than reimplementing them. That is the only reason these names are exposed.

// TurnFunc is a test-only alias for the unexported turnFunc.
type TurnFunc = turnFunc

// RunAgentLoop is a test-only export of runAgentLoop.
func RunAgentLoop(ctx context.Context, next TurnFunc, messages []ChatMessage, tools []Tool, opts ...AgentOption) (AgentResult, error) {
	return runAgentLoop(ctx, next, messages, tools, opts...)
}
