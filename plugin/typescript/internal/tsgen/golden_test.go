package tsgen

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/eval"
	"goa.design/goa/v3/expr"

	"github.com/xeger/goa-typescript/plugin/typescript/internal/tsgen/testdata"
	tsruntime "github.com/xeger/goa-typescript/runtime"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

// fixtures are the designs every generator test runs against. Tests that
// evaluate a DSL reset goa's global design root, so none of them may run in
// parallel.
var fixtures = []struct {
	Name string
	DSL  func()
}{
	{"Primitives", testdata.PrimitivesDSL},
	{"Formats", testdata.FormatsDSL},
	{"Validations", testdata.ValidationsDSL},
	{"Optional", testdata.OptionalDSL},
	{"Collections", testdata.CollectionsDSL},
	{"Nested", testdata.NestedDSL},
	{"Union", testdata.UnionDSL},
	{"Views", testdata.ViewsDSL},
	{"JSONTag", testdata.JSONTagDSL},
	{"Alias", testdata.AliasDSL},
	{"Names", testdata.NamesDSL},
	{"SSEWholeEvent", testdata.SSEWholeEventDSL},
	{"SSEDataField", testdata.SSEDataFieldDSL},
	{"SSEPostBody", testdata.SSEPostBodyDSL},
	{"SSEString", testdata.SSEStringDSL},
	{"SSEInt", testdata.SSEIntDSL},
	{"Security", testdata.SecurityDSL},
	{"Errors", testdata.ErrorsDSL},
	{"TwoServices", testdata.TwoServicesDSL},
	{"RESTBasic", testdata.RESTBasicDSL},
	{"RESTScalars", testdata.RESTScalarsDSL},
	{"RESTHeaders", testdata.RESTHeadersDSL},
	{"RESTStatuses", testdata.RESTStatusesDSL},
	{"RESTErrors", testdata.RESTErrorsDSL},
	{"RESTRaw", testdata.RESTRawDSL},
	{"RESTMixed", testdata.RESTMixedDSL},
	{"RESTSkipped", testdata.RESTSkippedDSL},
}

// design evaluates a DSL the way `goa gen` hands it to plugins: evaluated,
// then normalized, which wraps every inline method payload and result in a
// user type that goa generates.
func design(t *testing.T, dsl func()) *expr.RootExpr {
	t.Helper()
	root := codegen.RunDSL(t, dsl)
	if _, err := codegen.NewGeneration("example.com/toy/gen", []eval.Root{root}); err != nil {
		t.Fatalf("NewGeneration: %v", err)
	}
	return root
}

// build evaluates a DSL and builds its package description.
func build(t *testing.T, dsl func()) *PackageSpec {
	t.Helper()
	pkg, err := Build([]*expr.RootExpr{design(t, dsl)}, Options{Runtime: tsruntime.Sorted()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return pkg
}

// renderTo writes the generated package into dir and returns the content of
// every file by slash-separated path relative to the package root (gen/ts).
func renderTo(t *testing.T, pkg *PackageSpec, dir string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	seen := make(map[string]struct{})
	for _, f := range Render(pkg) {
		if _, dup := seen[f.Path]; dup {
			t.Fatalf("path %s is rendered twice", f.Path)
		}
		seen[f.Path] = struct{}{}
		p, err := f.Render(dir)
		if err != nil {
			t.Fatalf("render %s: %v", f.Path, err)
		}
		content, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		rel, err := filepath.Rel(filepath.Join(dir, OutputDir), p)
		if err != nil {
			t.Fatalf("rel %s: %v", p, err)
		}
		out[filepath.ToSlash(rel)] = string(content)
	}
	return out
}

// TestGolden compares the types file and the service files of every fixture
// with testdata/golden/<Fixture>. Run with -update to rewrite them.
func TestGolden(t *testing.T) {
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			files := renderTo(t, build(t, fx.DSL), t.TempDir())
			golden := filepath.Join("testdata", "golden", fx.Name)
			want := make(map[string]struct{})
			for rel, content := range files {
				if !strings.HasPrefix(rel, "src/") || strings.HasPrefix(rel, "src/runtime/") || rel == "src/index.ts" {
					continue
				}
				want[rel] = struct{}{}
				assertGolden(t, filepath.Join(golden, filepath.FromSlash(rel)), content)
			}
			// A golden file that is no longer generated is stale.
			_ = filepath.WalkDir(golden, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				rel, _ := filepath.Rel(golden, p)
				if _, ok := want[filepath.ToSlash(rel)]; ok {
					return nil
				}
				if *update {
					if err := os.Remove(p); err != nil {
						t.Errorf("remove stale golden %s: %v", p, err)
					}
					return nil
				}
				t.Errorf("stale golden file %s (run with -update)", p)
				return nil
			})
		})
	}
}

