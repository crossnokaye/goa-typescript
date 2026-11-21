package runtime

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestSortedMatchesSources(t *testing.T) {
	onDisk, err := filepath.Glob(filepath.Join("src", "*.ts"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(onDisk) == 0 {
		t.Fatal("no runtime sources found under src/")
	}
	sort.Strings(onDisk)

	files := Sorted()
	if len(files) != len(onDisk) {
		t.Fatalf("embedded %d files, found %d on disk", len(files), len(onDisk))
	}
	for i, f := range files {
		if want := filepath.Base(onDisk[i]); f.Name != want {
			t.Errorf("file %d: got %q, want %q", i, f.Name, want)
		}
		want, err := os.ReadFile(onDisk[i])
		if err != nil {
			t.Fatalf("read %s: %v", onDisk[i], err)
		}
		if !bytes.Equal(f.Content, want) {
			t.Errorf("%s: embedded content differs from disk", f.Name)
		}
	}
}

func TestSortedIsOrderedAndComplete(t *testing.T) {
	files := Sorted()
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("files are not sorted: %v", names)
	}
	for _, want := range []string{"client.ts", "errors.ts", "index.ts", "middleware.ts", "request.ts", "result.ts", "schema.ts", "sse.ts"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("missing runtime file %s (have %v)", want, names)
		}
	}
}

// moduleSpecifier matches the module a static import or export names
// (`from '…'`), a side-effect import (`import '…'`) or a dynamic import
// (`import('…')`), in either quote style.
var moduleSpecifier = regexp.MustCompile(`(?:\bfrom|\bimport)\s*\(?\s*(['"])([^'"\n]+)(['"])`)

// moduleSpecifiers returns the modules a source file imports, with the line
// each one is on.
func moduleSpecifiers(src string) (specs []string, lines []int) {
	for _, m := range moduleSpecifier.FindAllStringSubmatchIndex(src, -1) {
		if src[m[2]:m[3]] != src[m[6]:m[7]] {
			continue
		}
		specs = append(specs, src[m[4]:m[5]])
		lines = append(lines, strings.Count(src[:m[0]], "\n")+1)
	}
	return specs, lines
}

func TestModuleSpecifiers(t *testing.T) {
	src := `import * as z from 'zod';
import {
  a,
  type B,
} from "./a.js";
export * from './b.js';
export { c } from './c';
import './side-effect.js';
const d = await import('node:fs');
`
	specs, lines := moduleSpecifiers(src)
	want := []string{"zod", "./a.js", "./b.js", "./c", "./side-effect.js", "node:fs"}
	if strings.Join(specs, " ") != strings.Join(want, " ") {
		t.Errorf("specifiers: got %q, want %q", specs, want)
	}
	if wantLines := []int{1, 5, 6, 7, 8, 9}; fmt.Sprint(lines) != fmt.Sprint(wantLines) {
		t.Errorf("lines: got %v, want %v", lines, wantLines)
	}
}

// The generator copies these files next to each other, so they may only
// import each other (with the .js suffix bundlers expect) and zod.
func TestSourcesOnlyImportSiblingsAndZod(t *testing.T) {
	total := 0
	for _, f := range Sorted() {
		specs, lines := moduleSpecifiers(string(f.Content))
		total += len(specs)
		for i, spec := range specs {
			if spec == "zod" {
				continue
			}
			if !strings.HasPrefix(spec, "./") || !strings.HasSuffix(spec, ".js") {
				t.Errorf("%s:%d: import %q must be \"zod\" or a ./sibling.js", f.Name, lines[i], spec)
			}
		}
	}
	// The runtime imports zod and its siblings, so finding nothing means the
	// matcher no longer recognizes the sources' imports.
	if total == 0 {
		t.Error("found no imports in the runtime sources")
	}
}
