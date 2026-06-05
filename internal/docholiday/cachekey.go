package docholiday

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// skillVersion is a short hash of both embedded skill bodies. Any edit to a
// SKILL.md changes it, which flows into every unit key and invalidates cached
// prompts authored under the old instructions.
func skillVersion() string {
	sum := sha256.Sum256([]byte(skillBody(staleDocsSkillRaw) + "\x00" + skillBody(newFeatureSkillRaw)))
	return hex.EncodeToString(sum[:])[:12]
}

// unitKey is the cache key for a unit under the current skill version.
func unitKey(u unit) string { return unitKeyWithSkill(u, skillVersion()) }

// unitKeyWithSkill is unitKey with an explicit skill-version token (test seam).
func unitKeyWithSkill(u unit, skill string) string {
	var b strings.Builder
	b.WriteString(string(u.category))
	b.WriteByte('|')
	b.WriteString(skill)
	b.WriteByte('|')
	switch u.category {
	case CategoryStale:
		b.WriteString(u.page)
		fmt.Fprintf(&b, "|%d/%d|", u.part, u.parts)
		for _, it := range u.staleItems {
			b.WriteString(it.feature)
			b.WriteByte('\x1f')
			b.WriteString(it.issue)
			b.WriteByte('\n')
		}
	case CategoryMissing:
		f := u.feature
		b.WriteString(f.Feature.Name)
		b.WriteByte('|')
		b.WriteString(f.Feature.Description)
		b.WriteByte('|')
		files := append([]string(nil), f.Files...)
		sort.Strings(files)
		b.WriteString(strings.Join(files, ","))
		b.WriteByte('|')
		syms := append([]string(nil), f.Symbols...)
		sort.Strings(syms)
		b.WriteString(strings.Join(syms, ","))
		b.WriteByte('|')
		b.WriteString(u.rationale)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
