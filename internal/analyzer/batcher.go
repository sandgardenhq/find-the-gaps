package analyzer

// batchSymLines groups symLines into batches where each batch's token estimate,
// added to featuresTokens, does not exceed budget.
// A single line that alone exceeds the remaining budget is placed in its own batch.
func batchSymLines(symLines []string, featuresTokens, budget int) [][]string {
	if len(symLines) == 0 {
		return nil
	}
	remaining := budget - featuresTokens
	var batches [][]string
	var current []string
	currentTokens := 0

	for _, line := range symLines {
		t := countTokens(line)
		if len(current) > 0 && currentTokens+t > remaining {
			batches = append(batches, current)
			current = nil
			currentTokens = 0
		}
		current = append(current, line)
		currentTokens += t
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}
