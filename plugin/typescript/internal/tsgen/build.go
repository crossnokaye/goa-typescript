package tsgen

import (
	"fmt"
	"sort"
	"strings"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/expr"
	goa "goa.design/goa/v3/pkg"

	tsruntime "github.com/crossnokaye/goa-typescript/runtime"
)

// Options configures Build.
type Options struct {
	// Runtime are the TypeScript runtime sources copied into the package.
	Runtime []tsruntime.File
}

// builder holds the state of one Build call.
type builder struct {
	mapper *typeMapper
	// reserved are the identifiers no generated declaration may take.
	reserved []string
	// typeNames are the identifiers of the hoisted types.
	typeNames []string
	// warnings collects remarks about the design for PackageSpec.Warnings.
	warnings []string
}

// Build turns evaluated designs into the description of the TypeScript
// package. It only reads the expressions: goa fails the generation when a
// plugin changes them.
func Build(roots []*expr.RootExpr, opts Options) (*PackageSpec, error) {
	pkg := &PackageSpec{GoaVersion: goa.Version()}
	for _, f := range opts.Runtime {
		pkg.Runtime = append(pkg.Runtime, RuntimeFile{Name: f.Name, Content: f.Content})
	}

	// Services across roots, by name; the first definition of a name wins.
	type service struct {
		root *expr.RootExpr
		http *expr.HTTPServiceExpr
	}
	var (
		services []service
		seen     = make(map[string]struct{})
	)
	for _, root := range roots {
		if root == nil || root.API == nil || root.API.HTTP == nil {
			continue
		}
		if pkg.Name == "" {
			describePackage(pkg, root.API)
		}
		for _, svc := range root.API.HTTP.Services {
			if svc.ServiceExpr == nil || disabled(svc.Meta) || disabled(svc.ServiceExpr.Meta) {
				continue
			}
			if _, dup := seen[svc.Name()]; dup {
				continue
			}
			seen[svc.Name()] = struct{}{}
			services = append(services, service{root: root, http: svc})
		}
	}
	if pkg.Name == "" {
		pkg.Name, pkg.Version = "api-client", "0.0.0"
	}
	sort.Slice(services, func(i, j int) bool { return services[i].http.Name() < services[j].http.Name() })

	b := &builder{mapper: newTypeMapper(roots...)}
	b.reserved = append(append([]string(nil), reservedGlobals...), runtimeExports(pkg.Runtime)...)

	// Hoist every user type that an HTTP endpoint can carry, including the
	// endpoints for which no function is generated yet: their types and
	// schemas are useful on their own.
	for _, svc := range services {
		for _, e := range endpoints(svc.http) {
			m := e.MethodExpr
			b.mapper.collect(m.Payload)
			b.mapper.collect(m.StreamingPayload)
			b.mapper.collect(m.Result)
			b.mapper.collect(m.StreamingResult)
			for _, he := range e.HTTPErrors {
				if he.ErrorExpr != nil {
					b.mapper.collect(he.ErrorExpr.AttributeExpr)
				}
			}
		}
	}
	typeScope := newScope(b.reserved)
	b.mapper.assignNames(typeScope)
	b.mapper.markRequests(typeScope)
	b.warnings = append(b.warnings, b.mapper.warnings...)
	typeRefs := newRefs()
	for _, d := range b.mapper.order() {
		spec := b.mapper.typeSpec(d, typeRefs)
		pkg.Types = append(pkg.Types, spec)
		b.typeNames = append(b.typeNames, spec.Name)
		if spec.Request != nil {
			b.typeNames = append(b.typeNames, spec.Request.Name)
		}
	}
	// A type never imports itself or its siblings in the same file.
	typeRefs.types = map[string]struct{}{}
	if len(pkg.Types) > 0 {
		typeRefs.zod = true
	}
	pkg.TypeImports = typeRefs.imports()

	var (
		namespaces = newScope(b.reserved, b.typeNames)
		dirs       = newScope([]string{"runtime"})
	)
	for _, svc := range services {
		spec := &ServiceSpec{
			Name:        svc.http.Name(),
			Dir:         dirs.Unique(kebab(svc.http.Name())),
			Namespace:   namespaces.Unique(camel(svc.http.Name())),
			Description: svc.http.ServiceExpr.Description,
		}
		if err := b.buildService(spec, svc.http); err != nil {
			return nil, fmt.Errorf("service %q: %w", spec.Name, err)
		}
		pkg.Services = append(pkg.Services, spec)
	}
	pkg.Warnings = b.warnings
	return pkg, nil
}

