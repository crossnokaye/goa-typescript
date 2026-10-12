// Package testdata holds the design fixtures of the tsgen tests: one per
// feature of the generator.
package testdata

import (
	. "goa.design/goa/v3/dsl"
)

// stream declares a service with one SSE method that streams the given type,
// which makes the type (and everything it refers to) reachable.
func stream(result any) {
	Service("svc", func() {
		Method("watch", func() {
			StreamingResult(result)
			HTTP(func() {
				GET("/watch")
				ServerSentEvents()
			})
		})
	})
}

// PrimitivesDSL covers every primitive type.
var PrimitivesDSL = func() {
	var Primitives = Type("Primitives", func() {
		Description("One attribute per primitive type.")
		Attribute("bool", Boolean, "A boolean.")
		Attribute("int", Int)
		Attribute("int32", Int32)
		Attribute("int64", Int64)
		Attribute("uint", UInt)
		Attribute("uint32", UInt32)
		Attribute("uint64", UInt64)
		Attribute("float32", Float32)
		Attribute("float64", Float64)
		Attribute("string", String)
		Attribute("bytes", Bytes)
		Attribute("any", Any)
		Required("bool", "int", "string")
	})
	stream(Primitives)
}

// FormatsDSL covers every string format.
var FormatsDSL = func() {
	var Formats = Type("Formats", func() {
		Attribute("date", String, func() { Format(FormatDate) })
		Attribute("date_time", String, func() { Format(FormatDateTime) })
		Attribute("uuid", String, func() { Format(FormatUUID) })
		Attribute("email", String, func() { Format(FormatEmail) })
		Attribute("hostname", String, func() { Format(FormatHostname) })
		Attribute("ipv4", String, func() { Format(FormatIPv4) })
		Attribute("ipv6", String, func() { Format(FormatIPv6) })
		Attribute("ip", String, func() { Format(FormatIP) })
		Attribute("uri", String, func() { Format(FormatURI) })
		Attribute("mac", String, func() { Format(FormatMAC) })
		Attribute("cidr", String, func() { Format(FormatCIDR) })
		Attribute("regexp", String, func() { Format(FormatRegexp) })
		Attribute("json", String, func() { Format(FormatJSON) })
		Attribute("rfc1123", String, func() { Format(FormatRFC1123) })
		Required("date", "date_time")
	})
	stream(Formats)
}

// ValidationsDSL covers enumerations, defaults and validations.
var ValidationsDSL = func() {
	var Validated = Type("Validated", func() {
		Attribute("kind", String, func() {
			Enum("widget", "gadget")
			Default("widget")
		})
		Attribute("priority", Int, func() { Enum(1, 2, 3) })
		Attribute("enabled", Boolean, func() { Default(true) })
		Attribute("name", String, func() {
			MinLength(1)
			MaxLength(64)
			Pattern(`^[a-z][a-z0-9-]*$`)
		})
		Attribute("re2_only", String, func() { Pattern(`^(?i)[a-z]+\z`) })
		Attribute("count", Int64, func() { Minimum(0) })
		Attribute("ratio", Float64, func() {
			Minimum(0)
			Maximum(1)
		})
		Attribute("exclusive", Float64, func() {
			ExclusiveMinimum(0)
			ExclusiveMaximum(100)
		})
		Attribute("retries", UInt, func() {
			Maximum(10)
			Default(3)
		})
		Attribute("since", String, func() {
			Format(FormatDateTime)
			Default("2026-01-01T00:00:00Z")
		})
		Attribute("tags", ArrayOf(String), func() {
			MinLength(1)
			MaxLength(5)
			Default([]string{"new"})
		})
		Attribute("limits", MapOf(String, Int), func() {
			MinLength(1)
			MaxLength(3)
		})
		Required("name")
	})
	stream(Validated)
}

// OptionalDSL covers required and optional attributes.
var OptionalDSL = func() {
	var Optional = Type("Optional", func() {
		Attribute("required_string", String)
		Attribute("optional_string", String)
		Attribute("required_list", ArrayOf(Int))
		Attribute("optional_list", ArrayOf(Int))
		Attribute("required_time", String, func() { Format(FormatDateTime) })
		Attribute("optional_time", String, func() { Format(FormatDateTime) })
		Required("required_string", "required_list", "required_time")
	})
	stream(Optional)
}

// CollectionsDSL covers arrays, maps and named collections.
var CollectionsDSL = func() {
	var Label = Type("Label", func() {
		Attribute("key", String)
		Attribute("value", String)
		Required("key", "value")
	})
	var Labels = Type("Labels", ArrayOf(Label))
	var Collections = Type("Collections", func() {
		Attribute("strings", ArrayOf(String))
		Attribute("kinds", ArrayOf(String, func() { Enum("a", "b") }))
		Attribute("matrix", ArrayOf(ArrayOf(Float64)))
		Attribute("labels", ArrayOf(Label))
		Attribute("named_labels", Labels)
		Attribute("counts", MapOf(String, Int))
		Attribute("by_id", MapOf(Int, Label))
		Attribute("timestamps", MapOf(String, String, func() {
			Elem(func() { Format(FormatDateTime) })
		}))
		Attribute("blobs", ArrayOf(Bytes))
		Required("strings")
	})
	stream(Collections)
}

