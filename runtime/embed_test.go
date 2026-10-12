package runtime

import (
	"bytes"
	"os"
	"path/filepath"
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

// The generator copies these files next to each other, so they may only
// import each other (with the .js suffix bundlers expect) and zod.
func TestSourcesOnlyImportSiblingsAndZod(t *testing.T) {
	for _, f := range Sorted() {
		for n, line := range strings.Split(string(f.Content), "\n") {
			_, spec, ok := strings.Cut(line, " from \"")
			if !ok || !(strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "export ") || strings.HasPrefix(line, "} from")) {
				continue
			}
			spec = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(spec), ";"), "\"")
			if spec == "zod" {
				continue
			}
			if !strings.HasPrefix(spec, "./") || !strings.HasSuffix(spec, ".js") {
				t.Errorf("%s:%d: import %q must be \"zod\" or a ./sibling.js", f.Name, n+1, spec)
			}
		}
	}
}
