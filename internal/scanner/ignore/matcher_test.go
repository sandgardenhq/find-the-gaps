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

func TestMatch_laterLayerNegatesEarlier(t *testing.T) {
	m, err := newMatcherFromLayers(map[string]string{
		"defaults":   "vendor/\n",
		".ftgignore": "!vendor/\n",
	}, []string{"defaults", ".ftgignore"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("vendor/lib.go", false)
	if got.Skip {
		t.Errorf("later !vendor/ should re-include; got %+v", got)
	}
	if got.Reason != ".ftgignore" {
		t.Errorf("reason = %q, want %q", got.Reason, ".ftgignore")
	}
}

func TestMatch_earlierLayerCannotNegateLater(t *testing.T) {
	// Sanity: a defaults negation does NOT undo a .ftgignore positive match.
	m, err := newMatcherFromLayers(map[string]string{
		"defaults":   "!something\n",
		".ftgignore": "something\n",
	}, []string{"defaults", ".ftgignore"})
	if err != nil {
		t.Fatalf("newMatcherFromLayers: %v", err)
	}
	got := m.Match("something", false)
	if !got.Skip {
		t.Errorf("later positive should win; got %+v", got)
	}
}
