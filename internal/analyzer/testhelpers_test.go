package analyzer_test

import "context"

// fakeClient is a test double for analyzer.LLMClient.
type fakeClient struct {
	responses []string // popped in order; last entry repeated when exhausted
	callCount int
	forcedErr error
}

func (f *fakeClient) Complete(_ context.Context, _ string) (string, error) {
	if f.forcedErr != nil {
		return "", f.forcedErr
	}
	if len(f.responses) == 0 {
		return "", nil
	}
	idx := f.callCount
	if idx >= len(f.responses) {
		idx = len(f.responses) - 1
	}
	f.callCount++
	return f.responses[idx], nil
}
