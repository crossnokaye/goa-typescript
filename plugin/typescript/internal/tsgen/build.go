package tsgen

import (
	"fmt"
	"sort"
	"strings"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/expr"
	goa "goa.design/goa/v3/pkg"

	tsruntime "github.com/xeger/goa-typescript/runtime"
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

	b := &builder{mapper: newTypeMapper()}
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
	b.mapper.assignNames(newScope(b.reserved))
	typeRefs := newRefs()
	for _, ut := range b.mapper.order() {
		spec := b.mapper.typeSpec(ut, typeRefs)
		pkg.Types = append(pkg.Types, spec)
		b.typeNames = append(b.typeNames, spec.Name)
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
	for _, e := range endpoints(svc) {
		method := e.MethodExpr.Name
		switch {
		case e.UsesSSE():
			spec.HasSSE = true
		case e.UsesWebSocket():
			spec.HasWS = true
			spec.Skipped = append(spec.Skipped, SkippedEndpoint{Method: method, Reason: "WebSocket endpoints are not generated yet"})
			continue
		default:
			spec.HasREST = true
			spec.Skipped = append(spec.Skipped, SkippedEndpoint{Method: method, Reason: "REST endpoints are not generated yet"})
			continue
		}
		if reason := unsupported(e); reason != "" {
			spec.Skipped = append(spec.Skipped, SkippedEndpoint{Method: method, Reason: reason})
			continue
		}
		ep, err := b.buildSSEEndpoint(e, names, files)
		if err != nil {
			return fmt.Errorf("method %q: %w", method, err)
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

// buildSSEEndpoint builds the function for a Server-Sent Events endpoint.
func (b *builder) buildSSEEndpoint(e *expr.HTTPEndpointExpr, scope, files *codegen.NameScope) (*EndpointSpec, error) {
	m := e.MethodExpr
	r := route(e)
	refs := newRefs()

	ep := &EndpointSpec{
		MethodName:   m.Name,
		FileName:     files.Unique(kebab(m.Name)),
		Description:  m.Description,
		Kind:         EndpointSSE,
		Mixed:        m.HasMixedResults(),
		Verb:         r.Method,
		PathTemplate: r.FullPaths()[0],
		Accept:       "text/event-stream",
	}
	funcName := camel(m.Name)
	if ep.Mixed {
		// The plain function keeps the method name; the stream is its sibling.
		funcName += "Stream"
	}
	ep.FuncName = scope.Unique(funcName)
	// Every other declaration derives from the function name, which is
	// already unique.
	base := upperFirst(ep.FuncName)
	ep.SpecName = scope.Unique(ep.FuncName + "Spec")
	ep.ErrorsName = scope.Unique(ep.FuncName + "Errors")

	security, strip := securityBindings(e)
	ep.Security = security
	if e.SSE != nil && e.SSE.RequestIDField != "" {
		// Supplied through options.lastEventId instead.
		strip[e.SSE.RequestIDField] = struct{}{}
	}
	req, err := b.buildRequest(e, strip, scope.Unique(base+"Payload"), refs)
	if err != nil {
		return nil, err
	}
	ep.Request = req

	ep.SSE = b.buildSSE(e, scope.Unique(base+"Event"), refs)
	ep.ResultTS = "EventStream<" + ep.SSE.EventTypeName + ">"

	ep.ErrorTypeName = scope.Unique(base + "Error")
	b.buildErrors(e, ep, refs)
	ep.SpecTS = endpointSpecLiteral(ep)

	refs.useRuntime(
		"type Client", "type ClientError", "type EndpointSpec", "type GoaError", "type OtherErrorName",
		"type Result", "type StreamOptions",
		"err", "EventStream", "goaErrorDecoder", "ok", "send", "sseDecoder", "UnexpectedResponseError",
	)
	if ep.Request != nil {
		refs.useRuntime("encode")
	}
	ep.Imports = refs.imports()
	return ep, nil
}

// buildErrors fills the error table and the error union of an endpoint.
func (b *builder) buildErrors(e *expr.HTTPEndpointExpr, ep *EndpointSpec, refs *refs) {
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
		if _, ok := hoistable(att.Type); ok && expr.IsObject(att.Type) {
			spec.TypeName = c.tsType(att, "", false)
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

func hasMeta(att *expr.AttributeExpr, key string) bool {
	_, ok := att.Meta[key]
	return ok
}

// endpointSpecLiteral renders the runtime EndpointSpec of an endpoint.
func endpointSpecLiteral(ep *EndpointSpec) string {
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
	switch req.Body.Kind {
	case BodyFields:
		fields := make([]string, len(req.Body.Fields))
		for i, f := range req.Body.Fields {
			fields[i] = tsString(f)
		}
		fmt.Fprintf(&b, "  body: { kind: \"fields\", fields: [%s] },\n", strings.Join(fields, ", "))
	case BodyAttr:
		fmt.Fprintf(&b, "  body: { kind: \"attr\", attr: %s },\n", tsString(req.Body.Attr))
	case BodyWhole:
		b.WriteString("  body: { kind: \"whole\" },\n")
	}
	fmt.Fprintf(&b, "  accept: %s,\n", tsString(ep.Accept))
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
