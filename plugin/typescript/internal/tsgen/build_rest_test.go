package tsgen

import (
	"strings"
	"testing"

	. "goa.design/goa/v3/dsl"

	"github.com/crossnokaye/goa-typescript/plugin/typescript/internal/tsgen/testdata"
)

// response returns the response an endpoint declares for a status.
func response(t *testing.T, ep *EndpointSpec, status int) *ResponseSpec {
	t.Helper()
	for _, r := range ep.Responses {
		if r.Status == status {
			return r
		}
	}
	t.Fatalf("endpoint %q declares no %d response", ep.MethodName, status)
	return nil
}

func statuses(ep *EndpointSpec) []int {
	out := make([]int, len(ep.Responses))
	for i, r := range ep.Responses {
		out[i] = r.Status
	}
	return out
}

func TestBuildRESTBasic(t *testing.T) {
	pkg := build(t, testdata.RESTBasicDSL)
	svc := service(t, pkg, "svc")
	if !svc.HasREST || svc.HasSSE || svc.HasWS {
		t.Errorf("flags: SSE=%v REST=%v WS=%v", svc.HasSSE, svc.HasREST, svc.HasWS)
	}
	wantEqual(t, "skipped", len(svc.Skipped), 0)
	wantEqual(t, "warnings", len(pkg.Warnings), 0)

	get := endpoint(t, svc, "get_thing")
	wantEqual(t, "func", get.FuncName, "getThing")
	wantEqual(t, "file", get.FileName, "get-thing")
	wantEqual(t, "kind", get.Kind, EndpointREST)
	wantEqual(t, "verb", get.Verb, "GET")
	wantEqual(t, "path", get.PathTemplate, "/api/things/{id}")
	wantEqual(t, "accept", get.Accept, "application/json")
	wantEqual(t, "result", get.ResultTS, "Thing")
	wantEqual(t, "result spec", *get.Result, ResultSpec{TypeName: "Thing"})
	wantEqual(t, "responses", get.Responses, []*ResponseSpec{{Status: 200, Body: BodyFields}})
	wantEqual(t, "responses name", get.ResponsesName, "getThingResponses")
	wantEqual(t, "request", get.Request.TypeName, "GetThingPayload")
	wantEqual(t, "headers", get.Request.Headers, []ParamSpec{{Attr: "trace", Prop: "trace", Wire: "X-Trace-Id", Shape: "scalar"}})
	wantEqual(t, "errors", get.Errors, []*ErrorSpec{{Name: "not_found", Status: 404, Default: true}})
	wantEqual(t, "error type", get.ErrorTypeName, "GetThingError")
	if get.SSE != nil || get.Stream != nil || get.Mixed {
		t.Errorf("a unary endpoint has no stream: %+v", get)
	}
	for _, name := range []string{"type CallResult", "type RequestOptions", "decodeResponse", "encode", "send"} {
		if !contains(get.Imports.Runtime, name) {
			t.Errorf("runtime imports lack %q: %v", name, get.Imports.Runtime)
		}
	}
	for _, name := range []string{"EventStream", "type StreamOptions", "type Result", "ok", "err"} {
		if contains(get.Imports.Runtime, name) {
			t.Errorf("runtime imports should not have %q: %v", name, get.Imports.Runtime)
		}
	}

	create := endpoint(t, svc, "create_thing")
	wantEqual(t, "create statuses", statuses(create), []int{201})
	wantEqual(t, "create body", create.Request.Body, BodySpec{Kind: BodyFields, Fields: []string{"id", "name", "created_at"}})

	stats := endpoint(t, svc, "stats")
	wantEqual(t, "stats result", stats.ResultTS, "StatsResult")
	wantEqual(t, "stats inline", stats.Result.Inline, true)
	if !strings.Contains(stats.Result.TSDef, "newest?: Date | undefined;") || !strings.Contains(stats.Result.Zod, "count: z.int(),") {
		t.Errorf("inline result:\n%s\n%s", stats.Result.TSDef, stats.Result.Zod)
	}
	if stats.Request != nil {
		t.Errorf("stats takes no payload, got %+v", stats.Request)
	}

	del := endpoint(t, svc, "delete_thing")
	wantEqual(t, "delete result", del.ResultTS, "void")
	wantEqual(t, "delete result spec", *del.Result, ResultSpec{})
	wantEqual(t, "delete responses", del.Responses, []*ResponseSpec{{Status: 204, Body: BodyNone}})

	ping := endpoint(t, svc, "ping")
	wantEqual(t, "ping result", ping.ResultTS, "void")
	if ping.Request != nil || contains(ping.Imports.Runtime, "encode") {
		t.Errorf("ping takes no payload: %+v", ping.Request)
	}
}