// NestedDSL covers inline objects and recursive types.
var NestedDSL = func() {
	var Leaf = Type("Leaf", func() {
		Attribute("value", String)
	})
	var Tree = Type("Tree", func() {
		Description("A node that refers to itself.")
		Attribute("name", String)
		Attribute("children", ArrayOf("Tree"))
		Attribute("leaf", Leaf)
		Attribute("position", func() {
			Description("An inline object.")
			Attribute("x", Float64)
			Attribute("y", Float64)
			Attribute("label", func() {
				Attribute("text", String)
				Attribute("at", String, func() { Format(FormatDateTime) })
				Required("text")
			})
			Required("x", "y")
		})
		Required("name")
	})
	var _ = Type("Ping", func() {
		Attribute("pong", "Pong")
	})
	var Pong = Type("Pong", func() {
		Attribute("ping", "Ping")
		Attribute("tree", Tree)
	})
	var Forest = Type("Forest", func() {
		Attribute("trees", ArrayOf(Tree))
		Attribute("pong", Pong)
	})
	stream(Forest)
}

// UnionDSL covers OneOf, with default and custom discriminator keys.
var UnionDSL = func() {
	var Circle = Type("Circle", func() {
		Attribute("radius", Float64)
		Required("radius")
	})
	var Square = Type("Square", func() {
		Attribute("side", Float64)
		Required("side")
	})
	var Drawing = Type("Drawing", func() {
		OneOf("shape", func() {
			Attribute("circle", Circle)
			Attribute("square", Square)
			Attribute("label", String)
			Attribute("drawn_at", String, func() { Format(FormatDateTime) })
		})
		OneOf("payment", func() {
			Meta("oneof:type:field", "kind")
			Meta("oneof:value:field", "details")
			Attribute("cash", Int)
			Attribute("points", ArrayOf(Int))
		})
		Required("shape")
	})
	stream(Drawing)
}

// ViewsDSL covers result types with views.
var ViewsDSL = func() {
	var Owner = ResultType("application/vnd.owner", func() {
		TypeName("Owner")
		Attributes(func() {
			Attribute("id", String)
			Attribute("email", String)
			Required("id", "email")
		})
		View("default", func() {
			Attribute("id")
		})
		View("full", func() {
			Attribute("id")
			Attribute("email")
		})
	})
	var Viewed = ResultType("application/vnd.viewed", func() {
		TypeName("Viewed")
		Attributes(func() {
			Attribute("id", String)
			Attribute("name", String)
			Attribute("secret", String)
			Attribute("level", Int, func() { Default(1) })
			Attribute("owner", Owner)
			Required("id", "name", "secret")
		})
		View("default", func() {
			Attribute("id")
			Attribute("name")
		})
		View("extended", func() {
			Attribute("id")
			Attribute("name")
			Attribute("secret")
			Attribute("level")
			Attribute("owner", func() { View("full") })
		})
	})
	stream(Viewed)
}

// JSONTagDSL covers attributes whose JSON name is overridden.
var JSONTagDSL = func() {
	var Tagged = Type("Tagged", func() {
		Attribute("plain_name", String)
		Attribute("legacy_name", String, func() { Meta("struct:tag:json", "legacyName") })
		Attribute("omit_empty", String, func() { Meta("struct:tag:json", "omitEmpty,omitempty") })
		Attribute("required_tagged", String, func() { Meta("struct:tag:json", "requiredTagged") })
		Attribute("quoted", String, func() { Meta("struct:tag:json", "needs-quotes") })
		Attribute("hidden", String, func() { Meta("struct:tag:json", "-") })
		Attribute("go_only", String, func() {
			Meta("struct:field:name", "Renamed")
			Meta("struct:tag:json:name", "ignoredOnTheWire")
		})
		Required("required_tagged")
	})
	stream(Tagged)
}

// AliasDSL covers named primitives and renamed types.
var AliasDSL = func() {
	var UUID = Type("UUID", String, func() {
		Description("A unique identifier.")
		Format(FormatUUID)
	})
	var Timestamp = Type("Timestamp", String, func() { Format(FormatDateTime) })
	var Percent = Type("Percent", Float64, func() {
		Minimum(0)
		Maximum(100)
	})
	var Website = Type("Website", String, func() { Format(FormatURI) })
	var Renamed = Type("go_style_name", func() {
		Meta("typescript:name", "RenamedThing")
		Attribute("id", UUID)
	})
	var Snake = Type("snake_case_type", func() {
		Attribute("ok", Boolean)
	})
	var Aliased = Type("Aliased", func() {
		Attribute("id", UUID)
		Attribute("created_at", Timestamp)
		Attribute("progress", Percent)
		Attribute("website", Website)
		Attribute("renamed", Renamed)
		Attribute("snake_case_type", Snake)
		Required("id")
	})
	stream(Aliased)
}

