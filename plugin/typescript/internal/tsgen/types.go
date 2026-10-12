package tsgen

import (
	"mime"
	"sort"
	"strings"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/expr"
)

// typeMapper turns design types into TypeScript types and Zod schemas. Named
// user types are hoisted into src/types.ts and referenced by name; everything
// else is rendered inline.
type typeMapper struct {
	// names maps the origin of each hoisted user type to its identifier.
	names map[expr.UserType]string
	// origins lists the hoisted user types in discovery order.
	origins []expr.UserType
	// recursive holds the hoisted types that take part in a reference cycle.
	recursive map[expr.UserType]bool
	// authored holds the user types the design declares with Type or
	// ResultType. Only those are hoisted.
	authored map[expr.UserType]struct{}
}

// newTypeMapper returns a mapper that hoists the types declared by the roots.
func newTypeMapper(roots ...*expr.RootExpr) *typeMapper {
	m := &typeMapper{
		names:     make(map[expr.UserType]string),
		recursive: make(map[expr.UserType]bool),
		authored:  make(map[expr.UserType]struct{}),
	}
	for _, root := range roots {
		if root == nil {
			continue
		}
		for _, ut := range root.Types {
			m.authored[ut.Origin()] = struct{}{}
		}
		for _, ut := range root.ResultTypes {
			m.authored[ut.Origin()] = struct{}{}
		}
	}
	return m
}

// refs records what a rendered snippet needs imported.
type refs struct {
	zod     bool
	runtime map[string]struct{}
	types   map[string]struct{}
}

func newRefs() *refs {
	return &refs{runtime: make(map[string]struct{}), types: make(map[string]struct{})}
}

func (r *refs) useRuntime(names ...string) {
	for _, n := range names {
		r.runtime[n] = struct{}{}
	}
}

// imports returns the import lists. Runtime names are sorted ignoring their
// "type " prefix.
func (r *refs) imports() Imports {
	imp := Imports{Zod: r.zod}
	for n := range r.runtime {
		imp.Runtime = append(imp.Runtime, n)
	}
	sort.Slice(imp.Runtime, func(i, j int) bool {
		a, b := strings.TrimPrefix(imp.Runtime[i], "type "), strings.TrimPrefix(imp.Runtime[j], "type ")
		if strings.EqualFold(a, b) {
			return a < b
		}
		return strings.ToLower(a) < strings.ToLower(b)
	})
	for n := range r.types {
		imp.Types = append(imp.Types, n)
	}
	sort.Strings(imp.Types)
	return imp
}

// hoist reports whether t is a user type that gets its own declaration, and
// returns the declaration it stands for.
//
// Only types the design names qualify. goa also creates user types of its
// own: wrappers around inline method payloads and results (during `goa gen`,
// before plugins run) and around primitive union branches. Their names are
// artifacts of the Go generator, so they are rendered inline like the
// anonymous types they stand for. So is the result type CollectionOf(T)
// creates: goa registers it with the design's result types under the invented
// name "TCollection", but it is no more than T[]. The empty type and goa's
// default error type are modelled by the runtime.
func (m *typeMapper) hoist(t expr.DataType) (expr.UserType, bool) {
	ut, ok := t.(expr.UserType)
	if !ok || ut == expr.Empty || expr.IsErrorResult(ut) || isCollection(ut) {
		return nil, false
	}
	origin := ut.Origin()
	if _, authored := m.authored[origin]; !authored {
		return nil, false
	}
	return origin, true
}

// isCollection reports whether ut is the result type CollectionOf creates: an
// array whose identifier carries the "type=collection" parameter. goa has no
// other marker for it.
func isCollection(ut expr.UserType) bool {
	rt, ok := ut.(*expr.ResultTypeExpr)
	if !ok {
		return false
	}
	if _, ok := rt.Attribute().Type.(*expr.Array); !ok {
		return false
	}
	_, params, err := mime.ParseMediaType(rt.Identifier)
	return err == nil && params["type"] == "collection"
}

