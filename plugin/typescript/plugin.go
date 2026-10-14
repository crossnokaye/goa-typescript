package typescript

import (
	"fmt"
	"os"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/eval"
	"goa.design/goa/v3/expr"

	"github.com/crossnokaye/goa-typescript/plugin/typescript/internal/tsgen"
	tsruntime "github.com/crossnokaye/goa-typescript/runtime"
)

func init() {
	codegen.RegisterPlugin("typescript", "gen", nil, Generate)
}

// Generate is invoked by `goa gen` and appends the TypeScript client under
// gen/ts. It never mutates design expressions: goa verifies the design after
// every plugin and fails the generation if one changed it.
//
// Set GOA_TYPESCRIPT_DEBUG to also write gen/typescript_debug.json, the data
// the templates are rendered from.
func Generate(_ string, roots []eval.Root, files []*codegen.File) ([]*codegen.File, error) {
	// Goa may provide multiple roots (e.g. when the design imports
	// subpackages); only roots that describe an HTTP API matter here.
	var designs []*expr.RootExpr
	for _, r := range roots {
		root, ok := r.(*expr.RootExpr)
		if !ok || root.API == nil || root.API.HTTP == nil {
			continue
		}
		designs = append(designs, root)
	}
	if len(designs) == 0 {
		return files, nil
	}
	spec, err := tsgen.Build(designs, tsgen.Options{Runtime: tsruntime.Sorted()})
	if err != nil {
		return nil, fmt.Errorf("goa-typescript: %w", err)
	}
	// goa prints nothing of its own between the plugins and the list of
	// written files, so the warnings land right above that list.
	for _, w := range spec.Warnings {
		fmt.Fprintln(os.Stderr, "goa-typescript: warning: "+w)
	}
	if os.Getenv("GOA_TYPESCRIPT_DEBUG") != "" {
		files = append(files, tsgen.DebugFile(spec))
	}
	return append(files, tsgen.Render(spec)...), nil
}
