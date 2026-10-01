package web

import (
	"reflect"
	"strings"
	"testing"

	"github.com/le-marais/claimsgen/internal/infrastructure/config"
)

// numericPaths lists the json path of every float64 leaf in a config struct,
// skipping slices: the excess choices are a table, not form fields.
func numericPaths(t reflect.Type, prefix []string, out *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		path := append(append([]string(nil), prefix...), strings.Split(f.Tag.Get("json"), ",")[0])
		switch f.Type.Kind() {
		case reflect.Struct:
			numericPaths(f.Type, path, out)
		case reflect.Float64:
			*out = append(*out, strings.Join(path, "."))
		}
	}
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
			p := strings.Join(f.Path, ".")
			if !params[p] {
				t.Errorf("form field %s addresses no parameter", p)
			}
			if seen[p] {
				t.Errorf("form field %s appears twice", p)
			}
			seen[p] = true
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
