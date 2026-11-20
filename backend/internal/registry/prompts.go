package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

// Prompt is a named Go text/template fragment loaded from config/prompts.
type Prompt struct {
	Name string
	Path string

	tmpl *template.Template
}

// Template returns the parsed Go text/template.
func (p Prompt) Template() *template.Template { return p.tmpl }

// loadPrompts loads config/prompts/*.md as templates named by file stem.
func loadPrompts(snap *Snapshot, dir string, errs *[]LoadError) {
	for _, e := range readKindDir(dir, KindPrompt, errs) {
		if e.IsDir() || skipEntry(e.Name()) || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			addErr(errs, path, KindPrompt, err)
			continue
		}
		name := stem(e.Name())
		tmpl, err := template.New(name).Parse(string(raw))
		if err != nil {
			addErr(errs, path, KindPrompt, fmt.Errorf("template parse: %w", err))
			continue
		}
		snap.Prompts[name] = Prompt{Name: name, Path: path, tmpl: tmpl}
	}
}
