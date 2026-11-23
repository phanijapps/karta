package wards

import (
	"errors"
	"strings"
	"testing"
)

const validConf = `
archetype: research
layout:
  kind: directory
  children:
    - { id: canonical, match: "{ward}.md", kind: file, format: markdown }
    - { id: log, match: log.md, kind: file, format: raw }
    - id: tasks
      match: tasks
      kind: directory
      required: false
      children:
        - { id: task, match: "*.md", kind: file, required: false, repeat: true, format: markdown }
`

func TestParseConfValid(t *testing.T) {
	conf, err := ParseConf([]byte(validConf), "ward-conf.yaml")
	if err != nil {
		t.Fatalf("ParseConf: %v", err)
	}
	if conf.Archetype != "research" {
		t.Fatalf("archetype: %q", conf.Archetype)
	}
	if conf.Layout.Kind != KindDirectory || len(conf.Layout.Children) != 3 {
		t.Fatalf("layout root: %+v", conf.Layout)
	}
	tasks := conf.Layout.Children[2]
	if tasks.IsRequired() {
		t.Fatal("explicit required:false not honored")
	}
	if tasks.IsRepeat() {
		t.Fatal("repeat default must be false")
	}
	if !conf.Layout.Children[0].IsRequired() {
		t.Fatal("required default must be true")
	}
	if len(tasks.Children) != 1 || tasks.Children[0].ID != "task" {
		t.Fatalf("tasks children: %+v", tasks.Children)
	}
	if tasks.Children[0].IsRequired() || !tasks.Children[0].IsRepeat() {
		t.Fatal("explicit required:false repeat:true not honored")
	}
}

func TestParseConfErrors(t *testing.T) {
	cases := []struct {
		name        string
		conf        string
		mustMention []string
	}{
		{
			name:        "unknown field rejected naming field and file",
			conf:        "archetype: x\nlayout:\n  kind: directory\n  children:\n    - { id: a, match: a, kind: file, surprise: yes }\n",
			mustMention: []string{"ward-conf.yaml", "surprise"},
		},
		{
			name:        "root must be directory",
			conf:        "layout:\n  kind: file\n",
			mustMention: []string{"layout: kind"},
		},
		{
			name:        "root carries no match",
			conf:        "layout:\n  kind: directory\n  match: x\n",
			mustMention: []string{"layout: match"},
		},
		{
			name:        "malformed glob rejected at parse time",
			conf:        "layout:\n  kind: directory\n  children:\n    - { id: a, match: \"[\", kind: file }\n",
			mustMention: []string{"children[0] (a): match"},
		},
		{
			name:        "non-root node needs match",
			conf:        "layout:\n  kind: directory\n  children:\n    - { id: a, kind: file }\n",
			mustMention: []string{"children[0] (a): match"},
		},
		{
			name:        "file node cannot carry children",
			conf:        "layout:\n  kind: directory\n  children:\n    - id: a\n      match: a\n      kind: file\n      children:\n        - { id: b, match: b, kind: file }\n",
			mustMention: []string{"children[0] (a): children"},
		},
		{
			name:        "directory node cannot carry format",
			conf:        "layout:\n  kind: directory\n  children:\n    - { id: a, match: a, kind: directory, format: markdown }\n",
			mustMention: []string{"children[0] (a): format"},
		},
		{
			name:        "bad kind value",
			conf:        "layout:\n  kind: directory\n  children:\n    - { id: a, match: a, kind: symlink }\n",
			mustMention: []string{"children[0] (a): kind"},
		},
		{
			name:        "bad format value",
			conf:        "layout:\n  kind: directory\n  children:\n    - { id: a, match: a, kind: file, format: pdf }\n",
			mustMention: []string{"children[0] (a): format"},
		},
		{
			name:        "root format rejected",
			conf:        "layout:\n  kind: directory\n  format: markdown\n",
			mustMention: []string{"layout: format"},
		},
		{
			name:        "oversized archetype rejected",
			conf:        "archetype: " + strings.Repeat("a", 65) + "\nlayout:\n  kind: directory\n",
			mustMention: []string{"archetype"},
		},
		{
			name:        "oversized id rejected",
			conf:        "layout:\n  kind: directory\n  children:\n    - { id: \"" + strings.Repeat("i", 129) + "\", match: a, kind: file }\n",
			mustMention: []string{"children[0]", "id"},
		},
		{
			name:        "oversized match rejected",
			conf:        "layout:\n  kind: directory\n  children:\n    - { match: \"" + strings.Repeat("m", 257) + "\", kind: file }\n",
			mustMention: []string{"children[0]: match"},
		},
		{
			name:        "not yaml at all",
			conf:        ":::\n",
			mustMention: []string{"ward-conf.yaml"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseConf([]byte(tc.conf), "ward-conf.yaml")
			if err == nil {
				t.Fatal("want parse error")
			}
			var pe *ConfParseError
			if !errors.As(err, &pe) {
				t.Fatalf("want ConfParseError, got %T: %v", err, err)
			}
			for _, want := range tc.mustMention {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error must mention %q: %v", want, err)
				}
			}
		})
	}
}

// $ref is PARSE-ONLY in v1 (design decision 3): it decodes, the linter
// ignores it.
func TestParseConfRefAccepted(t *testing.T) {
	conf := "layout:\n  kind: directory\n  children:\n    - { id: a, match: a, kind: directory, $ref: specification }\n"
	got, err := ParseConf([]byte(conf), "ward-conf.yaml")
	if err != nil {
		t.Fatalf("ParseConf with $ref: %v", err)
	}
	if got.Layout.Children[0].Ref != "specification" {
		t.Fatalf("$ref not decoded: %+v", got.Layout.Children[0])
	}
}

// Node ids are OPTIONAL (design decision 3: `id?`) — a conforming
// user-authored tree without ids must parse and lint; findings identify
// id-less nodes by their match pattern.
func TestParseConfIdOptional(t *testing.T) {
	conf := "layout:\n  kind: directory\n  children:\n    - { match: notes.md, kind: file, format: markdown }\n"
	got, err := ParseConf([]byte(conf), "ward-conf.yaml")
	if err != nil {
		t.Fatalf("id-less node must parse: %v", err)
	}
	if len(got.Layout.Children) != 1 || got.Layout.Children[0].ID != "" {
		t.Fatalf("unexpected tree: %+v", got.Layout.Children)
	}
}
