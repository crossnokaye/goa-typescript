package tsgen

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/expr"
)

// The TanStack Query layer (ADR 0006): with Meta("typescript:query",
// "tanstack") on the API, every generated function that reads, streams or
// writes gets one-line exports in <method>.query.ts that call the runtime's
// query helpers (runtime/src/query.ts).

const (
	metaQuery       = "typescript:query"
	metaQueryKind   = "typescript:query:kind"
	metaInvalidates = "typescript:invalidates"
)

// queryEnabled reads Meta("typescript:query") on the API.
func queryEnabled(api *expr.APIExpr) (bool, error) {
	v, ok := api.Meta.Last(metaQuery)
	if !ok {
		return false, nil
	}
	if v != "tanstack" {
		return false, fmt.Errorf("Meta(%q, %q) on the API: the only supported value is \"tanstack\"", metaQuery, v)
	}
	return true, nil
}

// queryOverride returns the classification Meta("typescript:query:kind")
// forces on a method, or "".
func queryOverride(m *expr.MethodExpr) (QueryKind, error) {
	v, ok := m.Meta.Last(metaQueryKind)
	if !ok {
		return "", nil
	}
	switch k := QueryKind(v); k {
	case QueryRead, QueryMutation, QueryNone:
		return k, nil
	}
	return "", fmt.Errorf("Meta(%q, %q): the value must be \"query\", \"mutation\" or \"none\"", metaQueryKind, v)
}

// checkSkippedQuery reports an override that asks for query options on a
// method the SDK does not generate.
func checkSkippedQuery(m *expr.MethodExpr, reason string) error {
	k, err := queryOverride(m)
	if err != nil {
		return err
	}
	if k == QueryRead || k == QueryMutation {
		return fmt.Errorf("Meta(%q, %q) cannot apply: no function is generated (%s)", metaQueryKind, k, reason)
	}
	return nil
}

// classifyQuery decides what the TanStack Query layer generates for an
// endpoint and records it on the endpoint and, for nothing, on the service.
func classifyQuery(e *expr.HTTPEndpointExpr, ep *EndpointSpec, svc *ServiceSpec) error {
	m := e.MethodExpr
	override, err := queryOverride(m)
	if err != nil {
		return err
	}
	none := func(reason string) {
		svc.QueryNone = append(svc.QueryNone, SkippedEndpoint{Method: m.Name, Reason: reason})
	}
	if override == QueryNone {
		none(fmt.Sprintf("Meta(%q, \"none\")", metaQueryKind))
		return checkInvalidatesMeta(m, false)
	}

	required := ep.Request != nil && !ep.Request.Optional
	requestName := ""
	if ep.Request != nil {
		requestName = ep.Request.TypeName
	}
	add := func(kind QueryKind, funcName, specName string) *QuerySpec {
		q := &QuerySpec{
			Kind: kind, FuncName: funcName, SpecName: specName, Verb: ep.Verb, Path: ep.PathTemplate,
			RequestName: requestName, Required: required,
		}
		ep.Queries = append(ep.Queries, q)
		return q
	}

	switch ep.Kind {
	case EndpointREST:
		var (
			rawDownload = e.SkipResponseBodyEncodeDecode
			kind        QueryKind
		)
		switch {
		case override == QueryRead:
			switch {
			case rawDownload:
				return fmt.Errorf("Meta(%q, \"query\") cannot apply: the response body is not decoded (SkipResponseBodyEncodeDecode)", metaQueryKind)
			case ep.RawBody:
				return fmt.Errorf("Meta(%q, \"query\") cannot apply: the request body is raw (SkipRequestBodyEncodeDecode)", metaQueryKind)
			}
			kind = QueryRead
		case override == QueryMutation:
			if rawDownload {
				return fmt.Errorf("Meta(%q, \"mutation\") cannot apply: the response body is not decoded (SkipResponseBodyEncodeDecode)", metaQueryKind)
			}
			kind = QueryMutation
		case rawDownload:
			kind = QueryNone
		case ep.Verb == "GET" || ep.Verb == "HEAD":
			kind = QueryRead
		default:
			kind = QueryMutation
		}
		if kind == QueryNone {
			none("the response body is not decoded (SkipResponseBodyEncodeDecode)")
		} else {
			q := add(kind, ep.FuncName, ep.SpecName)
			q.Raw = ep.RawBody
		}
		if ep.Stream != nil {
			add(QueryStream, ep.Stream.FuncName, ep.Stream.SpecName)
		}
	case EndpointSSE:
		if override != "" {
			return fmt.Errorf("Meta(%q, %q) cannot apply: the method is a Server-Sent Events stream", metaQueryKind, override)
		}
		add(QueryStream, ep.FuncName, ep.SpecName)
	case EndpointWebSocket:
		if ep.WS.Kind != "server" {
			if override != "" {
				return fmt.Errorf("Meta(%q, %q) cannot apply: the WebSocket method streams a payload", metaQueryKind, override)
			}
			none("the WebSocket method streams a payload")
			break
		}
		if override != "" {
			return fmt.Errorf("Meta(%q, %q) cannot apply: the method is a WebSocket stream", metaQueryKind, override)
		}
		add(QueryStream, ep.FuncName, ep.SpecName)
	}
	mutation := len(ep.Queries) > 0 && ep.Queries[0].Kind == QueryMutation
	if err := checkInvalidatesMeta(m, mutation); err != nil {
		return err
	}
	ep.ExportSpec = len(ep.Queries) > 0
	return nil
}

