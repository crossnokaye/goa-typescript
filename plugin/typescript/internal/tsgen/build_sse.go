package tsgen

import (
	"goa.design/goa/v3/expr"
)

// buildSSE describes how an endpoint maps its events onto Server-Sent Events.
// inlineName names the event type when the design does not.
func (b *builder) buildSSE(e *expr.HTTPEndpointExpr, inlineName string, refs *refs) *SSESpec {
	c := &renderCtx{m: b.mapper, refs: refs}
	event := e.MethodExpr.StreamingResult
	if event == nil {
		event = e.MethodExpr.Result
	}
	spec := &SSESpec{RequestIDField: e.SSE.RequestIDField}

	if _, ok := b.mapper.hoist(event.Type); ok {
		// The schema is used as a value, so the reference registers the import.
		spec.EventTypeName = c.zod(event, "", false).code
	} else {
		spec.EventTypeName = inlineName
		spec.EventInline = true
		spec.EventTSDef = c.tsType(event, "", false)
		z := c.zod(event, "", false)
		spec.EventZod, spec.EventZodNote = z.code, z.note()
	}

	// key returns the JSON key of an event attribute, "" when it is not mapped.
	obj := expr.AsObject(event.Type)
	key := func(name string) string {
		if name == "" || obj == nil {
			return ""
		}
		return jsonKey(name, obj.Attribute(name)).Key
	}
	spec.DataField = key(e.SSE.DataField)
	spec.IDField = key(e.SSE.IDField)
	spec.EventField = key(e.SSE.EventField)
	spec.RetryField = key(e.SSE.RetryField)

	data := event
	if e.SSE.DataField != "" && obj != nil {
		if att := obj.Attribute(e.SSE.DataField); att != nil {
			data = att
		}
	}
	spec.DataEncoding = sseEncoding(data.Type)
	return spec
}

// sseEncoding returns how goa writes a value into the SSE data field:
// primitives as text, everything else as JSON.
func sseEncoding(t expr.DataType) string {
	for expr.IsAlias(t) {
		t = t.(expr.UserType).Attribute().Type
	}
	if _, ok := t.(expr.Primitive); !ok {
		return "json"
	}
	switch t.Kind() {
	case expr.StringKind:
		return "string"
	case expr.BooleanKind:
		return "boolean"
	case expr.IntKind, expr.Int32Kind, expr.Int64Kind, expr.UIntKind, expr.UInt32Kind, expr.UInt64Kind:
		return "int"
	case expr.Float32Kind, expr.Float64Kind:
		return "float"
	case expr.BytesKind:
		return "bytes"
	}
	return "json"
}