// NamesDSL covers identifiers that collide or are reserved.
var NamesDSL = func() {
	var ResultT = Type("Result", func() {
		Description("Collides with the runtime's Result.")
		Attribute("value", String)
	})
	var DateT = Type("Date", func() {
		Description("Collides with the global Date.")
		Attribute("iso", String, func() { Format(FormatDateTime) })
	})
	var Lower = Type("thing", func() { Attribute("a", String) })
	var Upper = Type("Thing", func() { Attribute("b", String) })
	var WatchPayload = Type("WatchPayload", func() {
		Description("Collides with the request type of the watch method.")
		Attribute("c", String)
	})
	Service("default", func() {
		Description("A service named after a reserved word.")
		Method("watch", func() {
			Payload(func() {
				Attribute("class", String, "A reserved word as a property name.")
				Attribute("needs-quotes", String)
				Attribute("result", ResultT)
				Attribute("date", DateT)
				Attribute("lower", Lower)
				Attribute("upper", Upper)
				Attribute("payload", WatchPayload)
			})
			StreamingResult(func() {
				Attribute("new", String)
			})
			HTTP(func() {
				POST("/watch")
				ServerSentEvents()
			})
		})
		Method("delete", func() {
			StreamingResult(String)
			HTTP(func() {
				GET("/delete")
				ServerSentEvents()
			})
		})
	})
	Service("send", func() {
		Description("A service named after a runtime export.")
		Method("stream_things", func() {
			StreamingResult(String)
			HTTP(func() {
				GET("/send")
				ServerSentEvents()
			})
		})
		Method("streamThings", func() {
			StreamingResult(String)
			HTTP(func() {
				GET("/send2")
				ServerSentEvents()
			})
		})
	})
}

// SSEWholeEventDSL streams a named type as the whole event.
var SSEWholeEventDSL = func() {
	var Event = Type("Event", func() {
		Attribute("type", String, func() { Default("thing") })
		Attribute("id", String)
		Attribute("at", String, func() { Format(FormatDateTime) })
		Required("id")
	})
	Service("svc", func() {
		Error("not_found")
		Error("bad_upstream", func() { Fault() })
		HTTP(func() {
			Path("/api")
			Response("not_found", StatusNotFound)
			Response("bad_upstream", StatusBadGateway)
		})
		Method("stream_things", func() {
			Description("Streams things.\nOne event per change.")
			Payload(func() {
				Attribute("id", String, "Thing identifier")
				Attribute("delay_ms", Int, "Delay between events", func() { Default(0) })
				Attribute("kinds", ArrayOf(String), "Kinds to include")
				Attribute("trace", String)
				Required("id")
			})
			StreamingResult(Event)
			HTTP(func() {
				GET("/things/{id}/events")
				Param("delay_ms")
				Param("kinds:kind")
				Header("trace:X-Trace-Id")
				ServerSentEvents()
			})
		})
	})
}

// SSEDataFieldDSL maps event attributes onto the SSE data, id, event and
// retry fields, and a payload attribute onto Last-Event-ID.
var SSEDataFieldDSL = func() {
	var Snapshot = Type("Snapshot", func() {
		Attribute("value", Float64)
		Attribute("taken_at", String, func() { Format(FormatDateTime) })
		Required("value")
	})
	var Envelope = Type("Envelope", func() {
		Attribute("data", Snapshot)
		Attribute("id", String)
		Attribute("event", String)
		Attribute("retry", Int)
		Attribute("event_id", String, func() { Meta("struct:tag:json", "eventId") })
		Required("data")
	})
	Service("svc", func() {
		Method("stream_wrapped", func() {
			Payload(func() {
				Attribute("id", String)
				Attribute("last_event_id", String)
				Attribute("count", Int)
				Required("id")
			})
			StreamingResult(Envelope)
			HTTP(func() {
				GET("/things/{id}/wrapped")
				Param("count")
				ServerSentEvents(func() {
					SSEEventData("data")
					SSEEventID("id")
					SSEEventType("event")
					SSEEventRetry("retry")
					SSERequestID("last_event_id")
				})
			})
		})
		Method("stream_renamed_id", func() {
			StreamingResult(Envelope)
			HTTP(func() {
				GET("/renamed")
				ServerSentEvents(func() {
					SSEEventID("event_id")
				})
			})
		})
		Method("stream_counter", func() {
			StreamingResult(func() {
				Attribute("count", Int)
				Attribute("id", String)
				Required("count")
			})
			HTTP(func() {
				GET("/counter")
				ServerSentEvents(func() {
					SSEEventData("count")
					SSEEventID("id")
				})
			})
		})
	})
}