// goaTypeName returns the name of a user type in the design, ignoring the
// Go-only "struct:type:name" override.
func goaTypeName(ut expr.UserType) string {
	switch t := ut.(type) {
	case *expr.UserTypeExpr:
		return t.TypeName
	case *expr.ResultTypeExpr:
		return t.TypeName
	default:
		return ut.Name()
	}
}

// collect registers every user type reachable from att.
func (m *typeMapper) collect(att *expr.AttributeExpr) {
	if att == nil || att.Type == nil {
		return
	}
	_ = codegen.Walk(att, func(a *expr.AttributeExpr) error {
		if origin, ok := m.hoist(a.Type); ok {
			if _, seen := m.names[origin]; !seen {
				m.names[origin] = ""
				m.origins = append(m.origins, origin)
			}
		}
		return nil
	})
}

// assignNames gives every collected type its identifier. Types are visited in
// name order so that a collision resolves the same way on every run.
func (m *typeMapper) assignNames(scope *codegen.NameScope) {
	sorted := append([]expr.UserType(nil), m.origins...)
	sort.SliceStable(sorted, func(i, j int) bool { return goaTypeName(sorted[i]) < goaTypeName(sorted[j]) })
	for _, ut := range sorted {
		name := goaTypeName(ut)
		if override, ok := ut.Attribute().Meta.Last("typescript:name"); ok && override != "" {
			name = override
		}
		m.names[ut] = scope.Unique(tsIdent(name))
	}
}

// directRefs returns the hoisted types that att references without going
// through another hoisted type.
func (m *typeMapper) directRefs(att *expr.AttributeExpr) []expr.UserType {
	var (
		out    []expr.UserType
		seen   = make(map[expr.UserType]struct{})
		inline = make(map[expr.UserType]struct{})
		walk   func(a *expr.AttributeExpr)
	)
	walk = func(a *expr.AttributeExpr) {
		if a == nil || a.Type == nil {
			return
		}
		if origin, ok := m.hoist(a.Type); ok {
			if _, dup := seen[origin]; !dup {
				seen[origin] = struct{}{}
				out = append(out, origin)
			}
			return
		}
		switch t := a.Type.(type) {
		case expr.UserType:
			// A type goa created: look through it, once.
			if _, dup := inline[t]; !dup {
				inline[t] = struct{}{}
				walk(t.Attribute())
			}
		case *expr.Array:
			walk(t.ElemType)
		case *expr.Map:
			walk(t.ElemType)
		case *expr.Union:
			for _, nat := range t.Values {
				walk(nat.Attribute)
			}
		case *expr.Object:
			for _, nat := range *t {
				walk(nat.Attribute)
			}
		}
	}
	walk(att)
	return out
}

