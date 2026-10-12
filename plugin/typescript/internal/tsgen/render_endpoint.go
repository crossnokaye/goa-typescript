package tsgen

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"goa.design/goa/v3/codegen"
)

// renderService renders src/<service>/: one file per endpoint and the barrel.
func renderService(pkg *PackageSpec, svc *ServiceSpec) []*codegen.File {
	files := []*codegen.File{
		tsFile(pkg, path.Join("src", svc.Dir, "index.ts"), "service_index", nil, svc),
	}
	partials := []string{"type_decl", "endpoint_types", "endpoint_errors", "rest_func", "sse_func"}
	for _, ep := range svc.Endpoints {
		data := endpointData{
			EndpointSpec:   ep,
			ImportBlock:    importBlock(ep.Imports, "../runtime/index.js", "../types.js"),
			StreamFuncName: ep.FuncName,
		}
		tmpl := "endpoint_sse"
		switch {
		case ep.Kind == EndpointREST:
			tmpl = "endpoint_rest"
			data.Doc = restDoc(ep)
			if ep.Stream != nil {
				data.StreamFuncName = ep.Stream.FuncName
				data.StreamDoc = sseDoc(ep)
			}
		default:
			data.Doc = sseDoc(ep)
		}
		files = append(files, tsFile(pkg, path.Join("src", svc.Dir, ep.FileName+".ts"), tmpl, partials, data))
	}
	return files
}

// endpointData is what an endpoint template renders.
type endpointData struct {
	*EndpointSpec
	ImportBlock string
	// Doc is the JSDoc text of the function named FuncName.
	Doc string
	// StreamFuncName is the name of the Server-Sent Events function, if any.
	StreamFuncName string
	// StreamDoc is the JSDoc text of a mixed method's stream function.
	StreamDoc string
}

// docHead starts the JSDoc of a generated function: the method's description
// and the route it calls.
func docHead(ep *EndpointSpec) string {
	var b strings.Builder
	if d := strings.TrimSpace(ep.Description); d != "" {
		b.WriteString(d + "\n\n")
	}
	b.WriteString(ep.Verb + " " + ep.PathTemplate)
	return b.String()
}

// sseDoc describes a Server-Sent Events function.
func sseDoc(ep *EndpointSpec) string {
	return docHead(ep) + " (Server-Sent Events)\n\n" +
		"The stream does not reconnect: iterate it with `for await` and call again with\n" +
		"`options.lastEventId` to resume."
}

// restDoc describes a REST function, including what a caller cannot tell
// from its signature.
func restDoc(ep *EndpointSpec) string {
	var b strings.Builder
	b.WriteString(docHead(ep))
	note := func(lines ...string) {
		b.WriteString("\n\n" + strings.Join(lines, "\n"))
	}
	if ep.RawBody {
		note("`body` is sent as the request body, as is. A `ReadableStream` does not work in",
			"every browser; a `Blob`, an `ArrayBuffer`, `FormData` or a string does.")
		if ep.Request == nil {
			note("The method has no payload: pass `undefined` as `payload`, which keeps `body` in",
				"the place every function has it.")
		}
	}
	if len(ep.Responses) > 1 {
		parts := make([]string, len(ep.Responses))
		for i, r := range ep.Responses {
			parts[i] = strconv.Itoa(r.Status)
			if r.Tag[0] != "" {
				parts[i] += " (`" + r.Tag[0] + "` is " + tsString(r.Tag[1]) + ")"
			}
		}
		note("Succeeds with " + strings.Join(parts[:len(parts)-1], ", ") + " or " + parts[len(parts)-1] +
			": `result.response.status` says which.")
	}
	raw := false
	for _, r := range ep.Responses {
		raw = raw || r.Body == BodyRaw
		if len(r.Missing) > 0 {
			note(fmt.Sprintf("A %d response does not carry %s, which the result requires: it fails with a",
				r.Status, codeList(r.Missing)),
				"`ResponseValidationError`.")
		}
		if len(r.Cookies) > 0 {
			note(fmt.Sprintf("%s travels in a response cookie, which JavaScript cannot read: it is never set.",
				codeList(r.Cookies)))
		}
	}
	if raw {
		note("The response body is not read: consume it from `result.response` with `blob()`,",
			"`arrayBuffer()` or `body`.")
	}
	return b.String()
}

// codeList renders names as `a`, `b` and `c`.
func codeList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	if len(quoted) < 2 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " and " + quoted[len(quoted)-1]
}
