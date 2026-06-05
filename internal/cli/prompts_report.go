package cli

import (
	"fmt"

	"github.com/sandgardenhq/find-the-gaps/internal/docholiday"
)

// promptsCounts summarizes the prompts.md reports-block annotation. An empty
// slice renders "0"; otherwise "N stale · M new" where N counts stale-doc
// prompts and M counts new-feature prompts.
func promptsCounts(prompts []docholiday.Prompt) string {
	if len(prompts) == 0 {
		return "0"
	}
	var stale, missing int
	for _, p := range prompts {
		if p.Category == docholiday.CategoryMissing {
			missing++
		} else {
			stale++
		}
	}
	return fmt.Sprintf("%d stale · %d new", stale, missing)
}
