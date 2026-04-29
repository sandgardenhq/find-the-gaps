// Package ignore composes layered gitignore-style rules and decides whether a
// path should be skipped during a repository walk.
package ignore

// Decision is the result of testing a path against the layered rules.
type Decision struct {
	Skip   bool
	Reason string // name of the layer that decided, "" if no layer matched
}

// Matcher evaluates paths against an ordered list of gitignore layers.
type Matcher struct{}

// Match reports whether relPath should be skipped.
func (m *Matcher) Match(relPath string, isDir bool) Decision {
	return Decision{}
}