// order returns the hoisted types so that every type follows the types its
// schema refers to: strongly connected components in topological order, ties
// broken by name. Members of a cycle are flagged recursive.
func (m *typeMapper) order() []expr.UserType {
	nodes := append([]expr.UserType(nil), m.origins...)
	sort.SliceStable(nodes, func(i, j int) bool { return m.names[nodes[i]] < m.names[nodes[j]] })
	edges := make(map[expr.UserType][]expr.UserType, len(nodes))
	for _, n := range nodes {
		edges[n] = m.directRefs(n.Attribute())
	}

	// Tarjan's strongly connected components.
	var (
		index   = make(map[expr.UserType]int)
		low     = make(map[expr.UserType]int)
		onStack = make(map[expr.UserType]bool)
		stack   []expr.UserType
		next    int
		comps   [][]expr.UserType
		comp    = make(map[expr.UserType]int)
		visit   func(n expr.UserType)
	)
	visit = func(n expr.UserType) {
		index[n], low[n] = next, next
		next++
		stack = append(stack, n)
		onStack[n] = true
		for _, d := range edges[n] {
			if _, seen := index[d]; !seen {
				visit(d)
				low[n] = min(low[n], low[d])
			} else if onStack[d] {
				low[n] = min(low[n], index[d])
			}
		}
		if low[n] != index[n] {
			return
		}
		var members []expr.UserType
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			comp[top] = len(comps)
			members = append(members, top)
			if top == n {
				break
			}
		}
		sort.Slice(members, func(i, j int) bool { return m.names[members[i]] < m.names[members[j]] })
		comps = append(comps, members)
	}
	for _, n := range nodes {
		if _, seen := index[n]; !seen {
			visit(n)
		}
	}

	for _, members := range comps {
		cyclic := len(members) > 1
		for _, d := range edges[members[0]] {
			cyclic = cyclic || d == members[0]
		}
		for _, n := range members {
			m.recursive[n] = cyclic
		}
	}

	// Kahn's algorithm over the condensation, smallest name first.
	deps := make([]map[int]struct{}, len(comps))
	users := make([][]int, len(comps))
	for i, members := range comps {
		deps[i] = make(map[int]struct{})
		for _, n := range members {
			for _, d := range edges[n] {
				if c := comp[d]; c != i {
					if _, dup := deps[i][c]; !dup {
						deps[i][c] = struct{}{}
						users[c] = append(users[c], i)
					}
				}
			}
		}
	}
	var ready []int
	for i := range comps {
		if len(deps[i]) == 0 {
			ready = append(ready, i)
		}
	}
	var out []expr.UserType
	for len(ready) > 0 {
		sort.Slice(ready, func(i, j int) bool { return m.names[comps[ready[i]][0]] < m.names[comps[ready[j]][0]] })
		c := ready[0]
		ready = ready[1:]
		out = append(out, comps[c]...)
		for _, u := range users[c] {
			delete(deps[u], c)
			if len(deps[u]) == 0 {
				ready = append(ready, u)
			}
		}
	}
	return out
}

// typeSpec renders the declaration of one hoisted type.
func (m *typeMapper) typeSpec(ut expr.UserType, r *refs) *TypeSpec {
	att := ut.Attribute()
	spec := &TypeSpec{
		GoaName:     goaTypeName(ut),
		Name:        m.names[ut],
		Description: att.Description,
		Kind:        TypeAlias,
		Recursive:   m.recursive[ut],
	}
	c := &renderCtx{m: m, refs: r}
	zodIndent := ""
	if spec.Recursive {
		zodIndent = "  "
	}
	if obj, ok := att.Type.(*expr.Object); ok {
		spec.Kind = TypeObject
		opts := objectOpts{}
		if rt, ok := ut.(*expr.ResultTypeExpr); ok {
			opts.views = rt
		}
		fields := c.fields(att, obj, opts)
		spec.TSDef = c.objectTS(fields, "")
		spec.Zod, _ = c.objectZod(fields, zodIndent)
		for _, f := range fields {
			spec.Props = append(spec.Props, PropSpec{
				Attr:        f.name,
				Key:         f.key,
				TSType:      c.tsType(f.att, "  ", f.param),
				Description: f.att.Description,
				Optional:    f.optional,
			})
		}
		return spec
	}
	// A named primitive, array, map or reference to another type.
	inner := &expr.AttributeExpr{Type: att.Type, Validation: att.Validation, Meta: att.Meta}
	spec.TSDef = c.tsType(inner, "", false)
	z := c.zod(inner, zodIndent, false)
	spec.Zod, spec.ZodNote = z.code, z.note()
	return spec
}

// renderCtx carries the state shared by the TypeScript and Zod renderers.
type renderCtx struct {
	m    *typeMapper
	refs *refs
	// inlining holds the unnamed user types being rendered, to stop on a
	// reference cycle that no named type breaks.
	inlining map[expr.UserType]struct{}
}