// describePackage fills the npm package metadata from the API expression.
func describePackage(pkg *PackageSpec, api *expr.APIExpr) {
	pkg.Name = kebab(api.Name) + "-client"
	if name, ok := api.Meta.Last("typescript:package"); ok && name != "" {
		pkg.Name = name
	}
	pkg.Version = api.Version
	if pkg.Version == "" {
		pkg.Version = "0.0.0"
	}
	pkg.Description = api.Description
	if pkg.Description == "" {
		pkg.Description = api.Title
	}
	if pkg.Description == "" {
		pkg.Description = "TypeScript client for the " + api.Name + " API."
	}
	pkg.Description = strings.Join(strings.Fields(pkg.Description), " ")
}

// disabled reports whether Meta("typescript:generate", "false") is set.
func disabled(meta expr.MetaExpr) bool {
	v, ok := meta.Last("typescript:generate")
	return ok && v == "false"
}

// endpoints returns the endpoints of a service that the plugin considers:
// plain HTTP ones (not JSON-RPC) that are not opted out.
func endpoints(svc *expr.HTTPServiceExpr) []*expr.HTTPEndpointExpr {
	var out []*expr.HTTPEndpointExpr
	for _, e := range svc.HTTPEndpoints {
		if e.MethodExpr == nil || e.IsJSONRPC() || disabled(e.Meta) || disabled(e.MethodExpr.Meta) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// buildService builds the endpoints of one service, in design order.
func (b *builder) buildService(spec *ServiceSpec, svc *expr.HTTPServiceExpr) error {
	// The barrel re-exports every declaration of every endpoint file, so the
	// names are unique across the service, and distinct from the runtime
	// names and hoisted types the files import.
	var (
		names = newScope(b.reserved, b.typeNames)
		files = newScope([]string{"index"})
	)
	skip := func(method, reason string) {
		spec.Skipped = append(spec.Skipped, SkippedEndpoint{Method: method, Reason: reason})
	}
	for _, e := range endpoints(svc) {
		var (
			method = e.MethodExpr.Name
			sse    = e.UsesSSE()
			ws     = e.UsesWebSocket()
			// plain is true when a function returns the method's result
			// from an ordinary response.
			plain = !ws && (!sse || e.MethodExpr.HasMixedResults())
		)
		switch {
		case sse:
			spec.HasSSE = true
			spec.HasREST = spec.HasREST || plain
		case ws:
			spec.HasWS = true
		default:
			spec.HasREST = true
		}
		if reason := unsupported(e); reason != "" {
			skip(method, reason)
			continue
		}
		if ws {
			if reason := unsupportedWebSocket(e); reason != "" {
				skip(method, reason)
				continue
			}
		}
		if plain {
			if reason := unsupportedREST(e); reason != "" {
				if !sse {
					skip(method, reason)
					continue
				}
				// Only the stream of a mixed method can be generated.
				skip(method, "plain response: "+reason)
				plain = false
			}
		}
		var (
			ep  *EndpointSpec
			err error
		)
		switch {
		case ws:
			// goa refuses a WebSocket method with both a Result and a
			// StreamingResult, so there is no mixed case here.
			ep, err = b.buildWebSocketEndpoint(e, names, files)
		case plain:
			ep, err = b.buildRESTEndpoint(e, names, files)
		default:
			ep, err = b.buildSSEEndpoint(e, names, files)
		}
		if err != nil {
			return fmt.Errorf("method %q: %w", method, err)
		}
		for _, r := range ep.Responses {
			if len(r.Missing) > 0 {
				b.warnings = append(b.warnings, fmt.Sprintf(
					"service %q, method %q: the %d response does not carry the required result attribute(s) %s; "+
						"the generated client rejects that response with a validation error",
					spec.Name, method, r.Status, strings.Join(r.Missing, ", ")))
			}
		}
		spec.Endpoints = append(spec.Endpoints, ep)
	}
	return nil
}

// unsupported returns why no function can be generated for an endpoint, or "".
func unsupported(e *expr.HTTPEndpointExpr) string {
	switch {
	case route(e) == nil:
		return "the endpoint has no route"
	case e.MultipartRequest:
		return "multipart requests are not supported"
	case e.FormRequest:
		return "form-encoded requests are not supported"
	}
	return ""
}

// route returns the route a generated function calls: the first one that is
// not an OPTIONS route.
func route(e *expr.HTTPEndpointExpr) *expr.RouteExpr {
	for _, r := range e.Routes {
		if r.Method != "OPTIONS" && len(r.FullPaths()) > 0 {
			return r
		}
	}
	return nil
}

// buildEndpointBase builds what every endpoint file has whatever its
// transport style: the names, the security bindings and the request. The
// caller adds the style's own parts, then calls finishEndpoint.
func (b *builder) buildEndpointBase(
	e *expr.HTTPEndpointExpr, kind EndpointKind, funcName, accept string, scope, files *codegen.NameScope, refs *refs,
) (*EndpointSpec, error) {
	m := e.MethodExpr
	r := route(e)
	ep := &EndpointSpec{
		MethodName:   m.Name,
		FileName:     files.Unique(kebab(m.Name)),
		Description:  m.Description,
		Kind:         kind,
		Mixed:        m.HasMixedResults(),
		Verb:         r.Method,
		PathTemplate: r.FullPaths()[0],
		Accept:       accept,
		RawBody:      e.SkipRequestBodyEncodeDecode,
	}
	ep.FuncName = scope.Unique(funcName)
	// Every other declaration derives from the function name, which is
	// already unique.
	ep.SpecName = scope.Unique(ep.FuncName + "Spec")
	ep.ErrorsName = scope.Unique(ep.FuncName + "Errors")

	security, strip := securityBindings(e)
	ep.Security = security
	if e.SSE != nil && e.SSE.RequestIDField != "" {
		// Supplied through options.lastEventId instead.
		strip[e.SSE.RequestIDField] = struct{}{}
	}
	req, err := b.buildRequest(e, strip, scope.Unique(upperFirst(ep.FuncName)+"Payload"), refs)
	if err != nil {
		return nil, err
	}
	ep.Request = req
	return ep, nil
}

// finishEndpoint adds the parts that depend on the style-specific ones: the
// errors, the transport mapping and the imports.
func (b *builder) finishEndpoint(e *expr.HTTPEndpointExpr, ep *EndpointSpec, scope *codegen.NameScope, refs *refs) {
	ep.ErrorTypeName = scope.Unique(upperFirst(ep.FuncName) + "Error")
	b.buildErrors(e, ep, scope, refs)
	ep.SpecTS = endpointSpecLiteral(ep, ep.Accept)
	if ep.Stream != nil {
		ep.Stream.SpecTS = endpointSpecLiteral(ep, ep.Stream.Accept)
	}

	refs.useRuntime(
		"type CallResult", "type Client", "type ClientError", "type EndpointSpec", "type GoaError",
		"type OtherErrorName", "goaErrorDecoder", "send",
	)
	if ep.Kind == EndpointREST {
		refs.useRuntime("type RequestOptions", "decodeResponse")
	}
	if ep.SSE != nil {
		refs.useRuntime("type StreamOptions", "EventStream", "sseDecoder", "UnexpectedResponseError")
	}
	if ep.Request != nil {
		refs.useRuntime("encode")
	}
	ep.Imports = refs.imports()
}

// buildSSEEndpoint builds the function for a Server-Sent Events endpoint.
func (b *builder) buildSSEEndpoint(e *expr.HTTPEndpointExpr, scope, files *codegen.NameScope) (*EndpointSpec, error) {
	refs := newRefs()
	funcName := camel(e.MethodExpr.Name)
	if e.MethodExpr.HasMixedResults() {
		// The plain function, were it generated, would have the method name.
		funcName += "Stream"
	}
	ep, err := b.buildEndpointBase(e, EndpointSSE, funcName, "text/event-stream", scope, files, refs)
	if err != nil {
		return nil, err
	}
	ep.SSE = b.buildSSE(e, scope.Unique(upperFirst(ep.FuncName)+"Event"), refs)
	ep.ResultTS = "EventStream<" + ep.SSE.EventTypeName + ">"
	b.finishEndpoint(e, ep, scope, refs)
	return ep, nil
}

// buildRESTEndpoint builds the function for a unary endpoint. A mixed method
// gets its Server-Sent Events function in the same file.
func (b *builder) buildRESTEndpoint(e *expr.HTTPEndpointExpr, scope, files *codegen.NameScope) (*EndpointSpec, error) {
	refs := newRefs()
	ep, err := b.buildEndpointBase(e, EndpointREST, camel(e.MethodExpr.Name), "application/json", scope, files, refs)
	if err != nil {
		return nil, err
	}
	ep.ResponsesName = scope.Unique(ep.FuncName + "Responses")
	b.buildResponses(e, ep, scope, refs)
	if ep.Mixed {
		// The plain function keeps the method name; the stream is its sibling.
		stream := &StreamFuncSpec{FuncName: scope.Unique(ep.FuncName + "Stream"), Accept: "text/event-stream"}
		stream.SpecName = scope.Unique(stream.FuncName + "Spec")
		ep.Stream = stream
		ep.SSE = b.buildSSE(e, scope.Unique(upperFirst(stream.FuncName)+"Event"), refs)
	}
	b.finishEndpoint(e, ep, scope, refs)
	return ep, nil
}

// buildErrors fills the error table and the error union of an endpoint.
func (b *builder) buildErrors(e *expr.HTTPEndpointExpr, ep *EndpointSpec, scope *codegen.NameScope, refs *refs) {
	c := &renderCtx{m: b.mapper, refs: refs}
	seen := make(map[string]struct{})
	for _, he := range e.HTTPErrors {
		if he.Response == nil || he.ErrorExpr == nil {
			continue
		}
		key := fmt.Sprintf("%d/%s", he.Response.StatusCode, he.Name)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		att := he.ErrorExpr.AttributeExpr
		spec := &ErrorSpec{
			Name:      he.Name,
			Status:    he.Response.StatusCode,
			Default:   expr.IsErrorResult(att.Type),
			Temporary: hasMeta(att, "goa:error:temporary"),
			Timeout:   hasMeta(att, "goa:error:timeout"),
			Fault:     hasMeta(att, "goa:error:fault"),
		}
		switch {
		case spec.Default || att.Type == nil || att.Type == expr.Empty:
			// Modelled by the runtime.
		case b.hoisted(att.Type):
			// The schema is used as a value, so the reference registers the import.
			spec.TypeName = c.zod(att, "", false).code
		default:
			// A type the design does not name: Error("x", func() { ... }) or
			// Error("x", String).
			spec.TypeName = scope.Unique(upperFirst(ep.FuncName) + tsIdent(he.Name) + "Detail")
			spec.Inline = true
			spec.TSDef = c.tsType(att, "", false)
			z := c.zod(att, "", false)
			spec.Zod, spec.ZodNote = z.code, z.note()
		}
		if spec.TypeName != "" {
			spec.Headers = errorHeaders(he.Response, att)
		}
		ep.Errors = append(ep.Errors, spec)
	}
	sort.SliceStable(ep.Errors, func(i, j int) bool { return ep.Errors[i].Status < ep.Errors[j].Status })

	// Table: { 404: [{ name: "not_found" }], ... } as const
	if len(ep.Errors) == 0 {
		ep.ErrorTable = "{} as const"
	} else {
		var t strings.Builder
		t.WriteString("{\n")
		for i := 0; i < len(ep.Errors); {
			j := i
			var entries []string
			for ; j < len(ep.Errors) && ep.Errors[j].Status == ep.Errors[i].Status; j++ {
				entry := "{ name: " + tsString(ep.Errors[j].Name)
				if ep.Errors[j].TypeName != "" {
					entry += ", schema: " + ep.Errors[j].TypeName
				}
				if len(ep.Errors[j].Headers) > 0 {
					entry += ", headers: " + headersLiteral(ep.Errors[j].Headers)
				}
				entries = append(entries, entry+" }")
			}
			fmt.Fprintf(&t, "  %d: [%s],\n", ep.Errors[i].Status, strings.Join(entries, ", "))
			i = j
		}
		t.WriteString("} as const")
		ep.ErrorTable = t.String()
	}

	// Union: declared errors by name, then the open-ended ones.
	var u strings.Builder
	named := make(map[string]struct{})
	for _, spec := range ep.Errors {
		member := "GoaError<" + tsString(spec.Name)
		if spec.TypeName != "" {
			member += ", " + spec.TypeName
		}
		member += ">"
		if _, dup := named[member]; dup {
			continue
		}
		named[member] = struct{}{}
		u.WriteString("\n  | " + member)
	}
	u.WriteString("\n  | GoaError<OtherErrorName>\n  | ClientError")
	ep.ErrorTS = u.String()
}

// hoisted reports whether t is a type declared in src/types.ts.
func (b *builder) hoisted(t expr.DataType) bool {
	_, ok := b.mapper.hoist(t)
	return ok
}

func hasMeta(att *expr.AttributeExpr, key string) bool {
	_, ok := att.Meta[key]
	return ok
}

// endpointSpecLiteral renders the runtime EndpointSpec of an endpoint for the
// function that sends the given Accept header.
func endpointSpecLiteral(ep *EndpointSpec, accept string) string {
	paramMap := func(params []ParamSpec) string {
		if len(params) == 0 {
			return "{}"
		}
		parts := make([]string, len(params))
		for i, p := range params {
			parts[i] = tsPropKey(p.Prop) + ": " + tsString(p.Wire)
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	var b strings.Builder
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  method: %s,\n", tsString(ep.Verb))
	fmt.Fprintf(&b, "  path: %s,\n", tsString(ep.PathTemplate))
	req := ep.Request
	if req == nil {
		req = &RequestSpec{Body: BodySpec{Kind: BodyNone}}
	}
	fmt.Fprintf(&b, "  pathParams: %s,\n", paramMap(req.Path))
	fmt.Fprintf(&b, "  queryParams: %s,\n", paramMap(req.Query))
	fmt.Fprintf(&b, "  headerParams: %s,\n", paramMap(req.Headers))
	if req.MapQuery != nil {
		fmt.Fprintf(&b, "  mapQuery: %s,\n", tsString(req.MapQuery.Prop))
	}
	switch {
	case ep.RawBody:
		b.WriteString("  body: { kind: \"raw\" },\n")
	case req.Body.Kind == BodyFields:
		fields := make([]string, len(req.Body.Fields))
		for i, f := range req.Body.Fields {
			fields[i] = tsString(f)
		}
		fmt.Fprintf(&b, "  body: { kind: \"fields\", fields: [%s] },\n", strings.Join(fields, ", "))
	case req.Body.Kind == BodyAttr:
		fmt.Fprintf(&b, "  body: { kind: \"attr\", attr: %s },\n", tsString(req.Body.Attr))
	case req.Body.Kind == BodyWhole:
		b.WriteString("  body: { kind: \"whole\" },\n")
	}
	fmt.Fprintf(&b, "  accept: %s,\n", tsString(accept))
	if len(ep.Security) == 0 {
		b.WriteString("  security: [],\n")
	} else {
		b.WriteString("  security: [\n")
		for _, s := range ep.Security {
			if s.Kind == "basic" {
				fmt.Fprintf(&b, "    { scheme: %s, kind: \"basic\" },\n", tsString(s.Scheme))
				continue
			}
			fmt.Fprintf(&b, "    { scheme: %s, kind: %s, in: %s, name: %s },\n",
				tsString(s.Scheme), tsString(s.Kind), tsString(s.In), tsString(s.Name))
		}
		b.WriteString("  ],\n")
	}
	b.WriteString("}")
	return b.String()
}