// TestPackageFiles covers the files that do not depend on the design's types:
// the manifest, the compiler configuration, the root barrel and the runtime.
func TestPackageFiles(t *testing.T) {
	pkg := build(t, testdata.TwoServicesDSL)
	files := renderTo(t, pkg, t.TempDir())
	golden := filepath.Join("testdata", "golden", "_package")
	for _, rel := range []string{"package.json", "tsconfig.json", "src/index.ts"} {
		content, ok := files[rel]
		if !ok {
			t.Errorf("%s was not generated", rel)
			continue
		}
		assertGolden(t, filepath.Join(golden, filepath.FromSlash(rel)), content)
	}

	runtime := tsruntime.Sorted()
	if len(runtime) == 0 {
		t.Fatal("no runtime files embedded")
	}
	for _, f := range runtime {
		got, ok := files["src/runtime/"+f.Name]
		if !ok {
			t.Errorf("runtime file %s was not generated", f.Name)
			continue
		}
		header, body, _ := strings.Cut(got, "\n\n")
		if !strings.HasPrefix(header, "// Code generated by goa-typescript (goa v") || strings.Contains(header, "\n") {
			t.Errorf("%s: unexpected header %q", f.Name, header)
		}
		if body != string(f.Content) {
			t.Errorf("src/runtime/%s is not a verbatim copy of runtime/src/%s:\n%s", f.Name, f.Name,
				unifiedDiff(string(f.Content), body))
		}
	}
	for rel := range files {
		if name, ok := strings.CutPrefix(rel, "src/runtime/"); ok {
			found := false
			for _, f := range runtime {
				found = found || f.Name == name
			}
			if !found {
				t.Errorf("unexpected runtime file %s", rel)
			}
		}
	}
}

// TestGeneratedFilesAreWellFormed checks properties every generated file has.
func TestGeneratedFilesAreWellFormed(t *testing.T) {
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			for rel, content := range renderTo(t, build(t, fx.DSL), t.TempDir()) {
				if !strings.HasSuffix(content, "\n") || strings.HasSuffix(content, "\n\n") {
					t.Errorf("%s must end with exactly one newline", rel)
				}
				if strings.HasSuffix(rel, ".ts") && !strings.HasPrefix(content, "// Code generated by goa-typescript (goa v") {
					t.Errorf("%s lacks the generated-code header", rel)
				}
				for n, line := range strings.Split(content, "\n") {
					if strings.TrimRight(line, " \t") != line {
						t.Errorf("%s:%d: trailing whitespace", rel, n+1)
					}
				}
				if strings.Contains(content, "\n\n\n") {
					t.Errorf("%s contains consecutive blank lines", rel)
				}
				if strings.Contains(content, ".transform(") && !strings.HasPrefix(rel, "src/runtime/") {
					t.Errorf("%s uses .transform(), which cannot encode", rel)
				}
			}
		})
	}
}