// SSEPostBodyDSL streams from POST endpoints that carry a JSON body.
var SSEPostBodyDSL = func() {
	var Filter = Type("Filter", func() {
		Attribute("kinds", ArrayOf(String, func() { Enum("widget", "gadget") }))
		Attribute("since", String, func() { Format(FormatDateTime) })
		Attribute("limit", Int, func() { Default(10) })
	})
	Service("svc", func() {
		Method("stream_filtered", func() {
			Description("The body is one payload attribute.")
			Payload(func() {
				Attribute("filter", Filter)
				Attribute("tenant", String)
				Required("filter", "tenant")
			})
			StreamingResult(String)
			HTTP(func() {
				POST("/tenants/{tenant}/things")
				Body("filter")
				ServerSentEvents()
			})
		})
		Method("stream_fields", func() {
			Description("The body is every attribute not mapped elsewhere.")
			Payload(func() {
				Attribute("tenant", String)
				Attribute("name", String)
				Attribute("legacy_name", String, func() { Meta("struct:tag:json", "legacyName") })
				Attribute("since", String, func() { Format(FormatDateTime) })
				Attribute("blob", Bytes)
				Attribute("page", Int, func() { Default(1) })
				Attribute("cursor", Bytes)
				Required("tenant", "name")
			})
			StreamingResult(String)
			HTTP(func() {
				POST("/tenants/{tenant}/fields")
				Param("page")
				Param("cursor")
				ServerSentEvents()
			})
		})
		Method("stream_named", func() {
			Description("The payload is a named type.")
			Payload(Filter)
			StreamingResult(String)
			HTTP(func() {
				POST("/named")
				ServerSentEvents()
			})
		})
		Method("stream_list", func() {
			Description("The payload is an array sent as the body.")
			Payload(ArrayOf(String))
			StreamingResult(String)
			HTTP(func() {
				POST("/list")
				ServerSentEvents()
			})
		})
	})
}

// SSEStringDSL streams plain strings.
var SSEStringDSL = func() {
	Service("svc", func() {
		Method("stream_text", func() {
			StreamingResult(String)
			HTTP(func() {
				GET("/text")
				ServerSentEvents()
			})
		})
		Method("stream_one", func() {
			Description("The payload is a single string mapped to the path.")
			Payload(String)
			StreamingResult(String)
			HTTP(func() {
				GET("/text/{id}")
				ServerSentEvents()
			})
		})
	})
}

// SSEIntDSL streams the other primitive kinds.
var SSEIntDSL = func() {
	Service("svc", func() {
		Method("stream_count", func() {
			StreamingResult(Int)
			HTTP(func() {
				GET("/count")
				ServerSentEvents()
			})
		})
		Method("stream_ratio", func() {
			StreamingResult(Float64)
			HTTP(func() {
				GET("/ratio")
				ServerSentEvents()
			})
		})
		Method("stream_flag", func() {
			StreamingResult(Boolean)
			HTTP(func() {
				GET("/flag")
				ServerSentEvents()
			})
		})
		Method("stream_blob", func() {
			StreamingResult(Bytes)
			HTTP(func() {
				GET("/blob")
				ServerSentEvents()
			})
		})
	})
}

