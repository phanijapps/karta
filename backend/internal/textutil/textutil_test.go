package textutil

import "testing"

// NormalizeText is the single normalization shared by the service layer,
// the LadybugDB driver, and the fake driver — these tests pin the
// contract so a change here is deliberate, not accidental drift.
func TestNormalizeText(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"identity", "hello", "hello"},
		{"lowercases", "HeLLo WoRLD", "hello world"},
		{"trims outer space", "  hello  ", "hello"},
		{"collapses inner whitespace", "a\tb\nc  d", "a b c d"},
		{"collapses unicode spaces", "a\u00a0b\u2003c", "a b c"},
		{"empty", "", ""},
		{"only spaces", " \t\n ", ""},
		// NFC fold: decomposed e + combining accent becomes precomposed é.
		{"nfc fold", "cafe\u0301", "caf\u00e9"},
		// A combining accent alone is not whitespace; it survives.
		{"combining mark kept", "e\u0301", "\u00e9"},
		// Mixed script lowercasing (approximate casefold: Go's ToLower maps
		// Σ→σ everywhere — no context-aware final sigma; pin that).
		{"greek capital sigma", "Σ ΟΔΟΣ", "σ οδοσ"},
		// Leading/trailing collapse must not leave a stray inner space.
		{"space then trim", " a b ", "a b"},
		{"cjk untouched", "你好 世界", "你好 世界"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeText(tc.in); got != tc.want {
				t.Fatalf("NormalizeText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