// TestRenderIsDeterministic renders every fixture twice.
func TestRenderIsDeterministic(t *testing.T) {
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			first := renderTo(t, build(t, fx.DSL), t.TempDir())
			second := renderTo(t, build(t, fx.DSL), t.TempDir())
			if len(first) != len(second) {
				t.Fatalf("file count changed: %d then %d", len(first), len(second))
			}
			for rel, content := range first {
				if second[rel] != content {
					t.Errorf("%s differs between runs:\n%s", rel, unifiedDiff(content, second[rel]))
				}
			}
		})
	}
}

// TestRenderIgnoresNormalization renders every fixture from the design as
// evaluated and from the design as `goa gen` normalizes it. The wrappers goa
// adds around inline payloads and results must not show in the output.
func TestRenderIgnoresNormalization(t *testing.T) {
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			raw, err := Build([]*expr.RootExpr{codegen.RunDSL(t, fx.DSL)}, Options{Runtime: tsruntime.Sorted()})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			evaluated := renderTo(t, raw, t.TempDir())
			normalized := renderTo(t, build(t, fx.DSL), t.TempDir())
			if len(evaluated) != len(normalized) {
				t.Fatalf("file sets differ: %v then %v", sortedKeys(evaluated), sortedKeys(normalized))
			}
			for _, rel := range sortedKeys(evaluated) {
				if evaluated[rel] != normalized[rel] {
					t.Errorf("%s depends on normalization:\n%s", rel, unifiedDiff(evaluated[rel], normalized[rel]))
				}
			}
		})
	}
}

// TestFixturesTypeCheck compiles the package generated for every fixture with
// the TypeScript version pinned by the runtime. It needs the runtime's
// node_modules (`pnpm install` at the repository root) and is skipped without
// them, except in CI.
func TestFixturesTypeCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the TypeScript compile in short mode")
	}
	modules, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "runtime", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	tsc := filepath.Join(modules, ".bin", "tsc")
	if _, err := os.Stat(tsc); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI requires %s: run `pnpm install` first", tsc)
		}
		t.Skipf("%s not found: run `pnpm install` at the repository root to enable this test", tsc)
	}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			dir := t.TempDir()
			renderTo(t, build(t, fx.DSL), dir)
			pkgDir := filepath.Join(dir, OutputDir)
			if err := os.Symlink(modules, filepath.Join(pkgDir, "node_modules")); err != nil {
				t.Fatalf("link node_modules: %v", err)
			}
			cmd := exec.Command(tsc, "--noEmit", "-p", ".")
			cmd.Dir = pkgDir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("tsc failed: %v\n%s", err, out)
			}
		})
	}
}

// assertGolden compares got with the golden file at path, or rewrites the
// file when -update is set.
func assertGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("golden file %s is missing (run with -update): %v", path, err)
		return
	}
	if !bytes.Equal(want, []byte(got)) {
		t.Errorf("%s does not match the generated output (run with -update):\n%s", path, unifiedDiff(string(want), got))
	}
}

// unifiedDiff renders a line diff of want and got with three lines of context.
func unifiedDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	// Longest common subsequence table.
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type line struct {
		op   byte
		text string
	}
	var lines []line
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			lines = append(lines, line{' ', a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			lines = append(lines, line{'-', a[i]})
			i++
		default:
			lines = append(lines, line{'+', b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		lines = append(lines, line{'-', a[i]})
	}
	for ; j < len(b); j++ {
		lines = append(lines, line{'+', b[j]})
	}

	const context = 3
	keep := make([]bool, len(lines))
	for n, l := range lines {
		if l.op == ' ' {
			continue
		}
		for k := max(0, n-context); k <= min(len(lines)-1, n+context); k++ {
			keep[k] = true
		}
	}
	var out strings.Builder
	out.WriteString("--- want\n+++ got\n")
	skipped := false
	for n, l := range lines {
		if !keep[n] {
			skipped = true
			continue
		}
		if skipped {
			out.WriteString("@@\n")
			skipped = false
		}
		fmt.Fprintf(&out, "%c%s\n", l.op, l.text)
	}
	return out.String()
}

// sortedKeys returns the keys of a file map in order.
func sortedKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