// checkInvalidatesMeta rejects Meta("typescript:invalidates") on a method
// that is not a mutation.
func checkInvalidatesMeta(m *expr.MethodExpr, mutation bool) error {
	if _, ok := m.Meta.Last(metaInvalidates); ok && !mutation {
		return fmt.Errorf("Meta(%q) applies to mutations only", metaInvalidates)
	}
	return nil
}

// nameQueries names the query exports of a service, once every function of
// the service has its name, and resolves the invalidation of its mutations.
func nameQueries(svc *ServiceSpec, methods map[string]*expr.MethodExpr, names *codegen.NameScope) error {
	// The query and stream functions of the service, by method name.
	reads := make(map[string][]*QuerySpec)
	for _, ep := range svc.Endpoints {
		for _, q := range ep.Queries {
			switch q.Kind {
			case QueryRead, QueryStream:
				q.Name = names.Unique(q.FuncName + "Query")
				q.KeyName = names.Unique(q.FuncName + "Key")
				reads[ep.MethodName] = append(reads[ep.MethodName], q)
			case QueryMutation:
				q.InvalidatesName = names.Unique(q.FuncName + "Invalidates")
				q.Name = names.Unique(q.FuncName + "Mutation")
			}
		}
	}
	for _, ep := range svc.Endpoints {
		for _, q := range ep.Queries {
			if q.Kind != QueryMutation {
				continue
			}
			v, ok := methods[ep.MethodName].Meta.Last(metaInvalidates)
			if !ok {
				q.Invalidates = []string{defaultInvalidation(q.Path)}
				continue
			}
			for _, name := range strings.Split(v, ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				targets, found := reads[name]
				if !found {
					if _, exists := methods[name]; exists {
						return fmt.Errorf("method %q: Meta(%q) names %q, which has no query options",
							ep.MethodName, metaInvalidates, name)
					}
					return fmt.Errorf("method %q: Meta(%q) names %q, which is not a method of the service",
						ep.MethodName, metaInvalidates, name)
				}
				for _, t := range targets {
					prefix := staticPrefix(t.Path)
					if !slices.Contains(q.Invalidates, prefix) {
						q.Invalidates = append(q.Invalidates, prefix)
					}
				}
			}
		}
	}
	return nil
}

// pathParam matches a path parameter, `{name}` or `{*name}`.
var pathParam = regexp.MustCompile(`\{\*?([^}]*)\}`)