// SecurityDSL covers the security schemes and where they put credentials.
var SecurityDSL = func() {
	var JWT = JWTSecurity("jwt")
	var Key = APIKeySecurity("api_key")
	var Basic = BasicAuthSecurity("basic")
	var OAuth = OAuth2Security("oauth", func() {
		ClientCredentialsFlow("http://example.com/token", "http://example.com/refresh")
	})
	Service("svc", func() {
		Security(JWT)
		Error("unauthorized")
		HTTP(func() { Response("unauthorized", StatusUnauthorized) })
		Method("stream_jwt", func() {
			Description("Inherits the service's JWT requirement.")
			Payload(func() {
				Token("token", String)
				Attribute("id", String)
				Required("token", "id")
			})
			StreamingResult(String)
			HTTP(func() {
				GET("/jwt/{id}")
				ServerSentEvents()
			})
		})
		Method("stream_token_only", func() {
			Description("Nothing is left of the payload once the token is hoisted.")
			Payload(func() {
				Token("token", String)
			})
			StreamingResult(String)
			HTTP(func() {
				GET("/token-only")
				ServerSentEvents()
			})
		})
		Method("stream_public", func() {
			NoSecurity()
			StreamingResult(String)
			HTTP(func() {
				GET("/public")
				ServerSentEvents()
			})
		})
		Method("stream_keyed", func() {
			Description("An API key in the query string.")
			Security(Key)
			Payload(func() {
				APIKey("api_key", "key", String)
				Attribute("id", String)
			})
			StreamingResult(String)
			HTTP(func() {
				GET("/keyed")
				Param("key:api_key")
				Param("id")
				ServerSentEvents()
			})
		})
		Method("stream_key_header", func() {
			Description("An API key in a header.")
			Security(Key)
			Payload(func() {
				APIKey("api_key", "key", String)
			})
			StreamingResult(String)
			HTTP(func() {
				GET("/key-header")
				Header("key:X-API-Key")
				ServerSentEvents()
			})
		})
		Method("stream_basic", func() {
			Security(Basic)
			Payload(func() {
				Username("user", String)
				Password("pass", String)
				Attribute("id", String)
				Required("user", "pass")
			})
			StreamingResult(String)
			HTTP(func() {
				GET("/basic")
				Param("id")
				ServerSentEvents()
			})
		})
		Method("stream_oauth", func() {
			Security(OAuth)
			Payload(func() {
				AccessToken("access_token", String)
			})
			StreamingResult(String)
			HTTP(func() {
				GET("/oauth")
				ServerSentEvents()
			})
		})
		Method("stream_either", func() {
			Description("Accepts a JWT or an API key.")
			Security(JWT)
			Security(Key)
			Payload(func() {
				Token("token", String)
				APIKey("api_key", "key", String)
			})
			StreamingResult(String)
			HTTP(func() {
				GET("/either")
				Header("key:X-API-Key")
				ServerSentEvents()
			})
		})
	})
}

// ErrorsDSL covers declared errors, including a custom error type.
var ErrorsDSL = func() {
	var Problem = Type("Problem", func() {
		Attribute("field", String)
		Attribute("message", String)
		Required("field", "message")
	})
	Service("svc", func() {
		Error("not_found")
		HTTP(func() { Response("not_found", StatusNotFound) })
		Method("stream_things", func() {
			Error("invalid", Problem)
			Error("busy", func() {
				Temporary()
				Timeout()
			})
			Error("precondition_failed")
			Error("gone")
			StreamingResult(String)
			HTTP(func() {
				GET("/things")
				ServerSentEvents()
				Response("invalid", StatusUnprocessableEntity)
				Response("busy", StatusServiceUnavailable)
				Response("precondition_failed", StatusBadRequest)
				Response("gone", StatusBadRequest)
			})
		})
	})
}

// TwoServicesDSL has two services that reuse method names and a type.
var TwoServicesDSL = func() {
	var Thing = Type("Thing", func() {
		Attribute("id", String)
		Required("id")
	})
	Service("toy", func() {
		Method("stream_things", func() {
			Payload(func() {
				Attribute("id", String)
				Required("id")
			})
			StreamingResult(Thing)
			HTTP(func() {
				GET("/toy/{id}")
				ServerSentEvents()
			})
		})
		Method("get_thing", func() {
			Payload(func() {
				Attribute("id", String)
				Required("id")
			})
			Result(Thing)
			HTTP(func() {
				GET("/toy/{id}/thing")
			})
		})
	})
	Service("echo", func() {
		Method("stream_things", func() {
			Payload(func() {
				Attribute("id", String)
				Attribute("loud", Boolean)
				Required("id")
			})
			StreamingResult(Thing)
			HTTP(func() {
				GET("/echo/{id}")
				Param("loud")
				ServerSentEvents()
			})
		})
		Method("chat", func() {
			StreamingPayload(String)
			StreamingResult(String)
			HTTP(func() {
				GET("/echo/chat")
			})
		})
	})
	var Unreachable = Type("Unreachable", func() { Attribute("x", String) })
	Service("hidden", func() {
		Meta("typescript:generate", "false")
		Method("stream_things", func() {
			StreamingResult(Unreachable)
			HTTP(func() {
				GET("/hidden")
				ServerSentEvents()
			})
		})
	})
}

// RESTBasicDSL covers the plain shapes of a unary endpoint: a named result,
// an inline result, no payload and no content.
var RESTBasicDSL = func() {
	var Thing = Type("Thing", func() {
		Attribute("id", String)
		Attribute("name", String)
		Attribute("created_at", String, func() { Format(FormatDateTime) })
		Required("id")
	})
	Service("svc", func() {
		Error("not_found")
		HTTP(func() {
			Path("/api")
			Response("not_found", StatusNotFound)
		})
		Method("get_thing", func() {
			Description("Returns one thing.")
			Payload(func() {
				Attribute("id", String)
				Attribute("verbose", Boolean)
				Attribute("trace", String)
				Required("id")
			})
			Result(Thing)
			HTTP(func() {
				GET("/things/{id}")
				Param("verbose")
				Header("trace:X-Trace-Id")
			})
		})
		Method("create_thing", func() {
			Payload(Thing)
			Result(Thing)
			HTTP(func() {
				POST("/things")
				Response(StatusCreated)
			})
		})
		Method("stats", func() {
			Description("The result is declared inline.")
			Result(func() {
				Attribute("count", Int)
				Attribute("newest", String, func() { Format(FormatDateTime) })
				Required("count")
			})
			HTTP(func() {
				GET("/stats")
			})
		})
		Method("delete_thing", func() {
			Payload(func() {
				Attribute("id", String)
				Required("id")
			})
			HTTP(func() {
				DELETE("/things/{id}")
				Response(StatusNoContent)
			})
		})
		Method("ping", func() {
			Description("Neither payload nor result.")
			HTTP(func() {
				GET("/ping")
			})
		})
	})
}