func TestBuildRESTScalars(t *testing.T) {
	pkg := build(t, testdata.RESTScalarsDSL)
	svc := service(t, pkg, "svc")
	// CollectionOf(Item) is Item[], not a declaration named ItemCollection.
	wantEqual(t, "types", typeNames(pkg), []string{"Item", "Shelf"})

	for method, want := range map[string]struct {
		ts, zod string
		resp    ResponseSpec
	}{
		"count":      {"number", "z.int()", ResponseSpec{Status: 200, Body: BodyWhole, Scalar: true}},
		"blob":       {"Uint8Array", "bytes()", ResponseSpec{Status: 200, Body: BodyWhole, Scalar: true}},
		"names":      {"string[]", "z.array(z.string())", ResponseSpec{Status: 200, Body: BodyWhole, Scalar: true, NullAs: "array"}},
		"when":       {"Record<string, Date>", "z.record(z.string(), dateTime())", ResponseSpec{Status: 200, Body: BodyWhole, Scalar: true, NullAs: "map"}},
		"list_items": {"Item[]", "z.array(Item)", ResponseSpec{Status: 200, Body: BodyWhole, Scalar: true, NullAs: "array"}},
	} {
		ep := endpoint(t, svc, method)
		wantEqual(t, method+" inline", ep.Result.Inline, true)
		wantEqual(t, method+" type", ep.Result.TSDef, want.ts)
		wantEqual(t, method+" schema", ep.Result.Zod, want.zod)
		wantEqual(t, method+" response", *response(t, ep, 200), want.resp)
	}
	wantEqual(t, "list result name", endpoint(t, svc, "list_items").ResultTS, "ListItemsResult")

	item := endpoint(t, svc, "get_item")
	wantEqual(t, "item result", *item.Result, ResultSpec{TypeName: "Item"})
	// secret is missing from the default view, so the schema does not require it.
	wantEqual(t, "item response", *response(t, item, 200), ResponseSpec{Status: 200, Body: BodyFields})

	// The collection is inlined wherever it appears.
	var shelf *TypeSpec
	for _, ts := range pkg.Types {
		if ts.Name == "Shelf" {
			shelf = ts
		}
	}
	if !strings.Contains(shelf.TSDef, "items?: Item[] | undefined;") || !strings.Contains(shelf.Zod, "items: z.array(Item).optional(),") {
		t.Errorf("Shelf should hold Item[]:\n%s\n%s", shelf.TSDef, shelf.Zod)
	}
}

