package tsgen

import (
	"fmt"

	"goa.design/goa/v3/codegen"
	"goa.design/goa/v3/expr"
)

// unsupportedWebSocket returns why no function can be generated for a
// WebSocket endpoint, or "".
func unsupportedWebSocket(e *expr.HTTPEndpointExpr) string {
	for _, r := range e.Responses {
		if r.StatusCode >= 400 || r.Body == nil {
			continue
		}
		// Body("attr") on the response of a client-streaming method: goa
		// sends that attribute alone, and the frame cannot be decoded as
		// the result.
		if names := r.Body.Meta["origin:attribute"]; len(names) > 0 {
			return fmt.Sprintf("the WebSocket result is sent as its %q attribute alone, which is not supported", names[0])
		}
	}
	return ""
}

// buildWebSocketEndpoint builds the function for a WebSocket endpoint.
func (b *builder) buildWebSocketEndpoint(e *expr.HTTPEndpointExpr, scope, files *codegen.NameScope) (*EndpointSpec, error) {
	refs := newRefs()
	// goa always upgrades with GET and sends no Accept header of its own.
	ep, err := b.buildEndpointBase(e, EndpointWebSocket, camel(e.MethodExpr.Name), "", scope, files, refs)
	if err != nil {
		return nil, err
	}
	ep.Verb = "GET"
	ep.WS = b.buildWebSocket(e, ep, scope, refs)
	ep.ResultTS = "WebSocketStream<" + ep.WS.RecvTS() + ", " + ep.WS.SendTS() + ">"

	// The browser hides the handshake response, so no goa error can be
	// decoded: a refused connection is a NetworkError.
	ep.ErrorTypeName = scope.Unique(upperFirst(ep.FuncName) + "Error")
	ep.ErrorTS = "\n  | RequestValidationError\n  | TransportError"
	for _, he := range e.HTTPErrors {
		if he.ErrorExpr != nil {
			ep.WS.DeclaredErrors = appendUnique(ep.WS.DeclaredErrors, he.Name)
		}
	}
	ep.SpecTS = endpointSpecLiteral(ep, ep.Accept)

	refs.useRuntime(
		"type CallResult", "type Client", "type EndpointSpec", "type RequestValidationError",
		"type TransportError", "type WebSocketOptions", "type WebSocketStream", "connectWebSocket",
	)
	if ep.Request != nil {
		refs.useRuntime("encode")
	}
	ep.Imports = refs.imports()
	return ep, nil
}

// buildWebSocket describes the frames of a WebSocket endpoint.
func (b *builder) buildWebSocket(e *expr.HTTPEndpointExpr, ep *EndpointSpec, scope *codegen.NameScope, refs *refs) *WebSocketSpec {
	m := e.MethodExpr
	spec := &WebSocketSpec{}
	switch m.Stream {
	case expr.ClientStreamKind:
		spec.Kind = "client"
	case expr.BidirectionalStreamKind:
		spec.Kind = "bidirectional"
	default:
		spec.Kind = "server"
	}

	// What the server sends: every message of a server or bidirectional
	// stream, the one result of a client stream. goa sets StreamingResult to
	// the Result of a client stream when it finalizes the design.
	recv := m.StreamingResult
	if recv == nil {
		recv = m.Result
	}
	if !isEmpty(recv) {
		c := &renderCtx{m: b.mapper, refs: refs}
		if b.hoisted(recv.Type) {
			// The schema is used as a value, so the reference registers the import.
			spec.RecvTypeName = c.zod(recv, "", false).code
		} else {
			spec.RecvTypeName = scope.Unique(upperFirst(ep.FuncName) + "Result")
			spec.RecvInline = true
			spec.RecvTSDef = c.tsType(recv, "", false)
			z := c.zod(recv, "", false)
			spec.RecvZod, spec.RecvZodNote = z.code, z.note()
		}
		spec.Single = spec.Kind == "client"
	}

	// What the client sends. goa's streaming body is the streaming payload
	// as is (no Body mapping applies to it), so the frame is the payload's
	// JSON. The caller builds it: defaulted attributes are optional.
	if send := m.StreamingPayload; m.IsPayloadStreaming() && !isEmpty(send) {
		c := &renderCtx{m: b.mapper, refs: refs, input: true}
		if b.hoisted(send.Type) {
			spec.SendTypeName = c.zod(send, "", false).code
		} else {
			spec.SendTypeName = scope.Unique(upperFirst(ep.FuncName) + "StreamingPayload")
			spec.SendInline = true
			spec.SendTSDef = c.tsType(send, "", false)
			z := c.zod(send, "", false)
			spec.SendZod, spec.SendZodNote = z.code, z.note()
		}
	}

	// What the browser cannot send.
	for _, s := range ep.Security {
		if s.In == "header" {
			spec.HeaderCredentials = append(spec.HeaderCredentials, s)
		}
	}
	if ep.Request != nil {
		spec.HeaderParams = ep.Request.Headers
	}
	return spec
}

// isEmpty reports whether an attribute carries nothing.
func isEmpty(att *expr.AttributeExpr) bool {
	return att == nil || att.Type == nil || att.Type == expr.Empty
}

func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}
