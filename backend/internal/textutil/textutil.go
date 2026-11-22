// Package textutil holds the chitta text normalization — THE one
// implementation shared by the service layer, the LadybugDB driver, and
// the fake driver (review round 2: no local copies, no divergence).
// It lives outside internal/domain so the domain package stays stdlib-
// pure (constitution rule).
package textutil

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// NormalizeText applies the contracts' normalization: NFC fold,
// lowercase (casefold approximation), whitespace collapse.
func NormalizeText(s string) string {
	s = norm.NFC.String(s)
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
