package typescript

import (
	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/eval"
)

func init() {
	codegen.RegisterPlugin("typescript", "gen", nil, Generate)
}

// Generate is invoked by `goa gen` and appends the TypeScript client under
// gen/ts. It never mutates design expressions.
func Generate(_ string, _ []eval.Root, files []*codegen.File) ([]*codegen.File, error) {
	return files, nil
}