// RESTScalarsDSL covers results that are not objects, collections and views.
var RESTScalarsDSL = func() {
	var Item = ResultType("application/vnd.item", func() {
		TypeName("Item")
		Attributes(func() {
			Attribute("id", String)
			Attribute("secret", String)
			Required("id", "secret")
		})
		View("default", func() {
			Attribute("id")
		})
		View("full", func() {
			Attribute("id")
			Attribute("secret")
		})
	})
	var Shelf = Type("Shelf", func() {
		Attribute("items", CollectionOf(Item))
	})
	Service("svc", func() {
		Method("count", func() {
			Result(Int)
			HTTP(func() { GET("/count") })
		})
		Method("blob", func() {
			Result(Bytes)
			HTTP(func() { GET("/blob") })
		})
		Method("names", func() {
			Result(ArrayOf(String))
			HTTP(func() { GET("/names") })
		})
		Method("when", func() {
			Result(MapOf(String, String, func() {
				Elem(func() { Format(FormatDateTime) })
			}))
			HTTP(func() { GET("/when") })
		})
		Method("list_items", func() {
			Result(CollectionOf(Item))
			HTTP(func() { GET("/items") })
		})
		Method("get_item", func() {
			Result(Item)
			HTTP(func() { GET("/item") })
		})
		Method("get_shelf", func() {
			Result(Shelf)
			HTTP(func() { GET("/shelf") })
		})
	})
}

// RESTHeadersDSL maps result attributes onto response headers of every shape.
var RESTHeadersDSL = func() {
	var Version = Type("Version", Int)
	var Page = Type("Page", func() {
		Attribute("items", ArrayOf(String))
		Attribute("etag", String)
		Attribute("total", Int)
		Attribute("ratio", Float64)
		Attribute("more", Boolean)
		Attribute("expires", String, func() { Format(FormatDateTime) })
		Attribute("version", Version)
		Attribute("tags", ArrayOf(String))
		Attribute("counts", ArrayOf(Int))
		Attribute("flags", ArrayOf(Boolean))
		Attribute("legacy_name", String, func() { Meta("struct:tag:json", "legacyName") })
		Attribute("session", String)
		Required("items", "etag", "total")
	})
	Service("svc", func() {
		Method("get_page", func() {
			Result(Page)
			HTTP(func() {
				GET("/page")
				Response(StatusOK, func() {
					Header("etag:ETag")
					Header("total:X-Total")
					Header("ratio:X-Ratio")
					Header("more:X-More")
					Header("expires:Expires-At")
					Header("version:X-Version")
					Header("tags:X-Tags")
					Header("counts:X-Counts")
					Header("flags:X-Flags")
					Header("legacy_name:X-Legacy-Name")
					Cookie("session:SID")
				})
			})
		})
		Method("head_page", func() {
			Description("Every attribute travels in a header.")
			Result(func() {
				Attribute("etag", String)
				Attribute("total", Int)
				Required("etag")
			})
			HTTP(func() {
				HEAD("/page")
				Response(StatusOK, func() {
					Header("etag:ETag")
					Header("total:X-Total")
				})
			})
		})
		Method("count_things", func() {
			Description("A primitive result in a header.")
			Result(Int)
			HTTP(func() {
				GET("/count")
				Response(StatusOK, func() {
					Header("X-Count")
				})
			})
		})
		Method("signature", func() {
			Description("A bytes header cannot be decoded.")
			Result(func() {
				Attribute("sig", Bytes)
			})
			HTTP(func() {
				GET("/signature")
				Response(StatusOK, func() {
					Header("sig:X-Signature")
				})
			})
		})
	})
}

