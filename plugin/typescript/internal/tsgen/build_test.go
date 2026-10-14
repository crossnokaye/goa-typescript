package tsgen

import (
	"reflect"
	"strings"
	"testing"

	"goa.design/goa/v3/codegen"
	. "goa.design/goa/v3/dsl"
	"goa.design/goa/v3/expr"

	"github.com/crossnokaye/goa-typescript/plugin/typescript/internal/tsgen/testdata"
	tsruntime "github.com/crossnokaye/goa-typescript/runtime"
)

// service returns the named service of a package description.
func service(t *testing.T, pkg *PackageSpec, name string) *ServiceSpec {
	t.Helper()
	for _, s := range pkg.Services {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("service %q not found", name)
	return nil
}

// endpoint returns the endpoint generated for the named method.
func endpoint(t *testing.T, svc *ServiceSpec, method string) *EndpointSpec {
	t.Helper()
	for _, e := range svc.Endpoints {
		if e.MethodName == method {
			return e
		}
	}
	t.Fatalf("endpoint %q not found in service %q", method, svc.Name)
	return nil
}

func typeNames(pkg *PackageSpec) []string {
	names := make([]string, len(pkg.Types))
	for i, ts := range pkg.Types {
		names[i] = ts.Name
	}
	return names
}

func wantEqual(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got %#v\nwant %#v", what, got, want)
	}
}

