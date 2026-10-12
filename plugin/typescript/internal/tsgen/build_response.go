package tsgen

import (
	"fmt"
	"mime"
	"sort"
	"strings"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/expr"
)

// isVoid reports whether a method has no result.
func isVoid(result *expr.AttributeExpr) bool {
	return result == nil || result.Type == nil || result.Type == expr.Empty
}

// emptyBody reports whether a response has no body.
func emptyBody(resp *expr.HTTPResponseExpr) bool {
	return resp.Body == nil || resp.Body.Type == nil || resp.Body.Type == expr.Empty
}

// successResponses returns the success responses of an endpoint by status.
// goa rejects two responses with the same status, so the status alone
// identifies a response.
func successResponses(e *expr.HTTPEndpointExpr) []*expr.HTTPResponseExpr {
	out := append([]*expr.HTTPResponseExpr(nil), e.Responses...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].StatusCode < out[j].StatusCode })
	return out
}

// unsupportedREST returns why no function can return the plain result of an
// endpoint, or "".
func unsupportedREST(e *expr.HTTPEndpointExpr) string {
	if e.Redirect != nil {
		return "redirect endpoints are not generated: fetch follows the redirect"
	}
	if r := route(e); e.SkipRequestBodyEncodeDecode && (r.Method == "GET" || r.Method == "HEAD") {
		return "fetch cannot send a request body with " + r.Method
	}
	result := e.MethodExpr.Result
	if isVoid(result) {
		return ""
	}
	scalar := expr.AsObject(result.Type) == nil
	for _, resp := range successResponses(e) {
		reason := ""
		_ = codegen.WalkMappedAttr(resp.Headers, func(_, elem string, _ bool, a *expr.AttributeExpr) error {
			if reason == "" && headerShape(a) == "" {
				reason = fmt.Sprintf("response header %q has type %s, which cannot be read from a header",
					elem, typeLabel(a.Type))
			}
			return nil
		})
		if reason != "" {
			return reason
		}
		if scalar && emptyBody(resp) && isEmptyMapped(resp.Headers) {
			return "the result travels in a response cookie, which JavaScript cannot read"
		}
		if !e.SkipResponseBodyEncodeDecode && !emptyBody(resp) && resp.ContentType != "" && !isJSONMediaType(resp.ContentType) {
			return fmt.Sprintf("the %d response has content type %q: only JSON responses are decoded",
				resp.StatusCode, resp.ContentType)
		}
	}
	return ""
}

func isEmptyMapped(ma *expr.MappedAttributeExpr) bool {
	return ma == nil || ma.IsEmpty()
}

// isJSONMediaType reports whether a content type is JSON: application/json or
// a "+json" suffix. The runtime applies the same rule to responses.
func isJSONMediaType(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mt = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	}
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

// headerShape returns how the runtime parses a response header into the wire
// form of an attribute: "string", "number", "boolean" or one of those followed
// by "[]". It returns "" for a type that cannot be read back. goa writes a
// Bytes header as the raw text, not as the base64 the schema expects, and an
// Any header through fmt.Sprintf.
func headerShape(att *expr.AttributeExpr) string {
	t := att.Type
	for {
		ut, ok := t.(expr.UserType)
		if !ok {
			break
		}
		t = ut.Attribute().Type
	}
	switch t := t.(type) {
	case expr.Primitive:
		switch t.Kind() {
		case expr.StringKind:
			return "string"
		case expr.BooleanKind:
			return "boolean"
		case expr.IntKind, expr.Int32Kind, expr.Int64Kind, expr.UIntKind, expr.UInt32Kind, expr.UInt64Kind,
			expr.Float32Kind, expr.Float64Kind:
			return "number"
		}
	case *expr.Array:
		if elem := headerShape(t.ElemType); elem != "" && !strings.HasSuffix(elem, "[]") {
			return elem + "[]"
		}
	}
	return ""
}

// typeLabel names a type in a message.
func typeLabel(t expr.DataType) string {
	switch t := t.(type) {
	case expr.UserType:
		return typeLabel(t.Attribute().Type)
	case *expr.Array:
		return "array of " + typeLabel(t.ElemType.Type)
	case *expr.Map:
		return "map"
	case *expr.Object:
		return "object"
	}
	return t.Name()
}

