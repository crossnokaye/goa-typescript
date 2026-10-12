package tsgen

import (
	"path"

	"goa.design/goa/v3/codegen"
)

// renderTypes renders src/types.ts: every hoisted design type with its schema.
func renderTypes(pkg *PackageSpec) *codegen.File {
	return tsFile(pkg, path.Join("src", "types.ts"), "types", []string{"type_decl"}, map[string]any{
		"ImportBlock": importBlock(pkg.TypeImports, "./runtime/index.js", ""),
		"Types":       pkg.Types,
	})
}
