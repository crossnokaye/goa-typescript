package tsgen

import (
	"fmt"
	"mime"
	"sort"
	"strings"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/expr"
)

// decl identifies one hoisted declaration of src/types.ts: a user type the
// design names, or an attribute the design names with
// Meta("typescript:name"). Exactly one of the fields is set.
type decl struct {
	ut  expr.UserType
	att *expr.AttributeExpr
}

// attribute returns the attribute the declaration renders.
func (d decl) attribute() *expr.AttributeExpr {
	if d.ut != nil {
		return d.ut.Attribute()
	}
	return d.att
}

// typeMapper turns design types into TypeScript types and Zod schemas. Named
// user types, and attributes named with Meta("typescript:name"), are hoisted
// into src/types.ts and referenced by name; everything else is rendered
// inline.
type typeMapper struct {
	// names maps each hoisted declaration to its identifier.
	names map[decl]string
	// origins lists the hoisted declarations in discovery order.
	origins []decl
	// recursive holds the hoisted declarations that take part in a reference
	// cycle.
	recursive map[decl]bool
	// authored holds the user types the design declares with Type or
	// ResultType. Only those are hoisted.
	authored map[expr.UserType]struct{}
	// attrs maps the attributes named with Meta("typescript:name") to their
	// declaration.
	attrs map[*expr.AttributeExpr]decl
	// named holds every attribute whose Meta("typescript:name") has been
	// considered, hoisted or not.
	named map[*expr.AttributeExpr]struct{}
	// warnings are remarks about metadata the mapper cannot honour.
	warnings []string
}