// RESTStatusesDSL covers Body("attr"), several success statuses and tags.
var RESTStatusesDSL = func() {
	var Diagram = Type("Diagram", func() {
		Attribute("svg", String)
		Required("svg")
	})
	var Upserted = Type("Upserted", func() {
		Attribute("id", String)
		Attribute("outcome", String, func() { Enum("updated", "created", "accepted") })
		Required("id")
	})
	Service("svc", func() {
		Method("upsert_thing", func() {
			Payload(func() {
				Attribute("id", String)
				Attribute("name", String)
				Required("id")
			})
			Result(Upserted)
			HTTP(func() {
				PUT("/things/{id}")
				Response(StatusOK)
				Response(StatusCreated, func() {
					Tag("outcome", "created")
				})
				Response(StatusAccepted, func() {
					Tag("outcome", "accepted")
					Body(Empty)
				})
			})
		})
		Method("find_diagram", func() {
			Description("An untagged 204 beside a tagged 200 that carries one attribute.")
			Result(func() {
				Attribute("diagram", Diagram)
				Attribute("found", String)
			})
			HTTP(func() {
				GET("/diagram")
				Response(StatusNoContent, func() {
					Body(Empty)
				})
				Response(StatusOK, func() {
					Tag("found", "yes")
					Body("diagram")
				})
			})
		})
		Method("strict", func() {
			Description("A required attribute that the 204 does not carry.")
			Result(func() {
				Attribute("id", String)
				Attribute("state", String)
				Required("id")
			})
			HTTP(func() {
				GET("/strict")
				Response(StatusOK)
				Response(StatusNoContent, func() {
					Tag("state", "gone")
					Body(Empty)
				})
			})
		})
	})
}

// RESTErrorsDSL covers error types: named with a header, unnamed and primitive.
var RESTErrorsDSL = func() {
	var ValidationProblem = Type("ValidationProblem", func() {
		Attribute("field", String)
		Attribute("message", String)
		Attribute("retry_after", Int)
		Required("field", "message")
	})
	Service("svc", func() {
		Method("create_thing", func() {
			Payload(func() {
				Attribute("name", String)
			})
			Result(String)
			Error("invalid", ValidationProblem)
			Error("conflict", func() {
				Attribute("existing_id", String)
				Attribute("at", String, func() { Format(FormatDateTime) })
				Required("existing_id")
			})
			Error("teapot", String)
			Error("busy")
			HTTP(func() {
				POST("/things")
				Response("invalid", StatusUnprocessableEntity, func() {
					Header("retry_after:Retry-After")
				})
				Response("conflict", StatusConflict)
				Response("teapot", StatusTeapot)
				Response("busy", StatusServiceUnavailable)
			})
		})
	})
}

// RESTRawDSL covers bodies that the client does not encode or decode.
var RESTRawDSL = func() {
	Service("svc", func() {
		Method("download", func() {
			Payload(func() {
				Attribute("id", String)
				Required("id")
			})
			Result(func() {
				Attribute("content_length", Int64)
				Attribute("content_type", String)
				Required("content_length")
			})
			HTTP(func() {
				GET("/files/{id}")
				SkipResponseBodyEncodeDecode()
				Response(StatusOK, func() {
					ContentType("application/zip")
					Header("content_length:Content-Length")
					Header("content_type:Content-Type")
				})
			})
		})
		Method("download_plain", func() {
			Description("No result besides the body.")
			HTTP(func() {
				GET("/plain")
				SkipResponseBodyEncodeDecode()
			})
		})
		Method("upload", func() {
			Payload(func() {
				Attribute("id", String)
				Attribute("content_type", String)
				Required("id")
			})
			Result(func() {
				Attribute("size", Int)
				Required("size")
			})
			HTTP(func() {
				PUT("/files/{id}")
				Header("content_type:Content-Type")
				SkipRequestBodyEncodeDecode()
			})
		})
		Method("upload_anonymous", func() {
			Description("No payload besides the body.")
			HTTP(func() {
				POST("/files")
				SkipRequestBodyEncodeDecode()
				Response(StatusNoContent)
			})
		})
		Method("upload_tagged", func() {
			Description("Every payload attribute is optional, but the body follows it.")
			Payload(func() {
				Attribute("tag", String)
			})
			HTTP(func() {
				POST("/tagged")
				Header("tag:X-Tag")
				SkipRequestBodyEncodeDecode()
				Response(StatusNoContent)
			})
		})
	})
}

// RESTMixedDSL offers one method both as a plain response and as a stream.
var RESTMixedDSL = func() {
	var Thing = Type("Thing", func() {
		Attribute("id", String)
		Required("id")
	})
	Service("svc", func() {
		Error("not_found")
		HTTP(func() { Response("not_found", StatusNotFound) })
		Method("list_things", func() {
			Description("Lists things, at once or as they are found.")
			Payload(func() {
				Attribute("kind", String)
			})
			Result(ArrayOf(Thing))
			StreamingResult(Thing)
			HTTP(func() {
				GET("/things")
				Param("kind")
				ServerSentEvents()
			})
		})
	})
}

