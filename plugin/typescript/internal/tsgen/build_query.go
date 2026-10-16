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
			// TanStack Query treats undefined data as an error: a read of a
			// method without a result resolves to null.
			q.Void = kind == QueryRead && (ep.Result == nil || ep.Result.TypeName == "")
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
// the service has its name, and resolves the invalidation its mutations
// declare with Meta("typescript:invalidates"). The default invalidation needs
// every read of every service: finishQueries computes it.
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
				q.defaultInvalidation = true
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
					if prefix == "/" {
						return fmt.Errorf("method %q: Meta(%q) cannot invalidate %q: its route has no static prefix",
							ep.MethodName, metaInvalidates, name)
					}
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

// defaultInvalidation is the path whose key a mutation invalidates by
// default: its own route with trailing parameters stripped, then shortened
// one segment at a time until it is a prefix of the route of at least one of
// reads, and never below one segment. ok is false when no such prefix
// reaches a read: the path is then the shortest one tried, and "" when the
// route has no segment at all.
func defaultInvalidation(path string, reads []string) (template string, ok bool) {
	segs := segments(path)
	for len(segs) > 1 && isParam(segs[len(segs)-1]) {
		segs = segs[:len(segs)-1]
	}
	if len(segs) == 0 {
		return "", false
	}
	for {
		template = "/" + strings.Join(segs, "/")
		for _, r := range reads {
			if invalidates(template, r) {
				return template, true
			}
		}
		if len(segs) == 1 {
			return template, false
		}
		segs = segs[:len(segs)-1]
	}
}

// staticPrefix is the path made of the static segments of a route up to its
// first parameter: what keyPrefixFor returns. It is "/" when the route
// starts with a parameter.
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