func TestBuildRESTHeaders(t *testing.T) {
	pkg := build(t, testdata.RESTHeadersDSL)
	svc := service(t, pkg, "svc")

	page := response(t, endpoint(t, svc, "get_page"), 200)
	wantEqual(t, "page body", page.Body, BodyFields)
	wantEqual(t, "page headers", page.Headers, []HeaderSpec{
		{Attr: "etag", Key: "etag", Name: "ETag", Shape: "string"},
		{Attr: "total", Key: "total", Name: "X-Total", Shape: "number"},
		{Attr: "ratio", Key: "ratio", Name: "X-Ratio", Shape: "number"},
		{Attr: "more", Key: "more", Name: "X-More", Shape: "boolean"},
		{Attr: "expires", Key: "expires", Name: "Expires-At", Shape: "string"},
		{Attr: "version", Key: "version", Name: "X-Version", Shape: "number"},
		{Attr: "tags", Key: "tags", Name: "X-Tags", Shape: "string[]"},
		{Attr: "counts", Key: "counts", Name: "X-Counts", Shape: "number[]"},
		{Attr: "flags", Key: "flags", Name: "X-Flags", Shape: "boolean[]"},
		// The attribute is found under its JSON key in the decoded value.
		{Attr: "legacy_name", Key: "legacyName", Name: "X-Legacy-Name", Shape: "string"},
	})
	wantEqual(t, "page cookies", page.Cookies, []string{"session"})
	wantEqual(t, "page missing", len(page.Missing), 0)

	head := endpoint(t, svc, "head_page")
	wantEqual(t, "head verb", head.Verb, "HEAD")
	wantEqual(t, "head response", *response(t, head, 200), ResponseSpec{
		Status: 200, Body: BodyNone,
		Headers: []HeaderSpec{
			{Attr: "etag", Key: "etag", Name: "ETag", Shape: "string"},
			{Attr: "total", Key: "total", Name: "X-Total", Shape: "number"},
		},
	})

	count := endpoint(t, svc, "count_things")
	wantEqual(t, "count result", count.Result.TSDef, "number")
	wantEqual(t, "count response", *response(t, count, 200), ResponseSpec{
		Status: 200, Body: BodyNone, Scalar: true,
		Headers: []HeaderSpec{{Attr: "X-Count", Key: "", Name: "X-Count", Shape: "number"}},
	})
	wantEqual(t, "count table", count.ResponsesTS, `{
  200: { body: 'none', scalar: true, headers: { '': { name: 'X-Count', shape: 'number' } } },
} as const`)

	wantEqual(t, "skipped", svc.Skipped, []SkippedEndpoint{{
		Method: "signature",
		Reason: `response header "X-Signature" has type bytes, which cannot be read from a header`,
	}})
}

func TestBuildRESTStatuses(t *testing.T) {
	pkg := build(t, testdata.RESTStatusesDSL)
	svc := service(t, pkg, "svc")

	upsert := endpoint(t, svc, "upsert_thing")
	wantEqual(t, "upsert result", upsert.ResultTS, "Upserted")
	wantEqual(t, "upsert responses", upsert.Responses, []*ResponseSpec{
		{Status: 200, Body: BodyFields},
		{Status: 201, Body: BodyFields, Tag: [2]string{"outcome", "created"}},
		// id is required and a 202 has no body to carry it.
		{Status: 202, Body: BodyNone, Tag: [2]string{"outcome", "accepted"}, Missing: []string{"id"}},
	})
	wantEqual(t, "upsert table", upsert.ResponsesTS, `{
  200: { body: 'fields' },
  201: { body: 'fields', tag: ['outcome', 'created'] },
  202: { body: 'none', tag: ['outcome', 'accepted'] },
} as const`)

	// gaia's shape: one result type decodes both responses.
	find := endpoint(t, svc, "find_diagram")
	wantEqual(t, "find responses", find.Responses, []*ResponseSpec{
		{Status: 200, Body: BodyAttr, Attr: "diagram", Tag: [2]string{"found", "yes"}},
		{Status: 204, Body: BodyNone},
	})

	strict := endpoint(t, svc, "strict")
	wantEqual(t, "strict missing", response(t, strict, 204).Missing, []string{"id"})
	wantEqual(t, "warnings", pkg.Warnings, []string{
		`service "svc", method "upsert_thing": the 202 response does not carry the required result attribute(s) id; ` +
			`the generated client rejects that response with a validation error`,
		`service "svc", method "strict": the 204 response does not carry the required result attribute(s) id; ` +
			`the generated client rejects that response with a validation error`,
	})
	if doc := restDoc(strict); !strings.Contains(doc, "A 204 response does not carry `id`") ||
		!strings.Contains(doc, "Succeeds with 200 or 204 (`state` is 'gone')") {
		t.Errorf("the function's documentation should name the gap:\n%s", doc)
	}
}