// enter marks an unnamed user type as being rendered. It reports false when
// the type is already being rendered further up.
func (c *renderCtx) enter(ut expr.UserType) bool {
	if _, busy := c.inlining[ut]; busy {
		return false
	}
	if c.inlining == nil {
		c.inlining = make(map[expr.UserType]struct{})
	}
	c.inlining[ut] = struct{}{}
	return true
}

func (c *renderCtx) leave(ut expr.UserType) {
	delete(c.inlining, ut)
}

// objectOpts tunes how the attributes of an object are rendered.
type objectOpts struct {
	// views makes attributes missing from any view of the result type optional.
	views *expr.ResultTypeExpr
	// request renders the top level of a request: attributes with a default
	// become optional because the server fills them in.
	request bool
	// skip lists attributes to leave out.
	skip map[string]struct{}
	// params lists attributes sent as HTTP parameters or headers. They keep
	// their attribute name and use the parameter encoding.
	params map[string]struct{}
	// plainKey lists further attributes that keep their attribute name
	// because they are not sent as a member of a JSON object.
	plainKey map[string]struct{}
}

// field is one resolved attribute of an object.
type field struct {
	// name is the design attribute name.
	name string
	// key is the TypeScript property name.
	key string
	att *expr.AttributeExpr
	// optional is true when the property may be absent.
	optional bool
	// nullWhenUnset is true when goa sends null for an absent value.
	nullWhenUnset bool
	// def is the default applied when decoding, "" if none.
	def string
	// serverDefault is the default the server applies to an omitted request
	// attribute, "" if none.
	serverDefault string
	// param is true when the attribute travels as an HTTP parameter.
	param bool
}

// fields resolves the attributes of obj, which belongs to parent.
func (c *renderCtx) fields(parent *expr.AttributeExpr, obj *expr.Object, opts objectOpts) []field {
	var out []field
	for _, nat := range *obj {
		if _, skip := opts.skip[nat.Name]; skip {
			continue
		}
		_, param := opts.params[nat.Name]
		wk := jsonKey(nat.Name, nat.Attribute)
		if _, plain := opts.plainKey[nat.Name]; param || plain {
			wk = wireKey{Key: nat.Name}
		}
		if wk.Omit {
			continue
		}
		f := field{name: nat.Name, key: wk.Key, att: nat.Attribute, param: param}
		required := parent.IsRequired(nat.Name)
		def, hasDefault := defaultLiteral(nat.Attribute)
		switch {
		case opts.request:
			f.optional = !required || nat.Attribute.DefaultValue != nil
			if hasDefault {
				f.serverDefault = def
			}
		case opts.views != nil && !inEveryView(opts.views, nat.Name):
			f.optional = true
		case hasDefault:
			f.def = def
		default:
			f.optional = !required
		}
		f.nullWhenUnset = f.optional && wk.NullWhenUnset && !opts.request
		out = append(out, f)
	}
	return out
}

// valueFields resolves the attributes of an object value the way its type and
// schema are rendered: a named type with its views, an unnamed one as is. It
// returns nil when the value is not an object.
func (c *renderCtx) valueFields(att *expr.AttributeExpr) []field {
	obj := expr.AsObject(att.Type)
	if obj == nil {
		return nil
	}
	parent, opts := att, objectOpts{}
	if ut, ok := att.Type.(expr.UserType); ok {
		parent = ut.Attribute()
		if rt, ok := ut.(*expr.ResultTypeExpr); ok {
			opts.views = rt
		}
	}
	return c.fields(parent, obj, opts)
}

// inEveryView reports whether every view of rt includes the attribute.
func inEveryView(rt *expr.ResultTypeExpr, name string) bool {
	for _, v := range rt.Views {
		if !rt.ViewHasAttribute(v.Name, name) {
			return false
		}
	}
	return true
}

