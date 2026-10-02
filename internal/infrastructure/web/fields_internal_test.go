package web

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/le-marais/claimsgen/internal/domain/lob"
	"github.com/le-marais/claimsgen/internal/infrastructure/config"
)

// numericPaths lists the json path of every float64 leaf in a config struct.
// A section list's elements are written as "[]", so a section field's path
// reads like claims.sections.[].base_frequency. The excess choices are a
// table, not form fields, so they are skipped.
func numericPaths(t reflect.Type, prefix []string, out *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		path := append(append([]string(nil), prefix...), strings.Split(f.Tag.Get("json"), ",")[0])
		switch f.Type.Kind() {
		case reflect.Struct:
			numericPaths(f.Type, path, out)
		case reflect.Slice:
			if f.Type.Elem().Kind() == reflect.Struct && strings.Join(path, ".") != "book.excess_choices" {
				numericPaths(f.Type.Elem(), append(path, "[]"), out)
			}
		case reflect.Float64:
			*out = append(*out, strings.Join(path, "."))
		}
	}
}

// formPath is the parameter path a form field addresses, with a section
// group's fields written under its section list.
func formPath(g fieldGroup, f formField) string {
	if g.Sections == nil {
		return strings.Join(f.Path, ".")
	}
	return strings.Join(slices.Concat(g.Sections, []string{"[]"}, f.Path), ".")
}

// RF-13: every numeric line-of-business parameter has exactly one form field,
// and every form field addresses a real parameter, so adding a parameter
// without its label and tip fails here rather than leaving it silently
// uneditable in the UI.
func TestFormFieldsCoverEveryParameter(t *testing.T) {
	var want []string
	numericPaths(reflect.TypeOf(config.LOBParams{}), nil, &want)
	params := map[string]bool{}
	for _, p := range want {
		params[p] = true
	}
	seen := map[string]bool{}
	for _, g := range formFields {
		if g.Label == "" {
			t.Error("a field group has no label")
		}
		for _, f := range g.Fields {
			p := formPath(g, f)
			if f.Kind != "" && (g.Sections == nil || f.Path[0] != "severity") {
				t.Errorf("form field %s has a severity kind but is not a section severity field", p)
			}
			if k := lob.SeverityKind(f.Kind); f.Kind != "" && k != lob.SumInsuredLognormal && k != lob.Pareto && k != lob.Lognormal && k != lob.LognormalPareto {
				t.Errorf("form field %s has unknown severity kind %q", p, f.Kind)
			}
			if !params[p] {
				t.Errorf("form field %s addresses no parameter", p)
			}
			// Each severity kind has its own fields, so a path is unique per kind.
			if key := p + " " + f.Kind; seen[key] {
				t.Errorf("form field %s appears twice", p)
			} else {
				seen[key] = true
				seen[p] = true
			}
			if f.Label == "" || f.Tip == "" {
				t.Errorf("form field %s needs a label and a tip", p)
			}
		}
	}
	for _, p := range want {
		if !seen[p] {
			t.Errorf("parameter %s has no form field", p)
		}
	}
}
