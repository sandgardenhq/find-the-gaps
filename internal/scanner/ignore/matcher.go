// Package ignore composes layered gitignore-style rules and decides whether a
// path should be skipped during a repository walk.
package ignore

import (
	gitignore "github.com/sabhiram/go-gitignore"
)

// Decision is the result of testing a path against the layered rules.
type Decision struct {
	Skip   bool
	Reason string // name of the layer that decided, "" if no layer matched
}

// Matcher evaluates paths against an ordered list of gitignore layers.
type Matcher struct {
	layers []layer
}

type layer struct {
	name string
	gi   *gitignore.GitIgnore
}

// newMatcherFromLayers compiles the given source strings in the given order.
// Exposed for tests only — production code uses Load.
func newMatcherFromLayers(sources map[string]string, order []string) (*Matcher, error) {
	m := &Matcher{}
	for _, name := range order {
		src, ok := sources[name]
		if !ok {
			continue
		}
		gi := gitignore.CompileIgnoreLines(splitLines(src)...)
		m.layers = append(m.layers, layer{name: name, gi: gi})
	}
	return m, nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// Match reports whether relPath should be skipped.
func (m *Matcher) Match(relPath string, isDir bool) Decision {
	d := Decision{}
	for _, l := range m.layers {
		if l.gi.MatchesPath(relPath) {
			d = Decision{Skip: true, Reason: l.name}
		}
	}
	return d
}