// RESTSkippedDSL holds the unary endpoints no function is generated for.
var RESTSkippedDSL = func() {
	Service("svc", func() {
		Method("moved", func() {
			HTTP(func() {
				GET("/old")
				Redirect("/new", StatusMovedPermanently)
			})
		})
		Method("page", func() {
			Result(String)
			HTTP(func() {
				GET("/page")
				Response(StatusOK, func() {
					ContentType("text/html")
				})
			})
		})
		Method("fetch_with_body", func() {
			HTTP(func() {
				GET("/fetch")
				SkipRequestBodyEncodeDecode()
			})
		})
		Method("ok", func() {
			Result(String)
			HTTP(func() { GET("/ok") })
		})
	})
}

// NamedAttrsDSL covers Meta("typescript:name") on inline attributes: hoisted
// for primitives, arrays, maps and unions; ignored, with a warning, for an
// inline object and for a user type; rendered in place for a parameter whose
// encoding differs from the body's.
var NamedAttrsDSL = func() {
	var Label = Type("Label", func() {
		Attribute("key", String)
		Attribute("value", String)
		Required("key", "value")
	})
	var ColorKeyT = Type("ColorKey", func() {
		Description("A declared type that takes the name an attribute asks for.")
		Attribute("hex", String)
	})
	var Widget = Type("Widget", func() {
		Attribute("color_key", String, "The key of the widget's color.", func() {
			Enum("blue", "green")
			Meta("typescript:name", "ColorKey")
		})
		Attribute("priority", Int, func() {
			Minimum(1)
			Maximum(5)
			Default(3)
			Meta("typescript:name", "Priority")
		})
		Attribute("labels", ArrayOf(Label), func() { Meta("typescript:name", "Labels") })
		Attribute("scores", MapOf(String, Float64), func() { Meta("typescript:name", "Scores") })
		Attribute("kinds", ArrayOf(String, func() {
			Enum("a", "b")
			Meta("typescript:name", "Kind")
		}))
		Attribute("children", ArrayOf("Widget"), func() { Meta("typescript:name", "Widgets") })
		OneOf("shape", func() {
			Meta("typescript:name", "Shape")
			Attribute("circle", Float64)
			Attribute("label", String)
		})
		Attribute("position", func() {
			Meta("typescript:name", "Position")
			Attribute("x", Int)
			Attribute("y", Int)
		})
		Attribute("label", Label, func() { Meta("typescript:name", "OneLabel") })
		Attribute("fallback", ColorKeyT)
		Required("color_key")
	})
	Service("svc", func() {
		Method("create", func() {
			Payload(func() {
				Attribute("widget", Widget)
				Attribute("view", String, func() {
					Enum("full", "short")
					Meta("typescript:name", "View")
				})
				Attribute("cursor", Bytes, func() { Meta("typescript:name", "Cursor") })
				Required("widget")
			})
			Result(Widget)
			HTTP(func() {
				POST("/widgets")
				Param("view")
				Param("cursor")
			})
		})
	})
}

// RequestsDSL covers the request-side variants of declared types: emitted for a
// type with a defaulted attribute and for the types that refer to one, and
// used by request payloads.
var RequestsDSL = func() {
	var Plain = Type("Plain", func() {
		Description("No default anywhere: no input-side variant.")
		Attribute("id", String)
		Required("id")
	})
	var Snapshot = Type("Snapshot", func() {
		Attribute("pending", Boolean, func() { Default(false) })
		Attribute("taken_at", String, func() { Format(FormatDateTime) })
		Required("taken_at")
	})
	var Node = Type("Node", func() {
		Description("Recursive, with a default.")
		Attribute("name", String)
		Attribute("weight", Int, func() { Default(1) })
		Attribute("children", ArrayOf("Node"))
		Attribute("plain", Plain)
		Required("name")
	})
	var Report = Type("Report", func() {
		Description("No default of its own: differs through Snapshot.")
		Attribute("title", String)
		Attribute("latest", Snapshot)
		Attribute("history", ArrayOf(Snapshot))
		Attribute("by_name", MapOf(String, Snapshot))
		Attribute("plain", Plain)
		Required("title", "latest")
	})
	var Snapshots = Type("Snapshots", ArrayOf(Snapshot))
	var SnapshotRequest = Type("SnapshotRequest", func() {
		Description("Collides with the input-side variant of Snapshot.")
		Attribute("x", String)
	})
	Service("svc", func() {
		Method("submit", func() {
			Payload(func() {
				Attribute("report", Report)
				Attribute("note", String, func() { Default("none") })
				Attribute("extra", func() {
					Attribute("flag", Boolean, func() { Default(true) })
					Attribute("plain", Plain)
				})
				Attribute("named", SnapshotRequest)
				Attribute("all", Snapshots)
				Required("report")
			})
			Result(Report)
			HTTP(func() {
				POST("/reports")
			})
		})
		Method("submit_node", func() {
			Payload(Node)
			Result(Node)
			HTTP(func() {
				POST("/nodes")
			})
		})
	})
}
