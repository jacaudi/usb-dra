package usb

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// goTemplateAction matches one {{ ... }} action, comments included, across lines.
var goTemplateAction = regexp.MustCompile(`(?s)\{\{.*?\}\}`)

// quotedScalar matches a double- or single-quoted YAML scalar on one line.
var quotedScalar = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'[^']*'`)

// flowStart matches a flow collection opening a value: at the start of a line, after "key: " or after "- ". A bracket in the middle of a plain scalar (a CEL index expression) is not one.
var flowStart = regexp.MustCompile(`(?:^\s*(?:-\s+)*|:\s+)[\[{]`)

// flowCollections returns "line N" for every flow mapping or sequence in the YAML documents of src, except an empty collection that is a whole document: values.yaml is exactly {}.
func flowCollections(src []byte) ([]string, error) {
	var found []string
	var walk func(n *yaml.Node, root bool)
	walk = func(n *yaml.Node, root bool) {
		if n.Kind == yaml.DocumentNode {
			for _, c := range n.Content {
				walk(c, true)
			}
			return
		}
		if n.Style&yaml.FlowStyle != 0 && (!root || len(n.Content) != 0) {
			found = append(found, fmt.Sprintf("line %d", n.Line))
		}
		for _, c := range n.Content {
			walk(c, false)
		}
	}
	dec := yaml.NewDecoder(bytes.NewReader(src))
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return found, nil
			}
			return nil, err
		}
		walk(&doc, false)
	}
}

// templateFlowLines returns "line N: text" for every line of a Helm template that opens a flow collection in its YAML body. Template actions, YAML comments and quoted strings (CEL expressions) are not YAML structure and are ignored. A template is not parseable YAML, so this is a line check, not a parse.
func templateFlowLines(src string) []string {
	src = goTemplateAction.ReplaceAllStringFunc(src, func(a string) string {
		return strings.Repeat("\n", strings.Count(a, "\n")) // keep line numbers
	})
	var found []string
	for i, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if flowStart.MatchString(quotedScalar.ReplaceAllString(line, `""`)) {
			found = append(found, fmt.Sprintf("line %d: %s", i+1, strings.TrimSpace(line)))
		}
	}
	return found
}

func TestFlowDetection(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int // flow collections found
	}{
		{"block mapping and sequence", "a:\n  b: 1\n  c:\n    - 2\n", 0},
		{"empty root mapping", "{}\n", 0},
		{"flow mapping value", "resources: {cpu: 10m}\n", 1},
		{"empty flow value", "nodeSelector: {}\n", 1},
		{"flow sequence value", "products: [\"2838\", \"2832\"]\n", 1},
		{"flow mapping in a block sequence", "tolerations:\n  - {operator: Exists}\n", 1},
		{"flow list inside a quoted string", "expression: 'a in [\"x\", \"y\"]'\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := flowCollections([]byte(tt.src))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tt.want {
				t.Errorf("flowCollections(%q) = %v, want %d", tt.src, got, tt.want)
			}
		})
	}

	tplTests := []struct {
		name string
		src  string
		want int
	}{
		{"block template", "args:\n  - {{ printf \"-v=%d\" 2 | quote }}\n  {{- with .Values.x }}\n  a: b\n  {{- end }}\n", 0},
		{"flow mapping value", "resources: {cpu: {{ .Values.cpu }}}\n", 1},
		{"flow sequence in a list item", "args:\n  - [a, b]\n", 1},
		{"flow mapping in a list item", "tolerations:\n  - {operator: Exists}\n", 1},
		{"brackets in a comment", "# a: [b] and {c}\nkey: value\n", 0},
		{"brackets inside a template comment", "{{/*\nkeys: [a, b]\n*/}}\nkey: value\n", 0},
		{"brackets inside a quoted CEL string", "expression: \"x.filter(s, s != null) in [1]\"\n", 0},
		{"brackets mid plain scalar", "expression: request.extra[?'k'][0].orValue('')\n", 0},
	}
	for _, tt := range tplTests {
		t.Run("template/"+tt.name, func(t *testing.T) {
			if got := templateFlowLines(tt.src); len(got) != tt.want {
				t.Errorf("templateFlowLines(%q) = %v, want %d", tt.src, got, tt.want)
			}
		})
	}
}

// TestChartAndExamplesAreBlockStyle fails on any flow mapping or sequence in the authored YAML under charts/ and examples/. values.yaml is exactly {} and the one flow collection allowed. values.schema.json is JSON and not checked.
func TestChartAndExamplesAreBlockStyle(t *testing.T) {
	var plain, templates []string
	err := filepath.WalkDir(chartDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.IsDir() && path == filepath.Join(chartDir, "charts"): // the vendored dependency
			return filepath.SkipDir
		case d.IsDir() && path == filepath.Join(chartDir, "templates"):
			entries, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			for _, e := range entries {
				if ext := filepath.Ext(e.Name()); ext == ".yaml" || ext == ".tpl" {
					templates = append(templates, filepath.Join(path, e.Name()))
				}
			}
			return filepath.SkipDir
		case !d.IsDir() && (filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml"):
			plain = append(plain, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	examples, err := filepath.Glob("../../examples/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	plain = append(plain, examples...)
	if len(plain) < 3 || len(templates) == 0 {
		t.Fatalf("found %d plain YAML files and %d templates, want Chart.yaml, values.yaml, an example and the templates", len(plain), len(templates))
	}

	for _, p := range plain {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		found, err := flowCollections(src)
		if err != nil {
			t.Errorf("%s: %v", p, err)
		}
		for _, f := range found {
			t.Errorf("%s: flow collection at %s, want block style", p, f)
		}
	}
	for _, p := range templates {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range templateFlowLines(string(src)) {
			t.Errorf("%s: flow collection at %s, want block style", p, f)
		}
	}
}