// newTypeMapper returns a mapper that hoists the types declared by the roots.
func newTypeMapper(roots ...*expr.RootExpr) *typeMapper {
	m := &typeMapper{
		names:     make(map[decl]string),
		recursive: make(map[decl]bool),
		authored:  make(map[expr.UserType]struct{}),
		attrs:     make(map[*expr.AttributeExpr]decl),
		named:     make(map[*expr.AttributeExpr]struct{}),
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

// collect registers every hoisted declaration reachable from att: the user
// types the design names and the attributes named with
// Meta("typescript:name").
func (m *typeMapper) collect(att *expr.AttributeExpr) {
	if att == nil || att.Type == nil {
		return
	}
	seen := make(map[expr.UserType]struct{})
	var walk func(a *expr.AttributeExpr, member string, isMember bool)
	walk = func(a *expr.AttributeExpr, member string, isMember bool) {
		if a == nil || a.Type == nil {
			return
		}
		if isMember {
			m.collectNamed(a, member)
		}
		if origin, ok := m.hoist(a.Type); ok {
			d := decl{ut: origin}
			if _, dup := m.names[d]; !dup {
				m.names[d] = ""
				m.origins = append(m.origins, d)
			}
		}
		switch t := a.Type.(type) {
		case expr.UserType:
			if _, dup := seen[t.Origin()]; dup {
				return
			}
			seen[t.Origin()] = struct{}{}
			// goa hands out copies of a user type that share its origin. A
			// hoisted type is rendered from the origin, so its members are
			// collected there too, or a named attribute would be hoisted
			// once per copy. The attribute of a user type is the type
			// itself, not a member of anything: its metadata names the type.
			if origin, ok := m.hoist(t); ok {
				walk(origin.Attribute(), "", false)
			} else {
				walk(t.Attribute(), "", false)
			}
		case *expr.Array:
			walk(t.ElemType, "", true)
		case *expr.Map:
			walk(t.ElemType, "", true)
		case *expr.Object:
			for _, nat := range *t {
				walk(nat.Attribute, nat.Name, true)
			}
		case *expr.Union:
			for _, nat := range t.Values {
				walk(nat.Attribute, nat.Name, true)
			}
		}
	}
	// A method payload or result that goa did not wrap in a user type is an
	// attribute in its own right.
	_, wrapped := att.Type.(expr.UserType)
	walk(att, "", !wrapped)
}

// collectNamed hoists an attribute that Meta("typescript:name") names, when
// its type has no name of its own: a primitive, an array, a map or a union.
// An inline object cannot be hoisted, because its attributes are resolved
// against the object that holds it, and a user type already has a name; the
// metadata is reported and ignored in both cases.
func (m *typeMapper) collectNamed(a *expr.AttributeExpr, member string) {
	name, ok := a.Meta.Last("typescript:name")
	if !ok || name == "" {
		return
	}
	if _, dup := m.named[a]; dup {
		return
	}
	m.named[a] = struct{}{}
	where := "an attribute"
	if member != "" {
		where = fmt.Sprintf("attribute %q", member)
	}
	switch t := a.Type.(type) {
	case expr.UserType:
		m.warnings = append(m.warnings, fmt.Sprintf(
			"Meta(\"typescript:name\", %q) on %s is ignored: its type %s already has a name",
			name, where, goaTypeName(t)))
	case *expr.Object:
		m.warnings = append(m.warnings, fmt.Sprintf(
			"Meta(\"typescript:name\", %q) on %s is ignored: an inline object attribute cannot be named; declare a Type instead",
			name, where))
	default:
		d := decl{att: a}
		m.attrs[a] = d
		m.names[d] = ""
		m.origins = append(m.origins, d)
	}
}

// declName returns the name a declaration asks for: the Meta("typescript:name")
// override, or the type name in the design.
func declName(d decl) string {
	if override, ok := d.attribute().Meta.Last("typescript:name"); ok && override != "" {
		return override
	}
	return goaTypeName(d.ut)
}

// assignNames gives every collected declaration its identifier. Declarations
// are visited in name order so that a collision resolves the same way on
// every run; a type the design declares keeps its name over an attribute
// that asks for the same one.
func (m *typeMapper) assignNames(scope *codegen.NameScope) {
	sorted := append([]decl(nil), m.origins...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := declName(sorted[i]), declName(sorted[j])
		if a != b {
			return a < b
		}
		return sorted[i].ut != nil && sorted[j].ut == nil
	})
	for _, d := range sorted {
		m.names[d] = scope.Unique(tsIdent(declName(d)))
	}
}

// walkInline visits the attributes rendered inline as part of d: every
// attribute reachable from d's own without entering another hoisted
// declaration. ref is called once for each hoisted declaration referred to.
func (m *typeMapper) walkInline(d decl, visit func(a *expr.AttributeExpr), ref func(decl)) {
	var (
		root   = d.attribute()
		seen   = make(map[decl]struct{})
		inline = make(map[expr.UserType]struct{})
		walk   func(a *expr.AttributeExpr)
	)
	refer := func(target decl) {
		if _, dup := seen[target]; !dup {
			seen[target] = struct{}{}
			ref(target)
		}
	}
	walk = func(a *expr.AttributeExpr) {
		if a == nil || a.Type == nil {
			return
		}
		if origin, ok := m.hoist(a.Type); ok {
			refer(decl{ut: origin})
			return
		}
		if target, ok := m.attrs[a]; ok && a != root {
			refer(target)
			return
		}
		visit(a)
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
	walk(root)
}

// directRefs returns the hoisted declarations that d refers to without going
// through another hoisted declaration.
func (m *typeMapper) directRefs(d decl) []decl {
	var out []decl
	m.walkInline(d, func(*expr.AttributeExpr) {}, func(target decl) { out = append(out, target) })
	return out
}

// order returns the hoisted declarations so that every one follows the ones
// its schema refers to: strongly connected components in topological order,
// ties broken by name. Members of a cycle are flagged recursive.
func (m *typeMapper) order() []decl {
	nodes := append([]decl(nil), m.origins...)
	sort.SliceStable(nodes, func(i, j int) bool { return m.names[nodes[i]] < m.names[nodes[j]] })
	edges := make(map[decl][]decl, len(nodes))
	for _, n := range nodes {
		edges[n] = m.directRefs(n)
	}

	// Tarjan's strongly connected components.
	var (
		index   = make(map[decl]int)
		low     = make(map[decl]int)
		onStack = make(map[decl]bool)
		stack   []decl
		next    int
		comps   [][]decl
		comp    = make(map[decl]int)
		visit   func(n decl)
	)
	visit = func(n decl) {
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
		var members []decl
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
	var out []decl
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
func (m *typeMapper) typeSpec(d decl, r *refs) *TypeSpec {
	att := d.attribute()
	spec := &TypeSpec{
		GoaName:     declName(d),
		Name:        m.names[d],
		Description: att.Description,
		Kind:        TypeAlias,
		Recursive:   m.recursive[d],
	}
	c := &renderCtx{m: m, refs: r}
	zodIndent := ""
	if spec.Recursive {
		zodIndent = "  "
	}
	if obj, ok := att.Type.(*expr.Object); ok {
		spec.Kind = TypeObject
		opts := objectOpts{}
		if rt, ok := d.ut.(*expr.ResultTypeExpr); ok {
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
	// A named primitive, array, map, union or reference to another type. The
	// copy stands for the attribute's own type and validations, so that a
	// named attribute is rendered in place rather than as a reference to its
	// own declaration.
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

// ref renders a reference to a hoisted declaration and records the import.
func (c *renderCtx) ref(d decl) string {
	name := c.m.names[d]
	c.refs.types[name] = struct{}{}
	return name
}

// namedAttr returns the declaration of an attribute hoisted under
// Meta("typescript:name"), if the reference can stand for the attribute here.
// It cannot when the attribute is an HTTP parameter whose rendering differs
// from the body rendering (bytes and untyped values travel as plain strings):
// the attribute is then rendered in place, and the declaration keeps the body
// form.
func (c *renderCtx) namedAttr(att *expr.AttributeExpr, param bool) (decl, bool) {
	d, ok := c.m.attrs[att]
	if !ok || (param && paramSensitive(att)) {
		return decl{}, false
	}
	return d, true
}

// paramSensitive reports whether an attribute is rendered differently as an
// HTTP parameter than as part of a body: it holds bytes or an untyped value.
func paramSensitive(att *expr.AttributeExpr) bool {
	switch t := att.Type.(type) {
	case expr.UserType:
		return paramSensitive(t.Attribute())
	case expr.Primitive:
		return t.Kind() == expr.BytesKind || t.Kind() == expr.AnyKind
	case *expr.Array:
		return paramSensitive(t.ElemType)
	case *expr.Map:
		return paramSensitive(t.ElemType)
	}
	return false
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
		_, named := c.namedAttr(f.att, f.param)
		if u, ok := f.att.Type.(*expr.Union); ok && len(u.Values) > 1 && !named {
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
		return c.ref(decl{ut: origin})
	}
	if d, ok := c.namedAttr(att, param); ok {
		return c.ref(d)
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
