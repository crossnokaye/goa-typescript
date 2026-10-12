package tsgen

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"goa.design/goa/v3/expr"
)

// zexpr is a rendered Zod expression and the remarks that belong next to it.
type zexpr struct {
	code  string
	notes []string
}

// note joins the remarks into the text of a trailing comment.
func (z zexpr) note() string {
	return strings.Join(z.notes, "; ")
}

// objectZod renders a z.object schema. indent is the indentation of the line
// the expression starts on. The second result is always empty: remarks are
// attached to the property they concern.
func (c *renderCtx) objectZod(fields []field, indent string) (string, string) {
	c.refs.zod = true
	if len(fields) == 0 {
		return "z.object({})", ""
	}
	var b strings.Builder
	b.WriteString("z.object({\n")
	inner := indent + "  "
	for _, f := range fields {
		z := c.zod(f.att, inner, f.param)
		code := z.code
		switch {
		case f.def != "":
			code += ".default(" + f.def + ")"
		case f.optional && f.nullWhenUnset:
			c.refs.useRuntime("nullAsAbsent")
			code = "nullAsAbsent(" + code + ")"
		case f.optional:
			code += ".optional()"
		}
		b.WriteString(inner + tsPropKey(f.key) + ": " + code + ",")
		if note := z.note(); note != "" {
			b.WriteString(" // " + note)
		}
		b.WriteString("\n")
	}
	b.WriteString(indent + "})")
	return b.String(), ""
}

// zod renders the Zod schema of an attribute. param selects the encoding of
// HTTP parameters, where goa sends bytes and untyped values as plain strings.
//
// Only checks and codecs are emitted, never `.transform()`: a transform cannot
// run backwards, and every schema is used to encode as well as to decode.
func (c *renderCtx) zod(att *expr.AttributeExpr, indent string, param bool) zexpr {
	if origin, ok := c.m.hoist(att.Type); ok {
		return zexpr{code: c.ref(decl{ut: origin})}
	}
	// A named attribute is checked before its primitive rendering: the
	// declaration carries the validations.
	if d, ok := c.namedAttr(att, param); ok {
		return zexpr{code: c.ref(d)}
	}
	c.refs.zod = true
	switch t := att.Type.(type) {
	case expr.UserType:
		// A type the design does not name: render what it stands for.
		if !c.enter(t) {
			return zexpr{code: "z.unknown()"}
		}
		defer c.leave(t)
		return c.zod(t.Attribute(), indent, param)
	case expr.Primitive:
		return c.zodPrimitive(att, t, param)
	case *expr.Array:
		elem := c.zod(t.ElemType, indent, param)
		code := "z.array(" + elem.code + ")"
		if v := att.Validation; v != nil {
			if v.MinLength != nil {
				code += fmt.Sprintf(".min(%d)", *v.MinLength)
			}
			if v.MaxLength != nil {
				code += fmt.Sprintf(".max(%d)", *v.MaxLength)
			}
		}
		return zexpr{code: code, notes: elem.notes}
	case *expr.Map:
		// JSON object keys are strings whatever the design key type is.
		elem := c.zod(t.ElemType, indent, param)
		code := "z.record(z.string(), " + elem.code + ")"
		if v := att.Validation; v != nil {
			if v.MinLength != nil {
				code += fmt.Sprintf(".refine((m) => Object.keys(m).length >= %d, { message: %s })",
					*v.MinLength, tsString(fmt.Sprintf("must have at least %d entries", *v.MinLength)))
			}
			if v.MaxLength != nil {
				code += fmt.Sprintf(".refine((m) => Object.keys(m).length <= %d, { message: %s })",
					*v.MaxLength, tsString(fmt.Sprintf("must have at most %d entries", *v.MaxLength)))
			}
		}
		return zexpr{code: code, notes: elem.notes}
	case *expr.Object:
		code, _ := c.objectZod(c.fields(att, t, objectOpts{}), indent)
		return zexpr{code: code}
	case *expr.Union:
		if len(t.Values) == 0 {
			return zexpr{code: "z.never()"}
		}
		typeKey, valueKey := tsPropKey(t.GetTypeKey()), tsPropKey(t.GetValueKey())
		var (
			b     strings.Builder
			notes []string
		)
		b.WriteString("z.discriminatedUnion(" + tsString(t.GetTypeKey()) + ", [\n")
		inner := indent + "  "
		for _, nat := range t.Values {
			value := c.zod(nat.Attribute, inner, false)
			notes = append(notes, value.notes...)
			b.WriteString(inner + "z.object({ " + typeKey + ": z.literal(" + tsString(nat.Name) + "), " +
				valueKey + ": " + value.code + " }),\n")
		}
		b.WriteString(indent + "])")
		return zexpr{code: b.String(), notes: notes}
	}
	return zexpr{code: "z.unknown()"}
}

