package tsgen

import (
	"strings"
	"testing"

	. "goa.design/goa/v3/dsl"
	"goa.design/goa/v3/expr"

	"github.com/crossnokaye/goa-typescript/plugin/typescript/internal/tsgen/testdata"
)

// queryKinds summarizes the query exports of a service by method.
func queryKinds(svc *ServiceSpec) map[string][]QueryKind {
	out := make(map[string][]QueryKind)
	for _, ep := range svc.Endpoints {
		for _, q := range ep.Queries {
			out[ep.MethodName] = append(out[ep.MethodName], q.Kind)
		}
	}
	return out
}

// query returns the query export of an endpoint for the named SDK function.
func query(t *testing.T, ep *EndpointSpec, funcName string) *QuerySpec {
	t.Helper()
	for _, q := range ep.Queries {
		if q.FuncName == funcName {
			return q
		}
	}
	t.Fatalf("no query export for %s in %s", funcName, ep.MethodName)
	return nil
}

func TestBuildQueryDisabledByDefault(t *testing.T) {
	pkg := build(t, testdata.RESTBasicDSL)
	if pkg.Query {
		t.Fatal("the layer must be off without Meta(\"typescript:query\")")
	}
	for _, svc := range pkg.Services {
		if len(svc.QueryNone) > 0 {
			t.Errorf("%s: unexpected QueryNone %v", svc.Name, svc.QueryNone)
		}
		for _, ep := range svc.Endpoints {
			if len(ep.Queries) > 0 || ep.ExportSpec {
				t.Errorf("%s: query exports without the API meta", ep.MethodName)
			}
		}
	}
}

func TestBuildQueryClassification(t *testing.T) {
	pkg := build(t, testdata.QueryDSL)
	if !pkg.Query {
		t.Fatal("the layer must be on")
	}
	things := service(t, pkg, "things")
	wantEqual(t, "kinds", queryKinds(things), map[string][]QueryKind{
		"get_thing":      {QueryRead},
		"head_thing":     {QueryRead},
		"list_things":    {QueryRead},
		"search_things":  {QueryRead}, // a POST, by Meta("typescript:query:kind")
		"count_things":   {QueryRead},
		"update_thing":   {QueryMutation},
		"delete_label":   {QueryMutation},
		"archive_thing":  {QueryMutation},
		"touch":          {QueryMutation},
		"upload_content": {QueryMutation},
		"watch_thing":    {QueryStream},
		"ticks":          {QueryStream},
		"feed":           {QueryRead, QueryStream},
	})
	wantEqual(t, "none", things.QueryNone, []SkippedEndpoint{
		{Method: "download_content", Reason: "the response body is not decoded (SkipResponseBodyEncodeDecode)"},
		{Method: "chat", Reason: "the WebSocket method streams a payload"},
		{Method: "ping", Reason: `Meta("typescript:query:kind", "none")`},
	})
	wantEqual(t, "legacy kinds", queryKinds(service(t, pkg, "legacy")), map[string][]QueryKind{
		"fetch_thing": {QueryRead},
	})

	feed := endpoint(t, things, "feed")
	wantEqual(t, "mixed names", []string{feed.Queries[0].Name, feed.Queries[0].KeyName, feed.Queries[1].Name, feed.Queries[1].KeyName},
		[]string{"feedQuery", "feedKey", "feedStreamQuery", "feedStreamKey"})
	wantEqual(t, "mixed specs", []string{feed.Queries[0].SpecName, feed.Queries[1].SpecName}, []string{"feedSpec", "feedStreamSpec"})
	if !feed.ExportSpec || endpoint(t, things, "chat").ExportSpec {
		t.Error("only classified endpoints export their transport mapping")
	}

	upload := query(t, endpoint(t, things, "upload_content"), "uploadContent")
	wantEqual(t, "raw", upload.Raw, true)
	wantEqual(t, "upload names", []string{upload.Name, upload.InvalidatesName}, []string{"uploadContentMutation", "uploadContentInvalidates"})

	wantEqual(t, "required", query(t, endpoint(t, things, "get_thing"), "getThing").Required, true)
	wantEqual(t, "optional", query(t, endpoint(t, things, "list_things"), "listThings").Required, false)
	wantEqual(t, "none", query(t, endpoint(t, things, "count_things"), "countThings").Required, false)
}

