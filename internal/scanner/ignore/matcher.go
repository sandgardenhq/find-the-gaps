// Package ignore composes layered gitignore-style rules and decides whether a
// path should be skipped during a repository walk.
package ignore

import (
	"strings"

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
	// gi is the full layer (positive + negated lines) — used for skip detection.
	gi *gitignore.GitIgnore
	// negate captures only the layer's `!`-prefixed lines, with `!` stripped so
	// the upstream library treats them as positives. Used to detect that this
	// layer wants to re-include a path even if no other line in the layer
	// matched. We need this because sabhiram/go-gitignore's MatchesPathHow
	// returns (false, nil) when ONLY a negated pattern matches a path — it
	// only signals negation when a positive match in the SAME GitIgnore is
	// being undone. Across layers, that signal is invisible, so we track the
	// negation set ourselves.
	negate *gitignore.GitIgnore
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
		lines := splitLines(src)
		gi := gitignore.CompileIgnoreLines(lines...)
		negate := gitignore.CompileIgnoreLines(extractNegatedLines(lines)...)
		m.layers = append(m.layers, layer{name: name, gi: gi, negate: negate})
	}
	return m, nil
}

// extractNegatedLines returns the `!`-prefixed lines with the `!` removed, so
// they can be compiled as a separate GitIgnore whose positive matches signal
// "this layer wants to re-include this path".
func extractNegatedLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "!") {
			out = append(out, strings.TrimPrefix(trimmed, "!"))
		}
	}
	return out
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

type layerResult int

const (
	layerNoMatch layerResult = iota
	layerSkip
	layerNegate
)

func (l layer) check(relPath string) layerResult {
	if l.gi.MatchesPath(relPath) {
		return layerSkip
	}
	if l.negate != nil && l.negate.MatchesPath(relPath) {
		return layerNegate
	}
	return layerNoMatch
}

// Match reports whether relPath should be skipped.
func (m *Matcher) Match(relPath string, isDir bool) Decision {
	d := Decision{}
	for _, l := range m.layers {
		switch l.check(relPath) {
		case layerSkip:
			d = Decision{Skip: true, Reason: l.name}
		case layerNegate:
			d = Decision{Skip: false, Reason: l.name}
		}
	}
	return d
}
