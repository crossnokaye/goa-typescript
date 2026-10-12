package tsgen

import (
	"path"
	"strings"

	"goa.design/goa/v3/codegen"
)

// renderService renders src/<service>/: one file per endpoint and the barrel.
func renderService(pkg *PackageSpec, svc *ServiceSpec) []*codegen.File {
	files := []*codegen.File{
		tsFile(pkg, path.Join("src", svc.Dir, "index.ts"), "service_index", nil, svc),
	}
	for _, ep := range svc.Endpoints {
		switch ep.Kind {
		case EndpointSSE:
			files = append(files, tsFile(pkg, path.Join("src", svc.Dir, ep.FileName+".ts"), "endpoint_sse",
				[]string{"type_decl"}, endpointData{
					EndpointSpec: ep,
					ImportBlock:  importBlock(ep.Imports, "../runtime/index.js", "../types.js"),
					Doc:          endpointDoc(ep),
				}))
		}
	}
	return files
}

// endpointData is what an endpoint template renders.
type endpointData struct {
	*EndpointSpec
	ImportBlock string
	// Doc is the JSDoc text of the function.
	Doc string
}

// endpointDoc describes a generated function: the method's description and
// the route it calls.
func endpointDoc(ep *EndpointSpec) string {
	var b strings.Builder
	if d := strings.TrimSpace(ep.Description); d != "" {
		b.WriteString(d + "\n\n")
	}
	b.WriteString(ep.Verb + " " + ep.PathTemplate)
	switch ep.Kind {
	case EndpointSSE:
		b.WriteString(" (Server-Sent Events)\n\n")
		b.WriteString("The stream does not reconnect: iterate it with `for await` and call again with\n")
		b.WriteString("`options.lastEventId` to resume.")
	}
	return b.String()
}