// objectTS renders an object type literal. indent is the indentation of the
// line the literal starts on.
func (c *renderCtx) objectTS(fields []field, indent string) string {
	if len(fields) == 0 {
		return "Record<string, never>"
	}
	var b strings.Builder
	b.WriteString("{\n")
	inner := indent + "  "
	for _, f := range fields {
		doc := f.att.Description
		if f.serverDefault != "" {
			doc = strings.TrimSpace(doc + "\n\nDefault: " + f.serverDefault + " (applied by the server when omitted).")
		}
		b.WriteString(jsdoc(doc, inner))
		b.WriteString(inner + tsPropKey(f.key))
		if f.optional {
			b.WriteString("?:")
		} else {
			b.WriteString(":")
		}
		if u, ok := f.att.Type.(*expr.Union); ok && len(u.Values) > 1 {
			// One member per line, each led by a bar.
			for _, member := range c.unionMembers(u, inner+"    ") {
				b.WriteString("\n" + inner + "  | " + member)
			}
			if f.optional {
				b.WriteString("\n" + inner + "  | undefined")
			}
			b.WriteString(";\n")
			continue
		}
		b.WriteString(" " + c.tsType(f.att, inner, f.param))
		if f.optional {
			b.WriteString(" | undefined")
		}
		b.WriteString(";\n")
	}
	b.WriteString(indent + "}")
	return b.String()
}

// tsType renders the TypeScript type of an attribute. param selects the
// encoding of HTTP parameters, where goa sends bytes and untyped values as
// plain strings.
func (c *renderCtx) tsType(att *expr.AttributeExpr, indent string, param bool) string {
	if origin, ok := c.m.hoist(att.Type); ok {
		name := c.m.names[origin]
		c.refs.types[name] = struct{}{}
		return name
	}
	switch t := att.Type.(type) {
	case expr.UserType:
		// A type the design does not name: render what it stands for.
		if !c.enter(t) {
			return "unknown"
		}
		defer c.leave(t)
		return c.tsType(t.Attribute(), indent, param)
	case expr.Primitive:
		if lits, ok := enumLiterals(att); ok {
			return strings.Join(lits, " | ")
		}
		switch t.Kind() {
		case expr.BooleanKind:
			return "boolean"
		case expr.IntKind, expr.Int32Kind, expr.Int64Kind, expr.UIntKind, expr.UInt32Kind, expr.UInt64Kind,
			expr.Float32Kind, expr.Float64Kind:
			return "number"
		case expr.StringKind:
			if formatOf(att) == expr.FormatDateTime {
				return "Date"
			}
			return "string"
		case expr.BytesKind:
			if param {
				return "string"
			}
			return "Uint8Array"
		default: // expr.AnyKind
			if param {
				return "string"
			}
			return "unknown"
		}
	case *expr.Array:
		elem := c.tsType(t.ElemType, indent, param)
		if strings.Contains(elem, " | ") || strings.Contains(elem, "=>") {
			elem = "(" + elem + ")"
		}
		return elem + "[]"
	case *expr.Map:
		return "Record<string, " + c.tsType(t.ElemType, indent, param) + ">"
	case *expr.Object:
		return c.objectTS(c.fields(att, t, objectOpts{}), indent)
	case *expr.Union:
		if len(t.Values) == 0 {
			return "never"
		}
		return strings.Join(c.unionMembers(t, indent), " | ")
	}
	return "unknown"
}

// unionMembers renders the branches of a union as goa serializes them: an
// envelope holding the branch name and its value.
func (c *renderCtx) unionMembers(u *expr.Union, indent string) []string {
	typeKey, valueKey := tsPropKey(u.GetTypeKey()), tsPropKey(u.GetValueKey())
	members := make([]string, len(u.Values))
	for i, nat := range u.Values {
		members[i] = "{ " + typeKey + ": " + tsString(nat.Name) + "; " + valueKey + ": " +
			c.tsType(nat.Attribute, indent, false) + " }"
	}
	return members
}

// formatOf returns the string format of an attribute, if any.
func formatOf(att *expr.AttributeExpr) expr.ValidationFormat {
	if att.Validation == nil {
		return ""
	}
	return att.Validation.Format
}