func (c *renderCtx) zodPrimitive(att *expr.AttributeExpr, t expr.Primitive, param bool) zexpr {
	if lits, ok := enumLiterals(att); ok {
		list := "[" + strings.Join(lits, ", ") + "]"
		if t.Kind() == expr.StringKind {
			return zexpr{code: "z.enum(" + list + ")"}
		}
		return zexpr{code: "z.literal(" + list + ")"}
	}
	switch t.Kind() {
	case expr.BooleanKind:
		return zexpr{code: "z.boolean()"}
	case expr.IntKind, expr.Int64Kind:
		return zexpr{code: "z.int()" + numberChecks(att)}
	case expr.Int32Kind:
		return zexpr{code: "z.int32()" + numberChecks(att)}
	case expr.UIntKind, expr.UInt64Kind:
		return zexpr{code: "z.int().min(0)" + numberChecks(att)}
	case expr.UInt32Kind:
		return zexpr{code: "z.uint32()" + numberChecks(att)}
	case expr.Float32Kind, expr.Float64Kind:
		return zexpr{code: "z.number()" + numberChecks(att)}
	case expr.StringKind:
		return c.zodString(att)
	case expr.BytesKind:
		if param {
			return zexpr{code: "z.string()"}
		}
		c.refs.useRuntime("bytes")
		return zexpr{code: "bytes()"}
	default: // expr.AnyKind
		if param {
			return zexpr{code: "z.string()"}
		}
		return zexpr{code: "z.unknown()"}
	}
}

func (c *renderCtx) zodString(att *expr.AttributeExpr) zexpr {
	var (
		z        zexpr
		chain    = true
		v        = att.Validation
		format   = formatOf(att)
		lenient  = false
		baseCode string
	)
	switch format {
	case expr.FormatDateTime:
		c.refs.useRuntime("dateTime")
		return zexpr{code: "dateTime()"}
	case expr.FormatDate:
		baseCode, chain = "z.iso.date()", false
	case expr.FormatUUID:
		baseCode = "z.uuid()"
	case expr.FormatEmail:
		baseCode = "z.email()"
	case expr.FormatHostname:
		baseCode = "z.hostname()"
	case expr.FormatIPv4:
		baseCode = "z.ipv4()"
	case expr.FormatIPv6:
		baseCode = "z.ipv6()"
	case expr.FormatIP:
		baseCode, chain = "z.union([z.ipv4(), z.ipv6()])", false
	case expr.FormatCIDR:
		baseCode, chain = "z.union([z.cidrv4(), z.cidrv6()])", false
	case "":
		baseCode = "z.string()"
	default:
		// uri, mac, regexp, json, rfc1123: goa's own checks have no faithful
		// Zod counterpart, so these stay plain strings.
		baseCode, lenient = "z.string()", true
	}
	z.code = baseCode
	if lenient {
		z.notes = append(z.notes, "format: "+string(format))
	}
	if v == nil || !chain {
		return z
	}
	if v.MinLength != nil {
		z.code += fmt.Sprintf(".min(%d)", *v.MinLength)
	}
	if v.MaxLength != nil {
		z.code += fmt.Sprintf(".max(%d)", *v.MaxLength)
	}
	if v.Pattern != "" {
		if jsCompatiblePattern(v.Pattern) {
			z.code += ".regex(new RegExp(" + tsString(v.Pattern) + "))"
		} else {
			z.notes = append(z.notes, "pattern not enforced (RE2-only syntax): "+strings.ReplaceAll(v.Pattern, "\n", `\n`))
		}
	}
	return z
}