func TestBuildQueryInvalidation(t *testing.T) {
	pkg := build(t, testdata.QueryDSL)
	things := service(t, pkg, "things")
	for method, want := range map[string][]string{
		"update_thing":   {"/things"},             // PUT /things/{id}: the trailing parameter goes
		"delete_label":   {"/things/{id}/labels"}, // trailing parameter only
		"archive_thing":  {"/things"},             // three methods named, one prefix
		"touch":          nil,                     // Meta("typescript:invalidates", "")
		"upload_content": {"/things/{id}/content"},
	} {
		ep := endpoint(t, things, method)
		wantEqual(t, method, ep.Queries[0].Invalidates, want)
	}
	get := query(t, endpoint(t, things, "get_thing"), "getThing")
	wantEqual(t, "invalidated by", get.InvalidatedBy, []string{"`updateThingMutation`", "`archiveThingMutation`"})
	fetch := query(t, endpoint(t, service(t, pkg, "legacy"), "fetch_thing"), "fetchThing")
	wantEqual(t, "invalidated across services", fetch.InvalidatedBy,
		[]string{"`things.updateThingMutation`", "`things.archiveThingMutation`"})
	labels := defaultInvalidation("/things/{id}/labels/{label_id}")
	wantEqual(t, "default", labels, "/things/{id}/labels")
	wantEqual(t, "static", defaultInvalidation("/things/{id}/archive"), "/things/{id}/archive")
	wantEqual(t, "prefix", staticPrefix("/things/{id}/labels"), "/things")
}

func TestBuildQueryCollisions(t *testing.T) {
	pkg := build(t, testdata.QueryDSL)
	things := service(t, pkg, "things")
	for _, c := range []struct {
		method, fn string
		want       string
	}{
		{"get_thing", "getThing", ""},         // the GET keeps its clean key
		{"head_thing", "headThing", "method"}, // {thing_id} is the same slot as {id}
		{"feed", "feed", ""},
		{"feed", "feedStream", "stream"}, // the stream of a mixed method shares the GET route
		{"list_things", "listThings", ""},
		{"watch_thing", "watchThing", ""},
	} {
		wantEqual(t, c.fn+" discriminator", query(t, endpoint(t, things, c.method), c.fn).Discriminator, c.want)
	}
	fetch := query(t, endpoint(t, service(t, pkg, "legacy"), "fetch_thing"), "fetchThing")
	wantEqual(t, "a POST in another service", fetch.Discriminator, "method")
	wantEqual(t, "collisions", fetch.Collisions, []string{
		"`GET /things/{id}` (`things.getThingQuery`)",
		"`HEAD /things/{thing_id}` (`things.headThingQuery`)",
	})
	wantEqual(t, "source", fetch.Source, `["things", key, { $method: "POST", fields? }]`)
	feed := endpoint(t, things, "feed")
	wantEqual(t, "stream source", query(t, feed, "feedStream").Source, `["feed", { $stream: true, kind? }]`)
	wantEqual(t, "clean key marks", query(t, feed, "feed").OtherMarks, []string{"`$stream`"})
	wantEqual(t, "get marks", query(t, endpoint(t, things, "get_thing"), "getThing").OtherMarks,
		[]string{"`$method`"})
	wantEqual(t, "get source", query(t, endpoint(t, things, "get_thing"), "getThing").Source,
		`["things", id, { tags?, verbose?, since?, trace? }]`)

	// Without a GET, every member is told apart.
	pkg = build(t, func() {
		API("api", func() { Meta("typescript:query", "tanstack") })
		Service("svc", func() {
			Method("peek", func() {
				Payload(func() {
					Attribute("id", String)
					Required("id")
				})
				HTTP(func() { HEAD("/items/{id}") })
			})
			Method("lookup", func() {
				Meta("typescript:query:kind", "query")
				Payload(func() {
					Attribute("item", String)
					Required("item")
				})
				Result(String)
				HTTP(func() { POST("/items/{item}") })
			})
			Method("other", func() {
				Result(String)
				HTTP(func() { GET("/items/other") })
			})
		})
	})
	svc := service(t, pkg, "svc")
	wantEqual(t, "peek", query(t, endpoint(t, svc, "peek"), "peek").Discriminator, "method")
	wantEqual(t, "lookup", query(t, endpoint(t, svc, "lookup"), "lookup").Discriminator, "method")
	wantEqual(t, "other", query(t, endpoint(t, svc, "other"), "other").Discriminator, "")
}