// finishQueries runs once every service is built: it resolves the default
// invalidation of mutations, detects key collisions across services, finds
// which mutations invalidate which keys, and renders the query files'
// declarations. It returns warnings for `goa gen` to print.
func finishQueries(services []*ServiceSpec) []string {
	var (
		reads, writes []queryRef
		readPaths     []string
		warnings      []string
	)
	for _, svc := range services {
		for _, ep := range svc.Endpoints {
			for _, q := range ep.Queries {
				switch q.Kind {
				case QueryMutation:
					writes = append(writes, queryRef{svc, ep, q})
				case QueryRead:
					readPaths = append(readPaths, q.Path)
					fallthrough
				default:
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

	// Default invalidation: the nearest ancestor of the mutation's route
	// that some read lives under. Streams do not count: mutations never
	// invalidate them.
	for _, w := range writes {
		if !w.q.defaultInvalidation {
			continue
		}
		template, ok := defaultInvalidation(w.q.Path, readPaths)
		if template != "" {
			w.q.Invalidates = []string{template}
		}
		if !ok {
			w.q.InvalidatesNoQuery = true
			warnings = append(warnings, fmt.Sprintf("service %q: mutation %q invalidates no query", w.svc.Name, w.ep.MethodName))
		}
	}

	// Collisions: reads with reads and streams with streams, on routes that
	// are equal once parameter names are ignored. A GET keeps its clean key;
	// every other member is told apart by its verb, so when no member is a
	// GET all of them are.
	groups := make(map[string][]queryRef)
	var order []string
	for _, r := range reads {
		k := string(r.q.Kind) + " " + normalizedPath(r.q.Path)
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
		for _, r := range group {
			if r.q.Verb != "GET" {
				r.q.Discriminator = "method"
			}
			for _, o := range group {
				if o.q != r.q {
					r.q.Collisions = append(r.q.Collisions,
						fmt.Sprintf("`%s %s` (`%s`)", o.q.Verb, o.q.Path, ref(r.svc, o, o.q.Name)))
				}
			}
		}
	}

	// Invalidation: a mutation's template invalidates every read key it is a
	// prefix of.
	for _, r := range reads {
		if r.q.Kind != QueryRead {
			continue
		}
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
	return warnings
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

// pathKeyParts renders the segments of a key for a state card: static
// segments quoted, a parameter by the request property holding it, or, when
// none does, by missing (or by the parameter's own name when missing is "").
func pathKeyParts(ep *EndpointSpec, path, missing string) []string {
	paramProp := make(map[string]string)
	if ep.Request != nil {
		for _, p := range ep.Request.Path {
			paramProp[p.Wire] = p.Prop
		}
	}
	var parts []string
	for _, s := range segments(path) {
		if m := pathParam.FindStringSubmatch(s); m != nil && m[0] == s {
			name := paramProp[m[1]]
			switch {
			case name != "":
			case missing != "":
				name = missing
			default:
				name = m[1]
			}
			parts = append(parts, name)
			continue
		}
		parts = append(parts, `"`+s+`"`)
	}
	return parts
}

// keySource renders the shape of a function's key for its state card, e.g.
// `["things", id, { tags?, verbose? }]`. It follows the key grammar of ADR
// 0006, which runtime/src/query.ts implements (queryKeyFor): the two must
// agree.
func keySource(ep *EndpointSpec, q *QuerySpec) string {
	req := ep.Request
	parts := pathKeyParts(ep, q.Path, "payload")
	var inputs []string
	if q.Kind == QueryStream {
		inputs = append(inputs, "$stream: true")
	}
	if q.Discriminator != "" {
		inputs = append(inputs, discriminatorEntry(q))
	}
	if req != nil {
		if req.Scalar {
			if len(req.Path) == 0 {
				if len(inputs) > 0 {
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

// discriminatorEntry renders the discriminator as it sits in the key.
func discriminatorEntry(q *QuerySpec) string {
	return `$method: "` + q.Verb + `"`
}

// templateKey renders the key a path template invalidates, e.g.
// `["things", id, "labels"]`.
func templateKey(ep *EndpointSpec, template string) string {
	return "[" + strings.Join(pathKeyParts(ep, template, ""), ", ") + "]"
}

// keyFlags renders the flags a key takes: the stream marker and the
// discriminator.
func keyFlags(q *QuerySpec) []string {
	var flags []string
	if q.Kind == QueryStream {
		flags = append(flags, "stream: true")
	}
	if q.Discriminator != "" {
		flags = append(flags, "discriminator: "+tsString(q.Discriminator))
	}
	return flags
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
		// A stream is marked by streamFor itself.
		flags = append(flags, "discriminator: "+tsString(q.Discriminator))
	}
	if q.Void {
		flags = append(flags, "void: true")
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
		switch {
		case q.Kind == QueryStream:
			by = "none (mutations never invalidate streams)"
		case len(q.InvalidatedBy) > 0:
			by = strings.Join(q.InvalidatedBy, ", ")
		}
		line("- Mutations & invalidation: %s", by)
		if len(q.Collisions) > 0 {
			if q.Discriminator != "" {
				line("- Collides with %s: the key carries `%s`", strings.Join(q.Collisions, ", "), discriminatorEntry(q))
			} else {
				line("- Shares its path with %s, told apart by `$method`", strings.Join(q.Collisions, ", "))
			}
		}
	case QueryMutation:
		line("- Owner: server")
		var keys []string
		for _, t := range q.Invalidates {
			keys = append(keys, "`"+templateKey(ep, t)+"`")
		}
		invalidated := strings.Join(keys, ", ")
		switch {
		case len(keys) == 0:
			invalidated = "nothing"
		case q.InvalidatesNoQuery:
			invalidated += " (no query lives under it)"
		}
		line("- Invalidates: %s", invalidated)
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
		if q.Void {
			doc += "\nThe method has no result: the data is `null`."
		}
		decls = append(decls,
			QueryDecl{
				Doc:  doc + "\n" + stateCard,
				Code: fmt.Sprintf("export const %s = queryFor(%s, %s, %s, %s);", q.Name, q.FuncName, q.SpecName, schema, literal(flags)),
			},
			keyDecl(q, schema, stateCard, literal))
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
			keyDecl(q, schema, stateCard, literal))
	case QueryMutation:
		runtime["invalidatesFor"], runtime["mutationFor"] = struct{}{}, struct{}{}
		templates := make([]string, len(q.Invalidates))
		for i, t := range q.Invalidates {
			templates[i] = tsString(t)
		}
		raw, vars := "", ""
		if q.Raw {
			raw, vars = ", { raw: true }", "\nThe variables are `{ payload, body }`."
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
					"`{ queryClient }`, `onSuccess` invalidates the keys below (never a stream's)." + vars + "\n" + stateCard,
				Code: fmt.Sprintf("export const %s = mutationFor(%s, %s%s);", q.Name, q.FuncName, q.InvalidatesName, raw),
			})
	}
	return decls
}

// prefixKeyText renders what xyzKey() returns without a payload, as
// keyFor does: the static prefix, the route with null parameters when it
// starts with one, and for a route without parameters the key's markers.
func prefixKeyText(q *QuerySpec) string {
	if staticPrefix(q.Path) == "/" {
		return "[" + strings.Join(pathKeyParts(&EndpointSpec{}, q.Path, "null"), ", ") + "]"
	}
	parts := pathKeyParts(&EndpointSpec{}, staticPrefix(q.Path), "")
	if !strings.Contains(q.Path, "{") {
		var marks []string
		if q.Kind == QueryStream {
			marks = append(marks, "$stream: true")
		}
		if q.Discriminator != "" {
			marks = append(marks, discriminatorEntry(q))
		}
		if len(marks) > 0 {
			parts = append(parts, "{ "+strings.Join(marks, ", ")+" }")
		}
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// keyDecl renders xyzKey.
func keyDecl(q *QuerySpec, schema, stateCard string, literal func([]string) string) QueryDecl {
	args := q.SpecName + ", " + schema
	if flags := keyFlags(q); len(flags) > 0 {
		args += ", " + literal(flags)
	}
	var doc string
	if strings.Contains(q.Path, "{") {
		doc = fmt.Sprintf("%s returns the key of a %s request. Without a payload it returns\n", q.KeyName, q.FuncName) +
			fmt.Sprintf("`%s`, an invalidation prefix that other methods under the same path share:\n", prefixKeyText(q)) +
			"it is not a cache key."
	} else {
		doc = fmt.Sprintf("%s returns the key of a %s request. Without a payload it returns\n", q.KeyName, q.FuncName) +
			fmt.Sprintf("`%s`, an invalidation prefix that matches every %s request and is no other\n", prefixKeyText(q), q.FuncName) +
			"query's key."
	}
	return QueryDecl{
		Doc:  doc + "\n" + stateCard,
		Code: fmt.Sprintf("export const %s = keyFor(%s);", q.KeyName, args),
	}
}
