package ignore

import "testing"

func TestMatch_emptyMatcher_returnsNoSkip(t *testing.T) {
	m := &Matcher{}
	got := m.Match("main.go", false)
	if got.Skip {
		t.Errorf("empty matcher should not skip; got %+v", got)
	}
	if got.Reason != "" {
		t.Errorf("empty matcher reason should be empty; got %q", got.Reason)
	}
}
