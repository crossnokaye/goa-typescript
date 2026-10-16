package tsgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"goa.design/goa/v3/codegen"
)

// OutputDir is the directory of the generated package, relative to the
// output directory of `goa gen`.
var OutputDir = filepath.Join(codegen.Gendir, "ts")

// Render turns a package description into the files of the npm package.
// Every path is produced once and nothing is skipped when it already exists:
// the whole of gen/ts is regenerated on every run.
func Render(pkg *PackageSpec) []*codegen.File {
	files := []*codegen.File{
		verbatimFile("package.json", packageJSON(pkg)),
		verbatimFile("tsconfig.json", tsconfigJSON()),
		tsFile(pkg, path.Join("src", "index.ts"), "index", nil, pkg),
		renderTypes(pkg),
	}
	for _, f := range pkg.Runtime {
		files = append(files, runtimeFile(pkg, f))
	}
	for _, svc := range pkg.Services {
		files = append(files, renderService(pkg, svc)...)
	}
	return files
}

// DebugFile dumps the package description as JSON (gen/typescript_debug.json).
func DebugFile(pkg *PackageSpec) *codegen.File {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(pkg); err != nil {
		panic(err) // bug: the spec is plain data
	}
	return &codegen.File{
		Path: filepath.Join(codegen.Gendir, "typescript_debug.json"),
		SectionTemplates: []*codegen.SectionTemplate{{
			Name:   "typescript-debug",
			Source: readTemplate("verbatim"),
			Data:   map[string]any{"Code": strings.TrimRight(b.String(), "\n")},
		}},
	}
}

// tsFile assembles a TypeScript file: the generated-code header followed by
// the output of the named template. The body is rendered when goa writes the
// file, so a template failure is reported like any other generation error.
func tsFile(pkg *PackageSpec, rel, tmpl string, partials []string, data any) *codegen.File {
	body := func() (string, error) {
		t, err := template.New(tmpl).Funcs(templateFuncs()).Parse(readTemplate(tmpl, partials...))
		if err != nil {
			return "", fmt.Errorf("template %s: %w", tmpl, err)
		}
		var b strings.Builder
		if err := t.Execute(&b, data); err != nil {
			return "", fmt.Errorf("template %s: %w", tmpl, err)
		}
		return tidy(b.String()), nil
	}
	return &codegen.File{
		Path: filepath.Join(OutputDir, filepath.FromSlash(rel)),
		SectionTemplates: []*codegen.SectionTemplate{
			{
				Name:    "typescript-header",
				Source:  readTemplate("header"),
				FuncMap: templateFuncs(),
				Data:    map[string]any{"GoaVersion": pkg.GoaVersion},
			},
			{
				Name:    "typescript-" + tmpl,
				Source:  "\n{{ body }}\n",
				FuncMap: map[string]any{"body": body},
			},
		},
	}
}

var blankLines = regexp.MustCompile(`\n{3,}`)

// tidy normalizes the vertical whitespace of rendered template output: no
// blank lines at either end and never two in a row. Generated declarations
// contain no blank lines of their own, so only template layout is affected.
func tidy(s string) string {
	return blankLines.ReplaceAllString(strings.Trim(s, "\n"), "\n\n")
}

// runtimeFile emits a runtime source file: the header, a blank line and the
// file exactly as it is in the repository.
func runtimeFile(pkg *PackageSpec, f RuntimeFile) *codegen.File {
	return &codegen.File{
		Path: filepath.Join(OutputDir, "src", "runtime", f.Name),
		SectionTemplates: []*codegen.SectionTemplate{
			{
				Name:    "typescript-header",
				Source:  readTemplate("header"),
				FuncMap: templateFuncs(),
				Data:    map[string]any{"GoaVersion": pkg.GoaVersion},
			},
			{
				Name:   "typescript-runtime",
				Source: "\n{{ .Code }}",
				Data:   map[string]any{"Code": string(f.Content)},
			},
		},
	}
}

// verbatimFile emits content as is. JSON files take no header: JSON has no
// comments.
func verbatimFile(rel, content string) *codegen.File {
	return &codegen.File{
		Path: filepath.Join(OutputDir, filepath.FromSlash(rel)),
		SectionTemplates: []*codegen.SectionTemplate{{
			Name:   "typescript-verbatim",
			Source: readTemplate("verbatim"),
			Data:   map[string]any{"Code": strings.TrimRight(content, "\n")},
		}},
	}
}

// importBlock renders the import statements of a generated file. runtimePath
// and typesPath are relative module specifiers; they keep the ".js" suffix
// that bundler module resolution maps back to the ".ts" source.
func importBlock(imp Imports, runtimePath, typesPath string) string {
	var blocks []string
	if imp.Zod {
		blocks = append(blocks, "import * as z from 'zod';")
	}
	var local []string
	if len(imp.Runtime) > 0 {
		local = append(local, importStatement(imp.Runtime, runtimePath))
	}
	if len(imp.Types) > 0 && typesPath != "" {
		local = append(local, importStatement(imp.Types, typesPath))
	}
	if len(local) > 0 {
		blocks = append(blocks, strings.Join(local, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func importStatement(names []string, from string) string {
	oneLine := "import { " + strings.Join(names, ", ") + " } from " + tsString(from) + ";"
	if len(oneLine) <= 100 {
		return oneLine
	}
	return "import {\n  " + strings.Join(names, ",\n  ") + ",\n} from " + tsString(from) + ";"
}

// packageJSON renders gen/ts/package.json. The package is consumed as
// TypeScript source (for instance through a pnpm workspace), so its exports
// point at the .ts files.
func packageJSON(pkg *PackageSpec) string {
	type manifest struct {
		Name             string            `json:"name"`
		Version          string            `json:"version"`
		Description      string            `json:"description,omitempty"`
		Private          bool              `json:"private"`
		Type             string            `json:"type"`
		SideEffects      bool              `json:"sideEffects"`
		Exports          map[string]string `json:"exports"`
		Scripts          map[string]string `json:"scripts"`
		PeerDependencies map[string]string `json:"peerDependencies"`
		DevDependencies  map[string]string `json:"devDependencies"`
	}
	m := manifest{
		Name:        pkg.Name,
		Version:     pkg.Version,
		Description: pkg.Description,
		Private:     true,
		Type:        "module",
		Exports: map[string]string{
			".":         "./src/index.ts",
			"./runtime": "./src/runtime/index.ts",
			"./types":   "./src/types.ts",
			"./*":       "./src/*/index.ts",
		},
		Scripts:          map[string]string{"type-check": "tsc --noEmit"},
		PeerDependencies: map[string]string{"zod": "^4.1.0"},
		DevDependencies:  map[string]string{"typescript": "^5.6.2", "zod": "^4.1.0"},
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		panic(err) // bug: the manifest is plain data
	}
	return b.String()
}

// tsconfigJSON renders gen/ts/tsconfig.json, used by the package's own
// type-check script.
func tsconfigJSON() string {
	return `{
  "compilerOptions": {
    "strict": true,
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "isolatedModules": true,
    "verbatimModuleSyntax": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "skipLibCheck": true,
    "noEmit": true
  },
  "include": ["src"]
}
`
}