// segments splits a goa path template, dropping empty segments.
func segments(path string) []string {
	var out []string
	for _, s := range strings.Split(path, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isParam(segment string) bool { return strings.Contains(segment, "{") }

// defaultInvalidation is the path whose key a mutation invalidates: its own,
// trailing parameters stripped.
func defaultInvalidation(path string) string {
	segs := segments(path)
	for len(segs) > 0 && isParam(segs[len(segs)-1]) {
		segs = segs[:len(segs)-1]
	}
	return "/" + strings.Join(segs, "/")
}

// staticPrefix is the path made of the static segments of a route up to its
// first parameter: what keyPrefixFor returns.
func staticPrefix(path string) string {
	var out []string
	for _, s := range segments(path) {
		if isParam(s) {
			break
		}
		out = append(out, s)
	}
	return "/" + strings.Join(out, "/")
}

// normalizedPath names every parameter of a route alike, so that two routes
// that differ only by parameter names compare equal.
func normalizedPath(path string) string {
	return "/" + strings.Join(segments(pathParam.ReplaceAllString(path, "{}")), "/")
}

// queryRef identifies a query export across services.
type queryRef struct {
	svc *ServiceSpec
	ep  *EndpointSpec
	q   *QuerySpec
}

// finishQueries runs once every service is built: it detects key collisions
// across services, finds which mutations invalidate which keys, and renders
// the query files' declarations.
func finishQueries(services []*ServiceSpec) {
	var reads, writes []queryRef
	for _, svc := range services {
		for _, ep := range svc.Endpoints {
			for _, q := range ep.Queries {
				if q.Kind == QueryMutation {
					writes = append(writes, queryRef{svc, ep, q})
				} else {
					reads = append(reads, queryRef{svc, ep, q})
				}
			}
		}
	}
	ref := func(from *ServiceSpec, r queryRef, name string) string {
		if r.svc != from {
			return r.svc.Namespace + "." + name
		}
		return name
	}

	// Collisions: equal routes once parameter names are ignored.
	groups := make(map[string][]queryRef)
	var order []string
	for _, r := range reads {
		k := normalizedPath(r.q.Path)
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], r)
	}
	for _, k := range order {
		group := groups[k]
		if len(group) < 2 {
			continue
		}
		gets := 0
		for _, r := range group {
			if r.q.Verb == "GET" {
				gets++
			}
		}
		for _, r := range group {
			// A GET read keeps its clean key; everything else that shares
			// the route is told apart by its verb. Two GETs on one route
			// are the plain function and the stream of a mixed method: the
			// stream is the one discriminated, as a stream.
			switch {
			case r.q.Verb != "GET":
				r.q.Discriminator = "method"
			case r.q.Kind == QueryStream && gets > 1:
				r.q.Discriminator = "stream"
			}
		}
		for _, r := range group {
			for _, o := range group {
				if o.q != r.q && o.q.Discriminator != "" {
					mark := "`" + discriminatorMark(o.q) + "`"
					if !slices.Contains(r.q.OtherMarks, mark) {
						r.q.OtherMarks = append(r.q.OtherMarks, mark)
					}
				}
			}
			for _, o := range group {
				if o.q != r.q {
					r.q.Collisions = append(r.q.Collisions,
						fmt.Sprintf("`%s %s` (`%s`)", o.q.Verb, o.q.Path, ref(r.svc, o, o.q.Name)))
				}
			}
		}
	}

	// Invalidation: a mutation's template invalidates every key it is a
	// prefix of.
	for _, r := range reads {
		for _, w := range writes {
			for _, t := range w.q.Invalidates {
				if invalidates(t, r.q.Path) {
					r.q.InvalidatedBy = append(r.q.InvalidatedBy, "`"+ref(r.svc, w, w.q.Name)+"`")
					break
				}
			}
		}
	}

	for _, svc := range services {
		for _, ep := range svc.Endpoints {
			if len(ep.Queries) == 0 {
				continue
			}
			runtime := make(map[string]struct{})
			for _, q := range ep.Queries {
				q.Source = keySource(ep, q)
				q.Decls = queryDecls(ep, q, runtime)
			}
			for name := range runtime {
				ep.QueryImports = append(ep.QueryImports, name)
			}
			sort.Strings(ep.QueryImports)
		}
	}
}

// discriminatorMark names the property a discriminated key carries.
func discriminatorMark(q *QuerySpec) string {
	if q.Discriminator == "stream" {
		return "$stream"
	}
	return "$method"
}

// discriminatorEntry renders the discriminator as it sits in the key, for
// the state card.
func discriminatorEntry(q *QuerySpec) string {
	if q.Discriminator == "stream" {
		return "$stream: true"
	}
	return `$method: "` + q.Verb + `"`
}

