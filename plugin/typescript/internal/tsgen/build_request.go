package tsgen

import (
	"fmt"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/expr"
)

// securityBindings returns where each security scheme of the endpoint puts
// its credential, and the payload attributes those credentials replace. The
// client supplies credentials from its `auth` option, so the attributes are
// left out of the request type.
func securityBindings(e *expr.HTTPEndpointExpr) ([]SecurityBinding, map[string]struct{}) {
	var (
		bindings []SecurityBinding
		strip    = make(map[string]struct{})
		seen     = make(map[string]struct{})
		payload  = e.MethodExpr.Payload
		inPath   = make(map[string]struct{})
	)
	_ = codegen.WalkMappedAttr(e.PathParams(), func(name, _ string, _ bool, _ *expr.AttributeExpr) error {
		inPath[name] = struct{}{}
		return nil
	})
	for _, req := range e.Requirements {
		for _, sch := range req.Schemes {
			if _, dup := seen[sch.SchemeName]; dup {
				continue
			}
			binding := SecurityBinding{Scheme: sch.SchemeName, In: sch.In, Name: sch.Name}
			switch sch.Kind {
			case expr.BasicAuthKind:
				user := expr.TaggedAttribute(payload, "security:username")
				pass := expr.TaggedAttribute(payload, "security:password")
				if user == "" || pass == "" {
					continue
				}
				binding.Kind, binding.In, binding.Name, binding.Attr = "basic", "header", "Authorization", user
				strip[user], strip[pass] = struct{}{}, struct{}{}
				seen[sch.SchemeName] = struct{}{}
				bindings = append(bindings, binding)
				continue
			case expr.APIKeyKind:
				binding.Kind = "apiKey"
				binding.Attr = expr.TaggedAttribute(payload, "security:apikey:"+sch.SchemeName)
			case expr.JWTKind:
				binding.Kind = "bearer"
				binding.Attr = expr.TaggedAttribute(payload, "security:token")
			case expr.OAuth2Kind:
				binding.Kind = "bearer"
				binding.Attr = expr.TaggedAttribute(payload, "security:accesstoken")
			case expr.BearerKind:
				binding.Kind = "bearer"
				binding.Attr = expr.TaggedAttribute(payload, "security:bearer")
			default:
				continue
			}
			if binding.Attr == "" {
				continue
			}
			// A credential in the body, a cookie or the path stays an
			// ordinary request property.
			if _, path := inPath[binding.Attr]; path || (binding.In != "header" && binding.In != "query") {
				continue
			}
			strip[binding.Attr] = struct{}{}
			seen[sch.SchemeName] = struct{}{}
			bindings = append(bindings, binding)
		}
	}
	return bindings, strip
}

// buildRequest describes the request argument of an endpoint: the payload
// without the attributes in strip, and how its properties map onto the HTTP
// request. It returns nil when nothing is left to pass.
func (b *builder) buildRequest(e *expr.HTTPEndpointExpr, strip map[string]struct{}, typeName string, refs *refs) (*RequestSpec, error) {
	payload := e.MethodExpr.Payload
	if payload == nil || payload.Type == nil || payload.Type == expr.Empty {
		return nil, nil
	}
	c := &renderCtx{m: b.mapper, refs: refs}
	req := &RequestSpec{TypeName: typeName, Multipart: e.MultipartRequest}
	obj := expr.AsObject(payload.Type)
	req.Scalar = obj == nil

	// prop is the request property holding an attribute; a non-object
	// payload is the value itself.
	prop := func(name string) string {
		if req.Scalar {
			return ""
		}
		return name
	}
	params := make(map[string]struct{})
	mapped := func(ma *expr.MappedAttributeExpr) []ParamSpec {
		var out []ParamSpec
		if ma == nil {
			return nil
		}
		_ = codegen.WalkMappedAttr(ma, func(name, elem string, required bool, a *expr.AttributeExpr) error {
			if _, skip := strip[name]; skip {
				return nil
			}
			params[name] = struct{}{}
			out = append(out, ParamSpec{Attr: name, Prop: prop(name), Wire: elem, Required: required, Shape: shape(a)})
			return nil
		})
		return out
	}
	req.Path = mapped(e.PathParams())
	req.Query = mapped(e.QueryParams())
	req.Headers = mapped(e.Headers)
	req.Cookies = mapped(e.Cookies)
	if e.MapQueryParams != nil {
		name := *e.MapQueryParams
		req.MapQuery = &ParamSpec{Attr: name, Prop: prop(name), Shape: "map"}
		if name != "" {
			params[name] = struct{}{}
		}
	}

	plainKey := make(map[string]struct{})
	switch {
	case e.SkipRequestBodyEncodeDecode:
		// The caller hands the body to the function; no attribute is in it.
		req.Body = BodySpec{Kind: BodyRaw}
	case e.Body == nil || e.Body.Type == nil || e.Body.Type == expr.Empty:
		req.Body = BodySpec{Kind: BodyNone}
	case len(e.Body.Meta["origin:attribute"]) > 0:
		name := e.Body.Meta["origin:attribute"][0]
		if _, skip := strip[name]; skip {
			return nil, fmt.Errorf("body attribute %q is a credential", name)
		}
		req.Body = BodySpec{Kind: BodyAttr, Attr: name}
		plainKey[name] = struct{}{}
	case req.Scalar:
		req.Body = BodySpec{Kind: BodyWhole}
	default:
		req.Body = BodySpec{Kind: BodyFields}
		if body := expr.AsObject(e.Body.Type); body != nil {
			for _, nat := range *body {
				att := obj.Attribute(nat.Name)
				if att == nil {
					continue
				}
				if _, skip := strip[nat.Name]; skip {
					continue
				}
				if wk := jsonKey(nat.Name, att); !wk.Omit {
					req.Body.Fields = append(req.Body.Fields, wk.Key)
				}
			}
		}
		if len(req.Body.Fields) == 0 {
			req.Body = BodySpec{Kind: BodyNone}
		}
	}

	if req.Scalar {
		// The value travels either as the body or as the single parameter
		// or header the design maps it to.
		param := req.Body.Kind == BodyNone || req.Body.Kind == BodyRaw
		req.TSDef = c.tsType(payload, "", param)
		z := c.zod(payload, "", param)
		req.Zod, req.ZodNote = z.code, z.note()
		return req, nil
	}

	fields := c.fields(payload, obj, objectOpts{request: true, skip: strip, params: params, plainKey: plainKey})
	if len(fields) == 0 {
		return nil, nil
	}
	seen := make(map[string]string)
	for _, f := range fields {
		if other, dup := seen[f.key]; dup {
			return nil, fmt.Errorf("payload attributes %q and %q both map to request property %q", other, f.name, f.key)
		}
		seen[f.key] = f.name
	}
	req.TSDef = c.objectTS(fields, "")
	req.Zod, _ = c.objectZod(fields, "")
	return req, nil
}

// shape classifies how a parameter value is serialized.
func shape(att *expr.AttributeExpr) string {
	switch {
	case expr.IsArray(att.Type):
		return "array"
	case expr.IsMap(att.Type):
		return "map"
	}
	return "scalar"
}