func TestBuildPackageMetadata(t *testing.T) {
	pkg := build(t, testdata.SSEStringDSL)
	wantEqual(t, "name", pkg.Name, "test-api-client")
	wantEqual(t, "goa version", pkg.GoaVersion, "v3.34.0")
	if len(pkg.Runtime) != len(tsruntime.Sorted()) {
		t.Errorf("runtime: got %d files, want %d", len(pkg.Runtime), len(tsruntime.Sorted()))
	}

	root := codegen.RunDSL(t, func() {
		API("fancy_api", func() {
			Title("Fancy")
			Description("A fancy\n  API.")
			Version("1.2.3")
			Meta("typescript:package", "@repo/api")
		})
	})
	pkg, err := Build([]*expr.RootExpr{root}, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	wantEqual(t, "name", pkg.Name, "@repo/api")
	wantEqual(t, "version", pkg.Version, "1.2.3")
	wantEqual(t, "description", pkg.Description, "A fancy API.")
	if len(pkg.Services) != 0 || len(pkg.Types) != 0 {
		t.Errorf("expected an empty package, got %d services and %d types", len(pkg.Services), len(pkg.Types))
	}
}

func TestBuildIgnoresRootsWithoutHTTP(t *testing.T) {
	root := design(t, testdata.SSEStringDSL)
	pkg, err := Build([]*expr.RootExpr{nil, {}, root, root}, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(pkg.Services) != 1 {
		t.Errorf("a service defined by two roots must be generated once, got %d", len(pkg.Services))
	}
}

func TestBuildSSEWholeEvent(t *testing.T) {
	pkg := build(t, testdata.SSEWholeEventDSL)
	svc := service(t, pkg, "svc")
	if !svc.HasSSE || svc.HasREST || svc.HasWS {
		t.Errorf("flags: SSE=%v REST=%v WS=%v", svc.HasSSE, svc.HasREST, svc.HasWS)
	}
	ep := endpoint(t, svc, "stream_things")
	wantEqual(t, "func", ep.FuncName, "streamThings")
	wantEqual(t, "file", ep.FileName, "stream-things")
	wantEqual(t, "kind", ep.Kind, EndpointSSE)
	wantEqual(t, "verb", ep.Verb, "GET")
	wantEqual(t, "path", ep.PathTemplate, "/api/things/{id}/events")
	wantEqual(t, "accept", ep.Accept, "text/event-stream")
	wantEqual(t, "result", ep.ResultTS, "EventStream<Event>")
	wantEqual(t, "mixed", ep.Mixed, false)

	req := ep.Request
	if req == nil {
		t.Fatal("expected a request")
	}
	wantEqual(t, "request type", req.TypeName, "StreamThingsPayload")
	wantEqual(t, "scalar", req.Scalar, false)
	wantEqual(t, "path params", req.Path, []ParamSpec{{Attr: "id", Prop: "id", Wire: "id", Required: true, Shape: "scalar"}})
	wantEqual(t, "query params", req.Query, []ParamSpec{
		{Attr: "delay_ms", Prop: "delay_ms", Wire: "delay_ms", Shape: "scalar"},
		{Attr: "kinds", Prop: "kinds", Wire: "kind", Shape: "array"},
	})
	wantEqual(t, "headers", req.Headers, []ParamSpec{{Attr: "trace", Prop: "trace", Wire: "X-Trace-Id", Shape: "scalar"}})
	wantEqual(t, "body", req.Body, BodySpec{Kind: BodyNone})
	// A top-level default makes the attribute optional: the server fills it.
	if !strings.Contains(req.TSDef, "delay_ms?: number | undefined;") {
		t.Errorf("delay_ms should be optional in:\n%s", req.TSDef)
	}
	if !strings.Contains(req.Zod, "delay_ms: z.int().optional(),") {
		t.Errorf("delay_ms should be optional without a default in:\n%s", req.Zod)
	}

	wantEqual(t, "sse", *ep.SSE, SSESpec{EventTypeName: "Event", DataEncoding: "json"})
	wantEqual(t, "errors", ep.Errors, []*ErrorSpec{
		{Name: "not_found", Status: 404, Default: true},
		{Name: "bad_upstream", Status: 502, Default: true, Fault: true},
	})
	wantEqual(t, "error type", ep.ErrorTypeName, "StreamThingsError")
	wantEqual(t, "security", len(ep.Security), 0)

	// Inside a named type a default is applied when decoding.
	wantEqual(t, "types", typeNames(pkg), []string{"Event"})
	event := pkg.Types[0]
	wantEqual(t, "event kind", event.Kind, TypeObject)
	if !strings.Contains(event.Zod, `type: z.string().default("thing"),`) || !strings.Contains(event.TSDef, "  type: string;") {
		t.Errorf("type should be required with a decode default:\n%s\n%s", event.TSDef, event.Zod)
	}
}

func TestBuildSSEDataField(t *testing.T) {
	pkg := build(t, testdata.SSEDataFieldDSL)
	svc := service(t, pkg, "svc")

	wrapped := endpoint(t, svc, "stream_wrapped")
	wantEqual(t, "wrapped sse", *wrapped.SSE, SSESpec{
		EventTypeName: "Envelope", DataField: "data", DataEncoding: "json",
		IDField: "id", EventField: "event", RetryField: "retry", RequestIDField: "last_event_id",
	})
	// The Last-Event-ID attribute is supplied through options.lastEventId.
	if strings.Contains(wrapped.Request.TSDef, "last_event_id") {
		t.Errorf("last_event_id must not be a request property:\n%s", wrapped.Request.TSDef)
	}
	wantEqual(t, "wrapped headers", len(wrapped.Request.Headers), 0)
	wantEqual(t, "wrapped query", wrapped.Request.Query, []ParamSpec{{Attr: "count", Prop: "count", Wire: "count", Shape: "scalar"}})

	// SSE fields refer to attributes by their JSON key.
	renamed := endpoint(t, svc, "stream_renamed_id")
	wantEqual(t, "renamed sse", *renamed.SSE, SSESpec{EventTypeName: "Envelope", DataEncoding: "json", IDField: "eventId"})
	if renamed.Request != nil {
		t.Errorf("a method without payload takes no request, got %+v", renamed.Request)
	}

	counter := endpoint(t, svc, "stream_counter")
	wantEqual(t, "counter event", counter.SSE.EventTypeName, "StreamCounterEvent")
	wantEqual(t, "counter inline", counter.SSE.EventInline, true)
	wantEqual(t, "counter data", counter.SSE.DataField, "count")
	wantEqual(t, "counter encoding", counter.SSE.DataEncoding, "int")
	wantEqual(t, "counter id", counter.SSE.IDField, "id")
}

func TestBuildSSEPostBody(t *testing.T) {
	svc := service(t, build(t, testdata.SSEPostBodyDSL), "svc")

	filtered := endpoint(t, svc, "stream_filtered")
	wantEqual(t, "filtered verb", filtered.Verb, "POST")
	wantEqual(t, "filtered body", filtered.Request.Body, BodySpec{Kind: BodyAttr, Attr: "filter"})
	wantEqual(t, "filtered path", filtered.Request.Path, []ParamSpec{{Attr: "tenant", Prop: "tenant", Wire: "tenant", Required: true, Shape: "scalar"}})
	// Filter has a defaulted attribute, so the request refers to its input side.
	wantEqual(t, "filtered type imports", filtered.Imports.Types, []string{"FilterRequest"})
	wantEqual(t, "filtered body type", filtered.Request.Body, BodySpec{Kind: BodyAttr, Attr: "filter"})
	if !strings.Contains(filtered.Request.TSDef, "filter: FilterRequest;") {
		t.Errorf("request should take FilterRequest:\n%s", filtered.Request.TSDef)
	}

	fields := endpoint(t, svc, "stream_fields")
	// Body properties are named by their JSON key; parameters keep the attribute name.
	wantEqual(t, "fields body", fields.Request.Body, BodySpec{Kind: BodyFields, Fields: []string{"name", "legacyName", "since", "blob"}})
	for _, want := range []string{
		"legacyName?: string | undefined;",
		"blob?: Uint8Array | undefined;",
		"cursor?: string | undefined;", // bytes travel as text in a query string
		"page?: number | undefined;",
	} {
		if !strings.Contains(fields.Request.TSDef, want) {
			t.Errorf("request type lacks %q:\n%s", want, fields.Request.TSDef)
		}
	}
	if !strings.Contains(fields.Request.Zod, "legacyName: z.string().optional(),") {
		t.Errorf("a request never sends null for an unset attribute:\n%s", fields.Request.Zod)
	}

	// A named payload is flattened like an inline one.
	named := endpoint(t, svc, "stream_named")
	wantEqual(t, "named body", named.Request.Body, BodySpec{Kind: BodyFields, Fields: []string{"kinds", "since", "limit"}})
	if !strings.Contains(named.Request.TSDef, "limit?: number | undefined;") {
		t.Errorf("limit has a default and should be optional:\n%s", named.Request.TSDef)
	}

	list := endpoint(t, svc, "stream_list")
	wantEqual(t, "list scalar", list.Request.Scalar, true)
	wantEqual(t, "list body", list.Request.Body, BodySpec{Kind: BodyWhole})
	wantEqual(t, "list type", list.Request.TSDef, "string[]")
	wantEqual(t, "list schema", list.Request.Zod, "z.array(z.string())")
}

func TestBuildSSEPrimitives(t *testing.T) {
	text := service(t, build(t, testdata.SSEStringDSL), "svc")
	wantEqual(t, "text encoding", endpoint(t, text, "stream_text").SSE.DataEncoding, "string")
	wantEqual(t, "text event", endpoint(t, text, "stream_text").SSE.EventTSDef, "string")

	one := endpoint(t, text, "stream_one")
	wantEqual(t, "one scalar", one.Request.Scalar, true)
	wantEqual(t, "one type", one.Request.TSDef, "string")
	wantEqual(t, "one path", one.Request.Path, []ParamSpec{{Attr: "id", Prop: "", Wire: "id", Required: true, Shape: "scalar"}})
	wantEqual(t, "one body", one.Request.Body, BodySpec{Kind: BodyNone})

	nums := service(t, build(t, testdata.SSEIntDSL), "svc")
	for method, want := range map[string]string{
		"stream_count": "int", "stream_ratio": "float", "stream_flag": "boolean", "stream_blob": "bytes",
	} {
		wantEqual(t, method, endpoint(t, nums, method).SSE.DataEncoding, want)
	}
}

func TestBuildSecurity(t *testing.T) {
	svc := service(t, build(t, testdata.SecurityDSL), "svc")
	jwt := SecurityBinding{Scheme: "jwt", Kind: "bearer", In: "header", Name: "Authorization", Attr: "token"}

	ep := endpoint(t, svc, "stream_jwt")
	wantEqual(t, "jwt", ep.Security, []SecurityBinding{jwt})
	if strings.Contains(ep.Request.TSDef, "token") {
		t.Errorf("the token must be hoisted out of the request:\n%s", ep.Request.TSDef)
	}
	wantEqual(t, "jwt headers", len(ep.Request.Headers), 0)
	wantEqual(t, "jwt errors", ep.Errors, []*ErrorSpec{{Name: "unauthorized", Status: 401, Default: true}})

	only := endpoint(t, svc, "stream_token_only")
	wantEqual(t, "token only", only.Security, []SecurityBinding{jwt})
	if only.Request != nil {
		t.Errorf("nothing is left of the payload, yet the request is %+v", only.Request)
	}

	public := endpoint(t, svc, "stream_public")
	wantEqual(t, "public", len(public.Security), 0)

	keyed := endpoint(t, svc, "stream_keyed")
	wantEqual(t, "keyed", keyed.Security, []SecurityBinding{{Scheme: "api_key", Kind: "apiKey", In: "query", Name: "api_key", Attr: "key"}})
	wantEqual(t, "keyed query", keyed.Request.Query, []ParamSpec{{Attr: "id", Prop: "id", Wire: "id", Shape: "scalar"}})

	header := endpoint(t, svc, "stream_key_header")
	wantEqual(t, "key header", header.Security, []SecurityBinding{{Scheme: "api_key", Kind: "apiKey", In: "header", Name: "X-API-Key", Attr: "key"}})

	basic := endpoint(t, svc, "stream_basic")
	wantEqual(t, "basic", basic.Security, []SecurityBinding{{Scheme: "basic", Kind: "basic", In: "header", Name: "Authorization", Attr: "user"}})
	if strings.Contains(basic.Request.TSDef, "user") || strings.Contains(basic.Request.TSDef, "pass") {
		t.Errorf("basic credentials must be hoisted out of the request:\n%s", basic.Request.TSDef)
	}

	oauth := endpoint(t, svc, "stream_oauth")
	wantEqual(t, "oauth", oauth.Security, []SecurityBinding{{Scheme: "oauth", Kind: "bearer", In: "header", Name: "Authorization", Attr: "access_token"}})

	either := endpoint(t, svc, "stream_either")
	wantEqual(t, "either", either.Security, []SecurityBinding{
		jwt,
		{Scheme: "api_key", Kind: "apiKey", In: "header", Name: "X-API-Key", Attr: "key"},
	})
}

func TestBuildErrors(t *testing.T) {
	pkg := build(t, testdata.ErrorsDSL)
	ep := endpoint(t, service(t, pkg, "svc"), "stream_things")
	wantEqual(t, "errors", ep.Errors, []*ErrorSpec{
		{Name: "precondition_failed", Status: 400, Default: true},
		{Name: "gone", Status: 400, Default: true},
		{Name: "not_found", Status: 404, Default: true},
		{Name: "invalid", Status: 422, TypeName: "Problem"},
		{Name: "busy", Status: 503, Default: true, Temporary: true, Timeout: true},
	})
	wantEqual(t, "table", ep.ErrorTable, `{
  400: [{ name: "precondition_failed" }, { name: "gone" }],
  404: [{ name: "not_found" }],
  422: [{ name: "invalid", schema: Problem }],
  503: [{ name: "busy" }],
} as const`)
	for _, member := range []string{
		`GoaError<"precondition_failed">`, `GoaError<"gone">`, `GoaError<"not_found">`,
		`GoaError<"invalid", Problem>`, `GoaError<"busy">`, "GoaError<OtherErrorName>", "ClientError",
	} {
		if !strings.Contains(ep.ErrorTS, "\n  | "+member) {
			t.Errorf("error union lacks %s:%s", member, ep.ErrorTS)
		}
	}
	// goa's own error type is modelled by the runtime, not hoisted.
	wantEqual(t, "types", typeNames(pkg), []string{"Problem"})
	wantEqual(t, "type imports", ep.Imports.Types, []string{"Problem"})
}

func TestBuildTwoServices(t *testing.T) {
	pkg := build(t, testdata.TwoServicesDSL)
	if len(pkg.Services) != 2 {
		t.Fatalf("got %d services, want echo and toy", len(pkg.Services))
	}
	echo, toy := pkg.Services[0], pkg.Services[1]
	wantEqual(t, "order", []string{echo.Name, toy.Name}, []string{"echo", "toy"})
	wantEqual(t, "dirs", []string{echo.Dir, toy.Dir}, []string{"echo", "toy"})
	wantEqual(t, "namespaces", []string{echo.Namespace, toy.Namespace}, []string{"echo", "toy"})

	// Both services declare the same names, each in its own module.
	a, b := endpoint(t, echo, "stream_things"), endpoint(t, toy, "stream_things")
	wantEqual(t, "func names", []string{a.FuncName, b.FuncName}, []string{"streamThings", "streamThings"})
	wantEqual(t, "request names", []string{a.Request.TypeName, b.Request.TypeName}, []string{"StreamThingsPayload", "StreamThingsPayload"})
	wantEqual(t, "event names", []string{a.SSE.EventTypeName, b.SSE.EventTypeName}, []string{"Thing", "Thing"})

	// Types are shared; the opted-out service contributes none.
	wantEqual(t, "types", typeNames(pkg), []string{"Thing"})

	wantEqual(t, "echo skipped", echo.Skipped, []SkippedEndpoint{{Method: "chat", Reason: "WebSocket endpoints are not generated yet"}})
	wantEqual(t, "toy skipped", len(toy.Skipped), 0)
	wantEqual(t, "toy get_thing", endpoint(t, toy, "get_thing").Kind, EndpointREST)
	if !echo.HasWS || !echo.HasSSE || !toy.HasREST || !toy.HasSSE {
		t.Errorf("flags: echo=%+v toy=%+v", echo, toy)
	}
}

func TestBuildNames(t *testing.T) {
	pkg := build(t, testdata.NamesDSL)
	wantEqual(t, "types", typeNames(pkg), []string{"Date2", "Result2", "Thing", "Thing2", "WatchPayload"})

	def := service(t, pkg, "default")
	wantEqual(t, "default namespace", def.Namespace, "default_")
	wantEqual(t, "default dir", def.Dir, "default")
	watch := endpoint(t, def, "watch")
	wantEqual(t, "watch request", watch.Request.TypeName, "WatchPayload2")
	wantEqual(t, "watch event", watch.SSE.EventTypeName, "WatchEvent")
	del := endpoint(t, def, "delete")
	wantEqual(t, "delete func", del.FuncName, "delete_")
	wantEqual(t, "delete file", del.FileName, "delete")
	wantEqual(t, "delete event", del.SSE.EventTypeName, "Delete_Event")

	send := service(t, pkg, "send")
	wantEqual(t, "send namespace", send.Namespace, "send2")
	first, second := endpoint(t, send, "stream_things"), endpoint(t, send, "streamThings")
	wantEqual(t, "funcs", []string{first.FuncName, second.FuncName}, []string{"streamThings", "streamThings2"})
	wantEqual(t, "files", []string{first.FileName, second.FileName}, []string{"stream-things", "stream-things2"})
	wantEqual(t, "events", []string{first.SSE.EventTypeName, second.SSE.EventTypeName}, []string{"StreamThingsEvent", "StreamThings2Event"})
}

func TestBuildTypeOrder(t *testing.T) {
	pkg := build(t, testdata.NestedDSL)
	// Dependencies first, ties by name; members of a cycle are adjacent.
	wantEqual(t, "order", typeNames(pkg), []string{"Leaf", "Tree", "Ping", "Pong", "Forest"})
	recursive := map[string]bool{}
	for _, ts := range pkg.Types {
		recursive[ts.Name] = ts.Recursive
	}
	wantEqual(t, "recursive", recursive, map[string]bool{"Leaf": false, "Tree": true, "Ping": true, "Pong": true, "Forest": false})
}

func TestBuildViews(t *testing.T) {
	pkg := build(t, testdata.ViewsDSL)
	var viewed *TypeSpec
	for _, ts := range pkg.Types {
		if ts.Name == "Viewed" {
			viewed = ts
		}
	}
	if viewed == nil {
		t.Fatalf("Viewed not found in %v", typeNames(pkg))
	}
	optional := map[string]bool{}
	for _, p := range viewed.Props {
		optional[p.Key] = p.Optional
	}
	// An attribute missing from any view is optional, even when required or defaulted.
	wantEqual(t, "optional", optional, map[string]bool{"id": false, "name": false, "secret": true, "level": true, "owner": true})
}

func TestBuildJSONTags(t *testing.T) {
	pkg := build(t, testdata.JSONTagDSL)
	tagged := pkg.Types[0]
	keys := make([]string, len(tagged.Props))
	for i, p := range tagged.Props {
		keys[i] = p.Attr + "=" + p.Key
	}
	wantEqual(t, "keys", keys, []string{
		"plain_name=plain_name", "legacy_name=legacyName", "omit_empty=omitEmpty",
		"required_tagged=requiredTagged", "quoted=needs-quotes", "go_only=go_only",
	})
	// A tag override drops goa's omitempty, so an unset value arrives as null.
	for _, want := range []string{
		"legacyName: nullAsAbsent(z.string()),",
		"omitEmpty: z.string().optional(),",
		"requiredTagged: z.string(),",
	} {
		if !strings.Contains(tagged.Zod, want) {
			t.Errorf("schema lacks %q:\n%s", want, tagged.Zod)
		}
	}
	wantEqual(t, "runtime imports", pkg.TypeImports.Runtime, []string{"nullAsAbsent"})
}

func TestBuildSkipsUnsupportedAndOptedOut(t *testing.T) {
	pkg := build(t, func() {
		Service("svc", func() {
			Method("upload", func() {
				Payload(func() { Attribute("name", String) })
				StreamingResult(String)
				HTTP(func() {
					POST("/upload")
					MultipartRequest()
					ServerSentEvents()
				})
			})
			Method("opted_out", func() {
				Meta("typescript:generate", "false")
				StreamingResult(String)
				HTTP(func() {
					GET("/opted-out")
					ServerSentEvents()
				})
			})
			Method("ok", func() {
				StreamingResult(String)
				HTTP(func() {
					GET("/ok")
					ServerSentEvents()
				})
			})
		})
	})
	svc := service(t, pkg, "svc")
	wantEqual(t, "skipped", svc.Skipped, []SkippedEndpoint{
		{Method: "upload", Reason: "multipart requests are not supported"},
	})
	if len(svc.Endpoints) != 1 || svc.Endpoints[0].MethodName != "ok" {
		t.Errorf("expected only the ok endpoint, got %d", len(svc.Endpoints))
	}
}

func TestDebugFile(t *testing.T) {
	pkg := build(t, testdata.SSEWholeEventDSL)
	dir := t.TempDir()
	f := DebugFile(pkg)
	wantEqual(t, "path", f.Path, "gen/typescript_debug.json")
	if _, err := f.Render(dir); err != nil {
		t.Fatalf("render: %v", err)
	}
}
