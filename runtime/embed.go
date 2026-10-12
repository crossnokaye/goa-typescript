// Package runtime embeds the TypeScript runtime that the goa-typescript plugin
// copies verbatim into every generated client (gen/ts/src/runtime).
//
// The sources live in src/ and are tested with vitest; this package only
// exposes them to the generator.
package runtime

import (
	"embed"
	"io/fs"
	"path"
	"sort"
)

// Files holds the runtime sources under src/.
//
//go:embed src/*.ts
var Files embed.FS

// File is one runtime source file.
type File struct {
	// Name is the base name of the file, e.g. "sse.ts".
	Name string
	// Content is the file content.
	Content []byte
}

// Sorted returns the runtime sources in name order so that generation is
// deterministic.
func Sorted() []File {
	entries, err := fs.ReadDir(Files, "src")
	if err != nil {
		panic(err) // bug: src is embedded at build time
	}
	files := make([]File, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		content, err := fs.ReadFile(Files, path.Join("src", e.Name()))
		if err != nil {
			panic(err) // bug: the entry was just listed
		}
		files = append(files, File{Name: e.Name(), Content: content})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files
}