func TestBuildRESTErrors(t *testing.T) {
	pkg := build(t, testdata.RESTErrorsDSL)
	ep := endpoint(t, service(t, pkg, "svc"), "create_thing")
	wantEqual(t, "types", typeNames(pkg), []string{"ValidationProblem"})
	wantEqual(t, "errors", len(ep.Errors), 4)

	conflict, teapot, invalid, busy := ep.Errors[0], ep.Errors[1], ep.Errors[2], ep.Errors[3]
	wantEqual(t, "conflict name", conflict.TypeName, "CreateThingConflictDetail")
	wantEqual(t, "conflict inline", conflict.Inline, true)
	if !strings.Contains(conflict.TSDef, "existing_id: string;") || !strings.Contains(conflict.Zod, "at: dateTime().optional(),") {
		t.Errorf("conflict detail:\n%s\n%s", conflict.TSDef, conflict.Zod)
	}
	wantEqual(t, "teapot", []string{teapot.TypeName, teapot.TSDef, teapot.Zod}, []string{"CreateThingTeapotDetail", "string", "z.string()"})
	wantEqual(t, "invalid", *invalid, ErrorSpec{
		Name: "invalid", Status: 422, TypeName: "ValidationProblem",
		Headers: []HeaderSpec{{Attr: "retry_after", Key: "retry_after", Name: "Retry-After", Shape: "number"}},
	})
	wantEqual(t, "busy", *busy, ErrorSpec{Name: "busy", Status: 503, Default: true})

	wantEqual(t, "table", ep.ErrorTable, `{
  409: [{ name: 'conflict', schema: CreateThingConflictDetail }],
  418: [{ name: 'teapot', schema: CreateThingTeapotDetail }],
  422: [{ name: 'invalid', schema: ValidationProblem, headers: { retry_after: { name: 'Retry-After', shape: 'number' } } }],
  503: [{ name: 'busy' }],
} as const`)
	for _, member := range []string{
		`GoaError<'conflict', CreateThingConflictDetail>`, `GoaError<'teapot', CreateThingTeapotDetail>`,
		`GoaError<'invalid', ValidationProblem>`, `GoaError<'busy'>`,
	} {
		if !strings.Contains(ep.ErrorTS, "\n  | "+member) {
			t.Errorf("error union lacks %s:%s", member, ep.ErrorTS)
		}
	}
}

func TestBuildRESTRaw(t *testing.T) {
	svc := service(t, build(t, testdata.RESTRawDSL), "svc")

	download := endpoint(t, svc, "download")
	wantEqual(t, "download response", *response(t, download, 200), ResponseSpec{
		Status: 200, ContentType: "application/zip", Body: BodyRaw,
		Headers: []HeaderSpec{
			{Attr: "content_length", Key: "content_length", Name: "Content-Length", Shape: "number"},
			{Attr: "content_type", Key: "content_type", Name: "Content-Type", Shape: "string"},
		},
	})
	wantEqual(t, "download raw request", download.RawBody, false)
	if !strings.Contains(restDoc(download), "The response body is not read") {
		t.Errorf("documentation should say the body is left unread:\n%s", restDoc(download))
	}

	plain := endpoint(t, svc, "download_plain")
	wantEqual(t, "plain result", plain.ResultTS, "void")
	wantEqual(t, "plain response", *response(t, plain, 200), ResponseSpec{Status: 200, Body: BodyRaw})

	upload := endpoint(t, svc, "upload")
	wantEqual(t, "upload raw", upload.RawBody, true)
	wantEqual(t, "upload body", upload.Request.Body, BodySpec{Kind: BodyRaw})
	if !strings.Contains(upload.SpecTS, `body: { kind: 'raw' },`) {
		t.Errorf("transport mapping should declare a raw body:\n%s", upload.SpecTS)
	}
	wantEqual(t, "upload result", upload.ResultTS, "UploadResult")

	anonymous := endpoint(t, svc, "upload_anonymous")
	wantEqual(t, "anonymous raw", anonymous.RawBody, true)
	if anonymous.Request != nil {
		t.Errorf("no payload, yet the request is %+v", anonymous.Request)
	}
	if !strings.Contains(anonymous.SpecTS, `body: { kind: 'raw' },`) {
		t.Errorf("transport mapping should declare a raw body:\n%s", anonymous.SpecTS)
	}

	if !strings.Contains(restDoc(anonymous), "pass `undefined` as `payload`") {
		t.Errorf("documentation should explain the placeholder payload:\n%s", restDoc(anonymous))
	}

	// An optional parameter cannot precede a required one, so the payload of
	// a raw-body function is always required, even when every attribute is
	// optional or there is no payload at all.
	tagged := endpoint(t, svc, "upload_tagged")
	wantEqual(t, "tagged raw", tagged.RawBody, true)
	wantEqual(t, "tagged optional", tagged.Request.Optional, false)

	files := renderTo(t, build(t, testdata.RESTRawDSL), t.TempDir())
	for file, want := range map[string]string{
		"src/svc/upload.ts":           "  payload: UploadPayload,\n  body: BodyInit,\n  options?: RequestOptions,\n",
		"src/svc/upload-tagged.ts":    "  payload: UploadTaggedPayload,\n  body: BodyInit,\n  options?: RequestOptions,\n",
		"src/svc/upload-anonymous.ts": "  client: Client,\n  payload: undefined,\n  body: BodyInit,\n  options?: RequestOptions,\n",
	} {
		if !strings.Contains(files[file], want) {
			t.Errorf("%s should take the body after the payload:\n%s", file, files[file])
		}
	}
}

