package tsgen

import (
	"embed"
	"fmt"
	"path"
	"strings"

	"goa.design/goa/v3/codegen"
)

//go:embed templates/*.ts.tpl templates/partial/*.ts.tpl
var templateFS embed.FS

// readTemplate returns the source of the named template with the named
// partials appended as {{ define "partial_<name>" }} blocks. It plays the role
// of goa's codegen/template.TemplateReader, which only reads ".go.tpl" files.
//
// Templates use the default delimiters and receive every piece of TypeScript
// as data, so generated code is never parsed as template source.
func readTemplate(name string, partials ...string) string {
	content, err := templateFS.ReadFile(path.Join("templates", name+".ts.tpl"))
	if err != nil {
		panic("failed to load template " + name + ": " + err.Error()) // bug
	}
	var b strings.Builder
	for _, partial := range partials {
		tmpl, err := templateFS.ReadFile(path.Join("templates", "partial", partial+".ts.tpl"))
		if err != nil {
			panic("failed to load partial template " + partial + ": " + err.Error()) // bug
		}
		b.WriteString("{{- define \"partial_" + partial + "\" -}}\n")
		b.WriteString(strings.TrimRight(string(tmpl), "\n"))
		b.WriteString("\n{{- end }}\n")
	}
	b.Write(content)
	return b.String()
}

// templateFuncs returns the functions available to the templates.
func templateFuncs() map[string]any {
	funcs := codegen.TemplateFuncs()
	funcs["jsdoc"] = jsdoc
	funcs["tsString"] = tsString
	funcs["tsPropKey"] = tsPropKey
	funcs["camel"] = camel
	funcs["kebab"] = kebab
	funcs["join"] = strings.Join
	funcs["indent"] = indentLines
	funcs["dict"] = dict
	funcs["nullable"] = nullable
	return funcs
}

// nullable renders a string literal, or null for the empty string.
func nullable(s string) string {
	if s == "" {
		return "null"
	}
	return tsString(s)
}

// indentLines prefixes every non-empty line of s.
func indentLines(prefix, s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n")
}

// dict builds the map passed to a partial from alternating keys and values.
func dict(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments")
	}
	out := make(map[string]any, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %v is not a string", pairs[i])
		}
		out[key] = pairs[i+1]
	}
	return out, nil
}
