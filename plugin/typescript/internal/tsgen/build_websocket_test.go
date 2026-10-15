package tsgen

import (
	"strings"
	"testing"

	"github.com/crossnokaye/goa-typescript/plugin/typescript/internal/tsgen/testdata"
)

func TestBuildWebSocketServer(t *testing.T) {
	pkg := build(t, testdata.WSServerDSL)
	svc := pkg.Services[0]
	if !svc.HasWS || svc.HasREST || svc.HasSSE {
		t.Errorf("flags: SSE=%v REST=%v WS=%v", svc.HasSSE, svc.HasREST, svc.HasWS)
	}
	wantEqual(t, "skipped", len(svc.Skipped), 0)

	ep := endpoint(t, svc, "watch_ticks")
	wantEqual(t, "kind", ep.Kind, EndpointWebSocket)
	wantEqual(t, "verb", ep.Verb, "GET")
	wantEqual(t, "ws kind", ep.WS.Kind, "server")
	wantEqual(t, "recv", ep.WS.RecvTypeName, "Tick")
	wantEqual(t, "recv inline", ep.WS.RecvInline, false)
	wantEqual(t, "send", ep.WS.SendTS(), "never")
	wantEqual(t, "single", ep.WS.Single, false)
	wantEqual(t, "result", ep.ResultTS, "WebSocketStream<Tick, never>")
	wantEqual(t, "declared errors", ep.WS.DeclaredErrors, []string{"not_found"})
	wantEqual(t, "errors", ep.ErrorTS, "\n  | RequestValidationError\n  | TransportError")
	if !strings.Contains(ep.SpecTS, `accept: "",`) {
		t.Errorf("spec sends an Accept header:\n%s", ep.SpecTS)
	}
	wantEqual(t, "type imports", ep.Imports.Types, []string{"Tick"})

	counts := endpoint(t, svc, "watch_counts")
	wantEqual(t, "inline recv", []any{counts.WS.RecvTypeName, counts.WS.RecvInline, counts.WS.RecvZod},
		[]any{"WatchCountsResult", true, "z.int()"})
	if counts.Request != nil {
		t.Errorf("watch_counts has a request: %+v", counts.Request)
	}
}

func TestBuildWebSocketBidirectional(t *testing.T) {
	svc := build(t, testdata.WSBidirectionalDSL).Services[0]
	echo := endpoint(t, svc, "echo")
	wantEqual(t, "kind", echo.WS.Kind, "bidirectional")
	wantEqual(t, "names", []string{echo.WS.RecvTypeName, echo.WS.SendTypeName}, []string{"EchoResult", "EchoStreamingPayload"})
	// The caller builds the message: the defaulted attribute is optional.
	if !strings.Contains(echo.WS.SendTSDef, "repeat?: number") {
		t.Errorf("defaulted attribute is required in the sent type:\n%s", echo.WS.SendTSDef)
	}

	notes := endpoint(t, svc, "notes")
	wantEqual(t, "named", []string{notes.WS.RecvTypeName, notes.WS.SendTypeName}, []string{"Note", "NoteRequest"})
	wantEqual(t, "type imports", notes.Imports.Types, []string{"Note", "NoteRequest"})
}

func TestBuildWebSocketClient(t *testing.T) {
	svc := build(t, testdata.WSClientDSL).Services[0]
	sum := endpoint(t, svc, "sum")
	wantEqual(t, "sum", []any{sum.WS.Kind, sum.WS.RecvTypeName, sum.WS.Single}, []any{"client", "Total", true})

	drain := endpoint(t, svc, "drain")
	wantEqual(t, "drain", []any{drain.WS.RecvTS(), drain.WS.Single}, []any{"never", false})

	wantEqual(t, "skipped", svc.Skipped, []SkippedEndpoint{{
		Method: "partial",
		Reason: `the WebSocket result is sent as its "sum" attribute alone, which is not supported`,
	}})
}

func TestBuildWebSocketSecurity(t *testing.T) {
	svc := build(t, testdata.WSSecurityDSL).Services[0]

	keyed := endpoint(t, svc, "keyed")
	wantEqual(t, "keyed request", keyed.Request.TSDef, "{\n  id: string;\n}")
	wantEqual(t, "keyed header credentials", len(keyed.WS.HeaderCredentials), 0)

	jwt := endpoint(t, svc, "jwt")
	if jwt.Request == nil || strings.Contains(jwt.Request.TSDef, "token") {
		t.Errorf("the token is not stripped from the request: %+v", jwt.Request)
	}
	wantEqual(t, "jwt header credentials", len(jwt.WS.HeaderCredentials), 1)
	wantEqual(t, "jwt header params", len(jwt.WS.HeaderParams), 1)
	doc := webSocketDoc(jwt)
	for _, want := range []string{"cannot send headers", "the `jwt` credential, in `Authorization`", "`trace`, in `X-Trace-Id`"} {
		if !strings.Contains(doc, want) {
			t.Errorf("doc lacks %q:\n%s", want, doc)
		}
	}
	if strings.Contains(webSocketDoc(keyed), "cannot send headers") {
		t.Errorf("keyed doc warns about headers it does not use")
	}
}