// TestBuildOptionalPayload covers the one call shape every function has,
// `fn(client, payload?, options?)`: the payload is optional exactly when it is
// an object with no required property, and `undefined` when the method has
// none.
func TestBuildOptionalPayload(t *testing.T) {
	basic := service(t, build(t, testdata.RESTBasicDSL), "svc")
	wantEqual(t, "required attribute", endpoint(t, basic, "get_thing").Request.Optional, false)
	wantEqual(t, "required type", endpoint(t, basic, "create_thing").Request.Optional, false)
	if ping := endpoint(t, basic, "ping"); ping.Request != nil {
		t.Errorf("no payload, yet the request is %+v", ping.Request)
	}

	mixed := service(t, build(t, testdata.RESTMixedDSL), "svc")
	list := endpoint(t, mixed, "list_things")
	wantEqual(t, "all optional", list.Request.Optional, true)

	// A scalar payload is the value itself: there is nothing to default it to.
	strings_ := service(t, build(t, testdata.SSEStringDSL), "svc")
	one := endpoint(t, strings_, "stream_one")
	wantEqual(t, "scalar", one.Request.Scalar, true)
	wantEqual(t, "scalar optional", one.Request.Optional, false)

	// Security attributes are stripped before the decision: a payload left
	// with only optional attributes is optional.
	security := service(t, build(t, testdata.SecurityDSL), "svc")
	wantEqual(t, "keyed", endpoint(t, security, "stream_keyed").Request.Optional, true)
	if only := endpoint(t, security, "stream_token_only"); only.Request != nil {
		t.Errorf("nothing is left of the payload, yet the request is %+v", only.Request)
	}

	for name, dsl := range map[string]func(){"basic": testdata.RESTBasicDSL, "mixed": testdata.RESTMixedDSL} {
		files := renderTo(t, build(t, dsl), t.TempDir())
		for file, want := range map[string]string{
			"src/svc/ping.ts":      "  client: Client,\n  payload?: undefined,\n  options?: RequestOptions,\n",
			"src/svc/get-thing.ts": "  client: Client,\n  payload: GetThingPayload,\n  options?: RequestOptions,\n",
			"src/svc/list-things.ts": "  client: Client,\n  payload?: ListThingsPayload,\n  options?: RequestOptions,\n" +
				"): Promise<CallResult<ListThingsResult, ListThingsError>> {\n  const wire = encode(ListThingsPayload, payload ?? {});\n",
		} {
			if content, ok := files[file]; ok && !strings.Contains(content, want) {
				t.Errorf("%s/%s should take the payload second:\n%s", name, file, content)
			}
		}
		if content, ok := files["src/svc/list-things.ts"]; ok && !strings.Contains(content, "  payload?: ListThingsPayload,\n  options?: StreamOptions,\n") {
			t.Errorf("the stream function should take the payload second too:\n%s", content)
		}
	}
}