func TestBuildQueryErrors(t *testing.T) {
	enable := func() { API("api", func() { Meta("typescript:query", "tanstack") }) }
	for name, c := range map[string]struct {
		dsl  func()
		want string
	}{
		"query on a bidirectional WebSocket": {func() {
			enable()
			Service("svc", func() {
				Method("chat", func() {
					Meta("typescript:query:kind", "query")
					StreamingPayload(String)
					StreamingResult(String)
					HTTP(func() { GET("/chat") })
				})
			})
		}, `method "chat": Meta("typescript:query:kind", "query") cannot apply: the WebSocket method streams a payload`},
		"query on a raw download": {func() {
			enable()
			Service("svc", func() {
				Method("download", func() {
					Meta("typescript:query:kind", "query")
					HTTP(func() {
						GET("/download")
						SkipResponseBodyEncodeDecode()
					})
				})
			})
		}, "the response body is not decoded"},
		"query on a raw upload": {func() {
			enable()
			Service("svc", func() {
				Method("upload", func() {
					Meta("typescript:query:kind", "query")
					HTTP(func() {
						POST("/upload")
						SkipRequestBodyEncodeDecode()
					})
				})
			})
		}, "the request body is raw"},
		"mutation on a stream": {func() {
			enable()
			Service("svc", func() {
				Method("watch", func() {
					Meta("typescript:query:kind", "mutation")
					StreamingResult(String)
					HTTP(func() {
						GET("/watch")
						ServerSentEvents()
					})
				})
			})
		}, "the method is a Server-Sent Events stream"},
		"query on a skipped method": {func() {
			enable()
			Service("svc", func() {
				Method("moved", func() {
					Meta("typescript:query:kind", "query")
					HTTP(func() {
						GET("/old")
						Redirect("/new", StatusMovedPermanently)
					})
				})
			})
		}, "no function is generated"},
		"unknown kind": {func() {
			enable()
			Service("svc", func() {
				Method("get", func() {
					Meta("typescript:query:kind", "read")
					HTTP(func() { GET("/get") })
				})
			})
		}, `the value must be "query", "mutation" or "none"`},
		"unknown API value": {func() {
			API("api", func() { Meta("typescript:query", "swr") })
			Service("svc", func() {
				Method("get", func() { HTTP(func() { GET("/get") }) })
			})
		}, `the only supported value is "tanstack"`},
		"invalidates an unknown method": {func() {
			enable()
			Service("svc", func() {
				Method("set", func() {
					Meta("typescript:invalidates", "nope")
					HTTP(func() { POST("/set") })
				})
			})
		}, `names "nope", which is not a method of the service`},
		"invalidates a mutation": {func() {
			enable()
			Service("svc", func() {
				Method("set", func() {
					Meta("typescript:invalidates", "reset")
					HTTP(func() { POST("/set") })
				})
				Method("reset", func() { HTTP(func() { POST("/reset") }) })
			})
		}, `names "reset", which has no query options`},
		"invalidates on a query": {func() {
			enable()
			Service("svc", func() {
				Method("get", func() {
					Meta("typescript:invalidates", "get")
					HTTP(func() { GET("/get") })
				})
			})
		}, "applies to mutations only"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build([]*expr.RootExpr{design(t, c.dsl)}, Options{})
			if err == nil {
				t.Fatalf("Build succeeded, want an error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err, c.want)
			}
		})
	}
}
