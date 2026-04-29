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

func TestMatch_singleLayer_matchesPositive(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "*.log\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("app.log", false)
	if !got.Skip {
		t.Errorf("expected skip for app.log; got %+v", got)
	}
	if got.Reason != "defaults" {
		t.Errorf("reason = %q, want %q", got.Reason, "defaults")
	}
}

func TestMatch_singleLayer_noMatch(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults": "*.log\n",
	}, []string{"defaults"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("main.go", false)
	if got.Skip {
		t.Errorf("expected no skip for main.go; got %+v", got)
	}
}
