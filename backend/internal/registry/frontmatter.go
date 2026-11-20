package registry

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// splitFrontmatter separates leading YAML frontmatter (--- delimited) from
// the markdown body. ok is false when the content has no frontmatter block.
// CRLF line endings are normalized so Windows-authored files load.
func splitFrontmatter(raw string) (fm string, body string, ok bool) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimPrefix(raw, "\ufeff") // tolerate a BOM
	if !strings.HasPrefix(raw, "---\n") && raw != "---" {
		return "", "", false
	}
	rest := raw[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", false
	}
	fm = rest[:end]
	body = rest[end+4:]
	body = strings.TrimPrefix(body, "\n")
	return fm, body, true
}

// decodeFrontmatter strictly decodes frontmatter text into out.
func decodeFrontmatter(fm string, out any) error {
	dec := yaml.NewDecoder(strings.NewReader(fm))
	dec.KnownFields(true)
	return dec.Decode(out)
}
