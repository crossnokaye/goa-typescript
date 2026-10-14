package tsgen

import (
	"testing"

	tsruntime "github.com/crossnokaye/goa-typescript/runtime"
)

func TestIdentifiers(t *testing.T) {
	cases := []struct {
		in                 string
		ident, camel, file string
	}{
		{"stream_things_sse", "StreamThingsSse", "streamThingsSse", "stream-things-sse"},
		{"ThingWithViews", "ThingWithViews", "thingWithViews", "thing-with-views"},
		{"thing", "Thing", "thing", "thing"},
		{"get-thing", "GetThing", "getThing", "get-thing"},
		{"thing_URL", "ThingURL", "thingURL", "thing-url"},
		{"HTTPServer", "HTTPServer", "hTTPServer", "httpserver"},
		{"v2_things", "V2Things", "v2Things", "v2-things"},
		{"getThing2", "GetThing2", "getThing2", "get-thing2"},
		{"application/vnd.thing", "ApplicationVndThing", "applicationVndThing", "application-vnd-thing"},
		{"2fast", "_2fast", "_2fast", "2fast"},
		{"delete", "Delete", "delete_", "delete"},
		{"default", "Default", "default_", "default"},
		{"new", "New", "new_", "new"},
		{"", "_", "_", "_"},
	}
	for _, c := range cases {
		if got := tsIdent(c.in); got != c.ident {
			t.Errorf("tsIdent(%q) = %q, want %q", c.in, got, c.ident)
		}
		if got := camel(c.in); got != c.camel {
			t.Errorf("camel(%q) = %q, want %q", c.in, got, c.camel)
		}
		if got := kebab(c.in); got != c.file {
			t.Errorf("kebab(%q) = %q, want %q", c.in, got, c.file)
		}
	}
}

func TestPropKeyAndString(t *testing.T) {
	keys := map[string]string{
		"id":           "id",
		"snake_case":   "snake_case",
		"$ref":         "$ref",
		"class":        "class",
		"needs-quotes": `"needs-quotes"`,
		"1st":          `"1st"`,
		"with space":   `"with space"`,
		"":             `""`,
	}
	for in, want := range keys {
		if got := tsPropKey(in); got != want {
			t.Errorf("tsPropKey(%q) = %s, want %s", in, got, want)
		}
	}
	strs := map[string]string{
		"plain":        `"plain"`,
		`say "hi"`:     `"say \"hi\""`,
		`back\slash`:   `"back\\slash"`,
		"line\nbreak":  `"line\nbreak"`,
		"<a&b>":        `"<a&b>"`,
		"caf\u00e9":    "\"caf\u00e9\"",
		"sep\u2028end": `"sep\u2028end"`,
	}
	for in, want := range strs {
		if got := tsString(in); got != want {
			t.Errorf("tsString(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestJSDoc(t *testing.T) {
	cases := []struct{ text, indent, want string }{
		{"", "", ""},
		{"  \n ", "  ", ""},
		{"One line.", "", "/** One line. */\n"},
		{"One line.", "  ", "  /** One line. */\n"},
		{"Two\nlines.", "  ", "  /**\n   * Two\n   * lines.\n   */\n"},
		{"With a gap.\n\nAfter.", "", "/**\n * With a gap.\n *\n * After.\n */\n"},
		{"Ends */ early", "", "/** Ends *\\/ early */\n"},
	}
	for _, c := range cases {
		if got := jsdoc(c.text, c.indent); got != c.want {
			t.Errorf("jsdoc(%q, %q) = %q, want %q", c.text, c.indent, got, c.want)
		}
	}
}

func TestScopeResolvesCollisions(t *testing.T) {
	scope := newScope([]string{"Result", "z"}, []string{"Thing"})
	for _, c := range []struct{ in, want string }{
		{"Result", "Result2"},
		{"Thing", "Thing2"},
		{"Thing", "Thing3"},
		{"Other", "Other"},
		{"z", "z2"},
	} {
		if got := scope.Unique(c.in); got != c.want {
			t.Errorf("Unique(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The root barrel re-exports the runtime next to the design types, so every
// runtime export must be known to the generator.
func TestRuntimeExportsAreReserved(t *testing.T) {
	var files []RuntimeFile
	for _, f := range tsruntime.Sorted() {
		files = append(files, RuntimeFile{Name: f.Name, Content: f.Content})
	}
	exports := make(map[string]struct{})
	for _, name := range runtimeExports(files) {
		exports[name] = struct{}{}
	}
	for _, want := range []string{
		"Result", "ok", "err", "unwrap", "unwrapAsync",
		"GoaError", "ClientError", "TransportError", "OtherErrorName", "ErrorResult", "isGoaError", "errorMessage",
		"NetworkError", "AbortError", "TimeoutError", "RequestValidationError", "ResponseValidationError",
		"UnexpectedResponseError",
		"Middleware", "Next", "compose",
		"Client", "ClientOptions", "createClient", "RequestOptions", "StreamOptions", "EndpointSpec", "send",
		"TokenProvider", "SecurityScheme",
		"dateTime", "date", "bytes", "nullAsAbsent", "decode", "decodeJSON", "encode", "encodeJSON",
		"goaErrorDecoder", "ErrorResultSchema",
		"EventStream", "SseFrame", "FrameDecoder", "createParser", "sseDecoder",
	} {
		if _, ok := exports[want]; !ok {
			t.Errorf("runtime export %s was not detected", want)
		}
	}
}
