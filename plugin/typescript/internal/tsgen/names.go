package tsgen

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"goa.design/goa/v3/codegen"
)

// reservedWords are identifiers that cannot name a TypeScript declaration.
var reservedWords = map[string]struct{}{
	"any": {}, "as": {}, "async": {}, "await": {}, "boolean": {}, "break": {}, "case": {}, "catch": {},
	"class": {}, "const": {}, "continue": {}, "debugger": {}, "declare": {}, "default": {}, "delete": {},
	"do": {}, "else": {}, "enum": {}, "export": {}, "extends": {}, "false": {}, "finally": {}, "for": {},
	"from": {}, "function": {}, "get": {}, "if": {}, "implements": {}, "import": {}, "in": {},
	"instanceof": {}, "interface": {}, "let": {}, "module": {}, "namespace": {}, "never": {}, "new": {},
	"null": {}, "number": {}, "object": {}, "of": {}, "package": {}, "private": {}, "protected": {},
	"public": {}, "return": {}, "set": {}, "static": {}, "string": {}, "super": {}, "switch": {},
	"symbol": {}, "this": {}, "throw": {}, "true": {}, "try": {}, "type": {}, "typeof": {},
	"undefined": {}, "unknown": {}, "var": {}, "void": {}, "while": {}, "with": {}, "yield": {},
}

// reservedGlobals are names the generated code uses from the JavaScript
// environment, or imports as is; a design type must not shadow them.
var reservedGlobals = []string{
	"z", "Array", "Boolean", "Date", "Error", "Headers", "JSON", "Map", "Math", "Number", "Object",
	"Promise", "Record", "RegExp", "Request", "Response", "Set", "String", "Symbol", "Uint8Array",
	"URL", "URLSearchParams",
}

var runtimeExportRE = regexp.MustCompile(`(?m)^export (?:abstract )?(?:async )?(?:class|function|const|let|type|interface|enum) ([A-Za-z_$][A-Za-z0-9_$]*)`)

// runtimeExports lists the names exported by the runtime sources. The root
// barrel re-exports both the runtime and the design types, so the two sets
// must not overlap.
func runtimeExports(files []RuntimeFile) []string {
	var names []string
	for _, f := range files {
		for _, m := range runtimeExportRE.FindAllSubmatch(f.Content, -1) {
			names = append(names, string(m[1]))
		}
	}
	return names
}

// newScope returns a name scope in which the given names are already taken.
func newScope(taken ...[]string) *codegen.NameScope {
	scope := codegen.NewNameScope()
	for _, names := range taken {
		for _, n := range names {
			scope.Unique(n)
		}
	}
	return scope
}

// words splits a design name into its words: at every character that cannot
// appear in an identifier and at lower-to-upper case changes.
func words(name string) []string {
	var (
		out []string
		cur []rune
	)
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = nil
		}
	}
	var prev rune
	for _, r := range name {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && (unicode.IsLower(prev) || unicode.IsDigit(prev)):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
		prev = r
	}
	flush()
	return out
}

func upperFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func lowerFirst(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// tsIdent returns the PascalCase identifier for a design name. Capitals
// inside a word are kept ("ThingWithViews", "thing_URL" -> "ThingURL").
func tsIdent(name string) string {
	var b strings.Builder
	for _, w := range words(name) {
		b.WriteString(upperFirst(w))
	}
	return safeIdent(b.String())
}

// camel returns the lowerCamelCase identifier for a design name
// ("stream_things_sse" -> "streamThingsSse").
func camel(name string) string {
	return safeIdent(lowerFirst(tsIdent(name)))
}

// kebab returns the kebab-case file name stem for a design name
// ("stream_things_sse" -> "stream-things-sse").
func kebab(name string) string {
	ws := words(name)
	for i, w := range ws {
		ws[i] = strings.ToLower(w)
	}
	if len(ws) == 0 {
		return "_"
	}
	return strings.Join(ws, "-")
}

// safeIdent makes s a legal, non-reserved identifier.
func safeIdent(s string) string {
	if s == "" {
		return "_"
	}
	if r := []rune(s)[0]; unicode.IsDigit(r) {
		s = "_" + s
	}
	if _, reserved := reservedWords[s]; reserved {
		s += "_"
	}
	return s
}

var identRE = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// tsPropKey renders an object property name, quoting it when it is not an
// identifier.
func tsPropKey(key string) string {
	if identRE.MatchString(key) {
		return key
	}
	return tsString(key)
}

// tsString renders a single-quoted TypeScript string literal.
func tsString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	rs := []rune(s)
	for i, r := range rs {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\'':
			b.WriteString(`\'`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\v':
			b.WriteString(`\v`)
		case 0:
			// \0 followed by a digit would read as a legacy octal escape.
			if i+1 < len(rs) && rs[i+1] >= '0' && rs[i+1] <= '9' {
				b.WriteString(`\u0000`)
			} else {
				b.WriteString(`\0`)
			}
		case '\u2028', '\u2029':
			// These end a line in older JavaScript parsers, so they are always
			// escaped.
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('\'')
	return b.String()
}

// jsdoc renders a JSDoc block for the given text at the given indentation, or
// "" when there is nothing to say. The result ends with a newline.
func jsdoc(text, indent string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "*/", "*\\/"))
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 1 {
		return indent + "/** " + lines[0] + " */\n"
	}
	var b strings.Builder
	b.WriteString(indent + "/**\n")
	for _, l := range lines {
		b.WriteString(strings.TrimRight(indent+" * "+strings.TrimSpace(l), " ") + "\n")
	}
	b.WriteString(indent + " */\n")
	return b.String()
}
