package analyzer

import (
	"strings"
	"testing"
)

func TestPriorityRubric(t *testing.T) {
	for _, s := range []string{`"large"`, `"medium"`, `"small"`, "priority_reason", "page_role"} {
		if !strings.Contains(priorityRubric, s) {
			t.Errorf("priorityRubric missing %q", s)
		}
	}
}