func TestBuildRESTMixed(t *testing.T) {
	svc := service(t, build(t, testdata.RESTMixedDSL), "svc")
	if !svc.HasREST || !svc.HasSSE {
		t.Errorf("flags: SSE=%v REST=%v", svc.HasSSE, svc.HasREST)
	}
	wantEqual(t, "endpoints", len(svc.Endpoints), 1)
	ep := endpoint(t, svc, "list_things")
	// One file, two functions: the plain one keeps the method name.
	wantEqual(t, "mixed", ep.Mixed, true)
	wantEqual(t, "kind", ep.Kind, EndpointREST)
	wantEqual(t, "file", ep.FileName, "list-things")
	wantEqual(t, "func", ep.FuncName, "listThings")
	wantEqual(t, "accept", ep.Accept, "application/json")
	wantEqual(t, "result", ep.ResultTS, "ListThingsResult")
	wantEqual(t, "result type", ep.Result.TSDef, "Thing[]")
	if ep.Stream == nil || ep.SSE == nil {
		t.Fatalf("expected a stream function, got %+v", ep)
	}
	wantEqual(t, "stream func", ep.Stream.FuncName, "listThingsStream")
	wantEqual(t, "stream spec", ep.Stream.SpecName, "listThingsStreamSpec")
	wantEqual(t, "stream accept", ep.Stream.Accept, "text/event-stream")
	wantEqual(t, "event", ep.SSE.EventTypeName, "Thing")
	// Each function sends its own Accept header; everything else is shared.
	if !strings.Contains(ep.SpecTS, `accept: 'application/json',`) || !strings.Contains(ep.Stream.SpecTS, `accept: 'text/event-stream',`) {
		t.Errorf("transport mappings:\n%s\n%s", ep.SpecTS, ep.Stream.SpecTS)
	}
	wantEqual(t, "same mapping otherwise",
		strings.Replace(ep.Stream.SpecTS, "text/event-stream", "application/json", 1), ep.SpecTS)
	for _, name := range []string{"type RequestOptions", "type StreamOptions", "decodeResponse", "EventStream"} {
		if !contains(ep.Imports.Runtime, name) {
			t.Errorf("runtime imports lack %q: %v", name, ep.Imports.Runtime)
		}
	}
}

// When the plain response of a mixed method cannot be decoded, the stream is
// still generated, under the name it would have had anyway.
func TestBuildMixedStreamOnly(t *testing.T) {
	pkg := build(t, func() {
		Service("svc", func() {
			Method("list", func() {
				Result(String)
				StreamingResult(String)
				HTTP(func() {
					GET("/list")
					ServerSentEvents()
					Response(StatusOK, func() { ContentType("text/plain") })
				})
			})
		})
	})
	svc := service(t, pkg, "svc")
	wantEqual(t, "skipped", svc.Skipped, []SkippedEndpoint{{
		Method: "list",
		Reason: `plain response: the 200 response has content type "text/plain": only JSON responses are decoded`,
	}})
	ep := endpoint(t, svc, "list")
	wantEqual(t, "kind", ep.Kind, EndpointSSE)
	wantEqual(t, "func", ep.FuncName, "listStream")
	wantEqual(t, "event", ep.SSE.EventTypeName, "ListStreamEvent")
	wantEqual(t, "encoding", ep.SSE.DataEncoding, "string")
	if ep.Stream != nil || ep.Result != nil {
		t.Errorf("a stream-only endpoint has no plain function: %+v", ep)
	}
}

func TestBuildRESTSkipped(t *testing.T) {
	svc := service(t, build(t, testdata.RESTSkippedDSL), "svc")
	wantEqual(t, "skipped", svc.Skipped, []SkippedEndpoint{
		{Method: "moved", Reason: "redirect endpoints are not generated: fetch follows the redirect"},
		{Method: "page", Reason: `the 200 response has content type "text/html": only JSON responses are decoded`},
		{Method: "fetch_with_body", Reason: "fetch cannot send a request body with GET"},
	})
	if len(svc.Endpoints) != 1 || svc.Endpoints[0].MethodName != "ok" {
		t.Errorf("expected only the ok endpoint, got %d", len(svc.Endpoints))
	}
}

func TestIsJSONMediaType(t *testing.T) {
	for ct, want := range map[string]bool{
		"application/json":                true,
		"application/json; charset=utf-8": true,
		"application/vnd.thing+json":      true,
		"Application/Problem+JSON":        true,
		"application/vnd.thing":           false,
		"text/html":                       false,
		"application/zip":                 false,
		"application/vnd.goa.error":       false,
		"application/jsonp":               false,
	} {
		wantEqual(t, ct, isJSONMediaType(ct), want)
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