// buildResponses describes the value a REST function returns and how each
// success response carries it.
func (b *builder) buildResponses(e *expr.HTTPEndpointExpr, ep *EndpointSpec, scope *codegen.NameScope, refs *refs) {
	var (
		c      = &renderCtx{m: b.mapper, refs: refs}
		result = e.MethodExpr.Result
		void   = isVoid(result)
		obj    *expr.Object
	)
	ep.Result = &ResultSpec{}
	ep.ResultTS = "void"
	if !void {
		obj = expr.AsObject(result.Type)
		if b.hoisted(result.Type) {
			// The schema is used as a value, so the reference registers the import.
			ep.Result.TypeName = c.zod(result, "", false).code
		} else {
			ep.Result.TypeName = scope.Unique(upperFirst(ep.FuncName) + "Result")
			ep.Result.Inline = true
			ep.Result.TSDef = c.tsType(result, "", false)
			z := c.zod(result, "", false)
			ep.Result.Zod, ep.Result.ZodNote = z.code, z.note()
		}
		ep.ResultTS = ep.Result.TypeName
	}

	// key returns the property that holds a result attribute in the decoded
	// value, and whether it has one.
	key := func(name string) (string, bool) {
		name, _, _ = strings.Cut(name, ":")
		if obj == nil {
			return "", true
		}
		wk := jsonKey(name, obj.Attribute(name))
		return wk.Key, !wk.Omit
	}
	var fields []field
	if !void {
		fields = c.valueFields(result)
	}

	for _, resp := range successResponses(e) {
		rs := &ResponseSpec{Status: resp.StatusCode, ContentType: resp.ContentType, Scalar: !void && obj == nil}
		// carried are the result attributes this response holds.
		carried := make(map[string]struct{})
		switch {
		case e.SkipResponseBodyEncodeDecode:
			rs.Body = BodyRaw
		case void || emptyBody(resp):
			rs.Body = BodyNone
		case len(resp.Body.Meta["origin:attribute"]) > 0 && obj != nil:
			name := resp.Body.Meta["origin:attribute"][0]
			rs.Body = BodyAttr
			rs.Attr, _ = key(name)
			carried[name] = struct{}{}
		case obj == nil:
			rs.Body = BodyWhole
			rs.NullAs = nullAs(result.Type)
		default:
			rs.Body = BodyFields
			if body := expr.AsObject(resp.Body.Type); body != nil {
				for _, nat := range *body {
					name, _, _ := strings.Cut(nat.Name, ":")
					carried[name] = struct{}{}
				}
			}
		}
		if !void {
			_ = codegen.WalkMappedAttr(resp.Headers, func(name, elem string, _ bool, a *expr.AttributeExpr) error {
				if k, ok := key(name); ok {
					rs.Headers = append(rs.Headers, HeaderSpec{Attr: name, Key: k, Name: elem, Shape: headerShape(a)})
					carried[name] = struct{}{}
				}
				return nil
			})
			_ = codegen.WalkMappedAttr(resp.Cookies, func(name, _ string, _ bool, _ *expr.AttributeExpr) error {
				if k, ok := key(name); ok && obj != nil {
					rs.Cookies = append(rs.Cookies, k)
				}
				return nil
			})
			if resp.Tag[0] != "" && obj != nil {
				if k, ok := key(resp.Tag[0]); ok {
					rs.Tag = [2]string{k, resp.Tag[1]}
					carried[resp.Tag[0]] = struct{}{}
				}
			}
		}
		for _, f := range fields {
			if _, ok := carried[f.name]; !ok && !f.optional && f.def == "" {
				rs.Missing = append(rs.Missing, f.key)
			}
		}
		ep.Responses = append(ep.Responses, rs)
	}
	ep.ResponsesTS = responsesLiteral(ep.Responses)
}

// nullAs returns what a null body stands for when the result is an array or
// a map: a Go service encodes a nil slice or map as null.
func nullAs(t expr.DataType) string {
	for {
		ut, ok := t.(expr.UserType)
		if !ok {
			break
		}
		t = ut.Attribute().Type
	}
	switch t.(type) {
	case *expr.Array:
		return "array"
	case *expr.Map:
		return "map"
	}
	return ""
}

// errorHeaders returns the attributes of a custom error type that its
// response sends as headers. A header whose type cannot be read back is left
// out, as the attribute would be if the header were missing.
func errorHeaders(resp *expr.HTTPResponseExpr, att *expr.AttributeExpr) []HeaderSpec {
	obj := expr.AsObject(att.Type)
	if obj == nil || resp.Headers == nil {
		return nil
	}
	var out []HeaderSpec
	_ = codegen.WalkMappedAttr(resp.Headers, func(name, elem string, _ bool, a *expr.AttributeExpr) error {
		name, _, _ = strings.Cut(name, ":")
		wk := jsonKey(name, obj.Attribute(name))
		if shape := headerShape(a); shape != "" && !wk.Omit {
			out = append(out, HeaderSpec{Attr: name, Key: wk.Key, Name: elem, Shape: shape})
		}
		return nil
	})
	return out
}

// headersLiteral renders a runtime ResponseHeaders value on one line.
func headersLiteral(headers []HeaderSpec) string {
	parts := make([]string, len(headers))
	for i, h := range headers {
		parts[i] = headerEntry(h)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func headerEntry(h HeaderSpec) string {
	return tsPropKey(h.Key) + ": { name: " + tsString(h.Name) + ", shape: " + tsString(h.Shape) + " }"
}

// responsesLiteral renders the runtime ResponseTable of an endpoint.
func responsesLiteral(responses []*ResponseSpec) string {
	var b strings.Builder
	b.WriteString("{\n")
	for _, r := range responses {
		props := []string{"body: " + tsString(string(r.Body))}
		if r.Body == BodyAttr {
			props = append(props, "attr: "+tsString(r.Attr))
		}
		if r.NullAs != "" {
			props = append(props, "nullAs: "+tsString(r.NullAs))
		}
		if r.Scalar {
			props = append(props, "scalar: true")
		}
		if r.Tag[0] != "" {
			props = append(props, "tag: ["+tsString(r.Tag[0])+", "+tsString(r.Tag[1])+"]")
		}
		line := fmt.Sprintf("  %d: { %s", r.Status, strings.Join(props, ", "))
		switch {
		case len(r.Headers) == 0:
			b.WriteString(line + " },\n")
		case len(line)+len(headersLiteral(r.Headers)) <= 84:
			b.WriteString(line + ", headers: " + headersLiteral(r.Headers) + " },\n")
		default:
			fmt.Fprintf(&b, "  %d: {\n", r.Status)
			for _, p := range props {
				b.WriteString("    " + p + ",\n")
			}
			b.WriteString("    headers: {\n")
			for _, h := range r.Headers {
				b.WriteString("      " + headerEntry(h) + ",\n")
			}
			b.WriteString("    },\n  },\n")
		}
	}
	b.WriteString("} as const")
	return b.String()
}