// numberChecks renders the range checks of a numeric attribute.
func numberChecks(att *expr.AttributeExpr) string {
	v := att.Validation
	if v == nil {
		return ""
	}
	var b strings.Builder
	if v.Minimum != nil {
		b.WriteString(".gte(" + formatNumber(*v.Minimum) + ")")
	}
	if v.ExclusiveMinimum != nil {
		b.WriteString(".gt(" + formatNumber(*v.ExclusiveMinimum) + ")")
	}
	if v.Maximum != nil {
		b.WriteString(".lte(" + formatNumber(*v.Maximum) + ")")
	}
	if v.ExclusiveMaximum != nil {
		b.WriteString(".lt(" + formatNumber(*v.ExclusiveMaximum) + ")")
	}
	return b.String()
}

func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// re2Only matches regular expression syntax that Go accepts and JavaScript
// rejects or reads differently: named groups written (?P<name>, inline flags,
// \A \z \Q \E \C anchors and escapes, POSIX and Unicode classes, \x{...}.
var re2Only = regexp.MustCompile(`\(\?P<|\(\?[a-zA-Z-]+[:)]|\\[AzZQEC]|\[\[:|\\[pP]|\\x\{`)

// jsCompatiblePattern reports whether a goa Pattern means the same thing as a
// JavaScript RegExp.
func jsCompatiblePattern(pattern string) bool {
	return !re2Only.MatchString(pattern)
}

// enumLiterals renders the values of an enumeration over a primitive type.
func enumLiterals(att *expr.AttributeExpr) ([]string, bool) {
	if att.Validation == nil || len(att.Validation.Values) == 0 {
		return nil, false
	}
	if p, ok := att.Type.(expr.Primitive); !ok || p.Kind() == expr.AnyKind || p.Kind() == expr.BytesKind {
		return nil, false
	}
	lits := make([]string, 0, len(att.Validation.Values))
	for _, v := range att.Validation.Values {
		lit, ok := scalarLiteral(v)
		if !ok {
			return nil, false
		}
		lits = append(lits, lit)
	}
	return lits, true
}

// scalarLiteral renders a string, number or boolean design value.
func scalarLiteral(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return tsString(x), true
	case bool:
		return strconv.FormatBool(x), true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", x), true
	case float32:
		return formatNumber(float64(x)), true
	case float64:
		return formatNumber(x), true
	}
	return "", false
}

// defaultLiteral renders the default value of an attribute as the expression
// passed to `.default()`, which takes a value of the decoded type. It reports
// false when there is no default or it cannot be written as a literal; the
// attribute is then treated as having none.
func defaultLiteral(att *expr.AttributeExpr) (string, bool) {
	if att.DefaultValue == nil {
		return "", false
	}
	// Look through named primitives.
	under := att
	for {
		ut, ok := under.Type.(expr.UserType)
		if !ok {
			break
		}
		under = ut.Attribute()
	}
	switch t := under.Type.(type) {
	case expr.Primitive:
		switch t.Kind() {
		case expr.BytesKind:
			return "", false
		case expr.StringKind:
			s, ok := att.DefaultValue.(string)
			if !ok {
				return "", false
			}
			if formatOf(under) == expr.FormatDateTime || formatOf(att) == expr.FormatDateTime {
				return "new Date(" + tsString(s) + ")", true
			}
			return tsString(s), true
		case expr.AnyKind:
			return jsonLiteral(att.DefaultValue)
		default:
			return scalarLiteral(att.DefaultValue)
		}
	case *expr.Array:
		if !plainJSON(t.ElemType) {
			return "", false
		}
		return jsonLiteral(att.DefaultValue)
	case *expr.Map:
		if !plainJSON(t.ElemType) {
			return "", false
		}
		return jsonLiteral(att.DefaultValue)
	}
	return "", false
}

// plainJSON reports whether values of the attribute decode to themselves:
// primitives other than date-time strings and bytes.
func plainJSON(att *expr.AttributeExpr) bool {
	p, ok := att.Type.(expr.Primitive)
	if !ok {
		return false
	}
	return p.Kind() != expr.BytesKind && formatOf(att) != expr.FormatDateTime
}

// jsonLiteral renders a design value as a JSON literal.
func jsonLiteral(v any) (string, bool) {
	b, err := json.Marshal(jsonable(v))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// jsonable converts the map[any]any values the DSL can produce into maps that
// encoding/json accepts.
func jsonable(v any) any {
	switch x := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[fmt.Sprint(k)] = jsonable(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = jsonable(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = jsonable(e)
		}
		return out
	}
	return v
}