// invalidates reports whether the key prefix of a template matches the keys
// of a route: every segment of the template is equal to the route's, a
// parameter matching a parameter.
func invalidates(template, path string) bool {
	t, p := segments(template), segments(path)
	if len(t) > len(p) {
		return false
	}
	for i := range t {
		if isParam(t[i]) != isParam(p[i]) || (!isParam(t[i]) && t[i] != p[i]) {
			return false
		}
	}
	return true
}

// keySource renders the shape of a function's key for its state card, e.g.
// `["things", id, { tags?, verbose? }]`.
func keySource(ep *EndpointSpec, q *QuerySpec) string {
	req := ep.Request
	paramProp := make(map[string]string)
	if req != nil {
		for _, p := range req.Path {
			paramProp[p.Wire] = p.Prop
		}
	}
	var parts []string
	for _, s := range segments(q.Path) {
		if m := pathParam.FindStringSubmatch(s); m != nil && m[0] == s {
			name := paramProp[m[1]]
			if name == "" {
				name = "payload"
			}
			parts = append(parts, name)
			continue
		}
		parts = append(parts, `"`+s+`"`)
	}
	var inputs []string
	if q.Discriminator != "" {
		inputs = append(inputs, discriminatorEntry(q))
	}
	if req != nil {
		if req.Scalar {
			if len(req.Path) == 0 {
				if q.Discriminator != "" {
					inputs = append(inputs, "$value: payload")
				} else {
					parts = append(parts, "payload")
				}
			}
		} else {
			optional := make(map[string]bool)
			for _, p := range req.Props {
				optional[p.Key] = p.Optional
			}
			seen := make(map[string]struct{})
			put := func(prop string) {
				if _, dup := seen[prop]; dup || prop == "" {
					return
				}
				seen[prop] = struct{}{}
				if optional[prop] {
					prop += "?"
				}
				inputs = append(inputs, prop)
			}
			for _, p := range req.Query {
				put(p.Prop)
			}
			if req.MapQuery != nil {
				if req.MapQuery.Prop == "" {
					inputs = append(inputs, "...payload")
				} else {
					put(req.MapQuery.Prop)
				}
			}
			for _, p := range req.Headers {
				put(p.Prop)
			}
			switch req.Body.Kind {
			case BodyFields:
				for _, f := range req.Body.Fields {
					put(f)
				}
			case BodyAttr:
				put(req.Body.Attr)
			}
		}
	}
	if len(inputs) > 0 {
		parts = append(parts, "{ "+strings.Join(inputs, ", ")+" }")
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// templateKey renders the key a path template invalidates, e.g.
// `["things", id, "labels"]`.
func templateKey(ep *EndpointSpec, template string) string {
	paramProp := make(map[string]string)
	if ep.Request != nil {
		for _, p := range ep.Request.Path {
			paramProp[p.Wire] = p.Prop
		}
	}
	var parts []string
	for _, s := range segments(template) {
		if m := pathParam.FindStringSubmatch(s); m != nil && m[0] == s {
			name := paramProp[m[1]]
			if name == "" {
				name = m[1]
			}
			parts = append(parts, name)
			continue
		}
		parts = append(parts, `"`+s+`"`)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// queryDecls renders the exports of one function: the JSDoc state card and
// one line of code each.
func queryDecls(ep *EndpointSpec, q *QuerySpec, runtime map[string]struct{}) []QueryDecl {
	schema := q.RequestName
	if schema == "" {
		schema = "null"
	}
	var flags []string
	if q.Required {
		flags = append(flags, "required: true")
	}
	if q.Discriminator != "" {
		flags = append(flags, "discriminator: "+tsString(q.Discriminator))
	}
	literal := func(fields []string) string {
		if len(fields) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(fields, ", ") + " }"
	}

	var card strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&card, "\n"+format, args...) }
	switch q.Kind {
	case QueryRead, QueryStream:
		line("- Owner: server")
		line("- Source: `%s`", q.Source)
		line("- Writable: no")
		by := "none"
		if len(q.InvalidatedBy) > 0 {
			by = strings.Join(q.InvalidatedBy, ", ")
		}
		line("- Mutations & invalidation: %s", by)
		if len(q.Collisions) > 0 {
			if q.Discriminator != "" {
				line("- Collides with %s: the key carries `%s`", strings.Join(q.Collisions, ", "), discriminatorEntry(q))
			} else {
				line("- Shares its path with %s, told apart by %s", strings.Join(q.Collisions, ", "),
					strings.Join(q.OtherMarks, " and "))
			}
		}
	case QueryMutation:
		line("- Owner: server")
		var keys []string
		for _, t := range q.Invalidates {
			keys = append(keys, "`"+templateKey(ep, t)+"`")
		}
		if len(keys) == 0 {
			keys = []string{"nothing"}
		}
		line("- Invalidates: %s", strings.Join(keys, ", "))
	}
	stateCard := card.String()

	var decls []QueryDecl
	switch q.Kind {
	case QueryRead:
		runtime["queryFor"], runtime["keyFor"] = struct{}{}, struct{}{}
		doc := fmt.Sprintf("%s returns the query options of %s, for `useQuery`, `fetchQuery` and the like.", q.Name, q.FuncName)
		if q.Required {
			doc += "\nA payload of `undefined` returns a disabled query (`enabled: false`)."
		}
		decls = append(decls,
			QueryDecl{
				Doc:  doc + "\n" + stateCard,
				Code: fmt.Sprintf("export const %s = queryFor(%s, %s, %s, %s);", q.Name, q.FuncName, q.SpecName, schema, literal(flags)),
			},
			keyDecl(q, schema, stateCard))
	case QueryStream:
		runtime["streamFor"], runtime["keyFor"] = struct{}{}, struct{}{}
		doc := fmt.Sprintf("%s returns the streamed query options of %s. Pass TanStack's\n", q.Name, q.FuncName) +
			"`experimental_streamedQuery` as `streamedQuery`; the data accumulates the events, or\n" +
			"folds them with `reducer` and `initialValue`. `staleTime` is `Infinity`."
		if q.Required {
			doc += "\nA payload of `undefined` returns a disabled query (`enabled: false`)."
		}
		decls = append(decls,
			QueryDecl{
				Doc:  doc + "\n" + stateCard,
				Code: fmt.Sprintf("export const %s = streamFor(%s, %s, %s, %s);", q.Name, q.FuncName, q.SpecName, schema, literal(flags)),
			},
			keyDecl(q, schema, stateCard))
	case QueryMutation:
		runtime["invalidatesFor"], runtime["mutationFor"] = struct{}{}, struct{}{}
		templates := make([]string, len(q.Invalidates))
		for i, t := range q.Invalidates {
			templates[i] = tsString(t)
		}
		raw := ""
		if q.Raw {
			raw = ", { raw: true }"
		}
		vars := ""
		if q.Raw {
			vars = "\nThe variables are `{ payload, body }`."
		}
		decls = append(decls,
			QueryDecl{
				Doc: fmt.Sprintf("%s returns the keys %s invalidates, from its variables, for wiring\n", q.InvalidatesName, q.FuncName) +
					"invalidation by hand.\n" + stateCard,
				Code: fmt.Sprintf("export const %s = invalidatesFor(%s, %s, [%s]%s);",
					q.InvalidatesName, q.SpecName, schema, strings.Join(templates, ", "), raw),
			},
			QueryDecl{
				Doc: fmt.Sprintf("%s returns the mutation options of %s, for `useMutation`. Given\n", q.Name, q.FuncName) +
					"`{ queryClient }`, `onSuccess` invalidates the keys below." + vars + "\n" + stateCard,
				Code: fmt.Sprintf("export const %s = mutationFor(%s, %s%s);", q.Name, q.FuncName, q.InvalidatesName, raw),
			})
	}
	return decls
}

// keyDecl renders xyzKey.
func keyDecl(q *QuerySpec, schema, stateCard string) QueryDecl {
	args := q.SpecName + ", " + schema
	if q.Discriminator != "" {
		args += ", { discriminator: " + tsString(q.Discriminator) + " }"
	}
	return QueryDecl{
		Doc: fmt.Sprintf("%s returns the key of a %s request, or without a payload the prefix\n", q.KeyName, q.FuncName) +
			fmt.Sprintf("`%s` that every one of them starts with.\n", templateKey(&EndpointSpec{}, staticPrefix(q.Path))) + stateCard,
		Code: fmt.Sprintf("export const %s = keyFor(%s);", q.KeyName, args),
	}
}
