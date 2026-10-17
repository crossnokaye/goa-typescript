package design

import . "goa.design/goa/v3/dsl"

var _ = Service("toy", func() {
	Description("Toy service with one method per feature of the generated client.")

	Error("not_found")
	Error("unauthorized")
	Error("bad_upstream", func() {
		Fault()
	})
	Error("precondition_failed")

	HTTP(func() {
		Response("not_found", StatusNotFound)
		Response("unauthorized", StatusUnauthorized)
		Response("bad_upstream", StatusBadGateway)
		Response("precondition_failed", StatusBadRequest)
	})

	Method("stream_things_sse", func() {
		Description("Streams three events about a thing. The id \"missing\" fails with not_found.")
		PayloadWithToken(func() {
			Attribute("id", String, "Thing identifier")
			Attribute("delay_ms", Int, "Pause between events, in milliseconds", func() {
				Default(0)
			})
			Required("id")
		})
		StreamingResult(ThingEvent)
		HTTP(func() {
			GET("/things/{id}/events")
			Param("delay_ms")
			ServerSentEvents()
		})
	})

	Method("stream_wrapped_sse", func() {
		Description("Streams things in an envelope mapped onto the SSE data, id, event and retry fields.")
		PayloadWithToken(func() {
			Attribute("id", String, "Thing identifier")
			Attribute("count", Int, "Number of events to send", func() {
				Default(3)
			})
			Attribute("since", String, "Becomes the creation time of every thing", func() {
				Format(FormatDateTime)
			})
			Attribute("last_event_id", String, "Resume after this event")
			Required("id")
		})
		StreamingResult(ThingEventEnvelope)
		HTTP(func() {
			GET("/things/{id}/wrapped")
			Param("count")
			Param("since")
			ServerSentEvents(func() {
				SSEEventData("data")
				SSEEventID("id")
				SSEEventType("event")
				SSEEventRetry("retry")
				SSERequestID("last_event_id")
			})
		})
	})

	Method("stream_post_sse", func() {
		Description("Streams one event per kind in the filter, which is the JSON request body.")
		PayloadWithToken(func() {
			Attribute("filter", ThingFilter)
			Required("filter")
		})
		StreamingResult(ThingEvent)
		HTTP(func() {
			POST("/things/events")
			Body("filter")
			ServerSentEvents()
		})
	})

	Method("stream_text_sse", func() {
		Description("Streams plain strings, one of them spanning two lines.")
		PayloadWithToken(nil)
		StreamingResult(String)
		HTTP(func() {
			GET("/text")
			ServerSentEvents()
		})
	})

	Method("stream_count_sse", func() {
		Description("Streams the integers 1, 2 and 3.")
		PayloadWithToken(nil)
		StreamingResult(Int)
		HTTP(func() {
			GET("/count")
			ServerSentEvents()
		})
	})

	Method("stream_viewed_sse", func() {
		Description("Streams a result type rendered with the requested view.")
		PayloadWithToken(func() {
			Attribute("view", String, "View to render", func() {
				Enum("default", "extended")
				Default("default")
			})
		})
		StreamingResult(ThingWithViews)
		HTTP(func() {
			GET("/viewed")
			Param("view")
			ServerSentEvents()
		})
	})

	Method("stream_public_sse", func() {
		Description("Needs no credential and reports whether one was sent anyway.")
		NoSecurity()
		StreamingResult(ThingEvent)
		HTTP(func() {
			GET("/public")
			ServerSentEvents()
		})
	})

	Method("stream_keyed_sse", func() {
		Description("Authenticates with an API key in the query string.")
		Security(APIKeyAuth)
		Payload(func() {
			APIKey("api_key", "key", String, "API key, supplied by the client's auth option")
		})
		StreamingResult(ThingEvent)
		HTTP(func() {
			GET("/keyed")
			Param("key")
			ServerSentEvents()
		})
	})

	Method("watch_ticks", func() {
		Description("Streams ticks over a WebSocket, then closes with code 1000. Authenticates with an API key " +
			"in the query string, which a browser WebSocket can send. The id \"missing\" fails with not_found " +
			"before the upgrade.")
		Security(APIKeyAuth)
		Payload(func() {
			APIKey("api_key", "key", String, "API key, supplied by the client's auth option")
			Attribute("id", String, "Identifier to watch")
			Attribute("count", Int, "Number of ticks", func() {
				Default(3)
			})
			Attribute("delay_ms", Int, "Pause between ticks, in milliseconds", func() {
				Default(0)
			})
			Required("id")
		})
		StreamingResult(Tick)
		HTTP(func() {
			GET("/ws/things/{id}/ticks")
			Param("key")
			Param("count")
			Param("delay_ms")
		})
	})

	Method("echo_ws", func() {
		Description("Answers every message over a WebSocket with its text upper-cased and repeated, until the " +
			"client sends goa's end-of-stream null; then closes with code 1000. The text \"kick\" closes the " +
			"connection with code 4001 instead.")
		NoSecurity()
		Payload(func() {
			Attribute("prefix", String, "Prepended to every answer")
		})
		StreamingPayload(func() {
			Attribute("text", String, func() {
				MinLength(1)
			})
			Attribute("repeat", Int, func() {
				Minimum(1)
				Default(1)
			})
			Required("text")
		})
		StreamingResult(func() {
			Attribute("text", String)
			Attribute("length", Int, "Length of the text received")
			Required("text", "length")
		})
		HTTP(func() {
			GET("/ws/echo")
			Param("prefix")
		})
	})

	Method("sum_ints", func() {
		Description("Sums the integers a client streams over a WebSocket and answers once with the total. " +
			"Authenticates with the JWT in the Authorization header, which a browser WebSocket cannot send.")
		PayloadWithToken(nil)
		StreamingPayload(Int)
		Result(func() {
			Attribute("sum", Int)
			Attribute("count", Int)
			Required("sum", "count")
		})
		HTTP(func() {
			GET("/ws/sum")
		})
	})

	Method("get_thing", func() {
		Description("Returns a thing and echoes the request. The id \"missing\" fails with not_found.")
		PayloadWithToken(func() {
			Attribute("id", String, "Thing identifier")
			Attribute("tags", ArrayOf(String), "Sent as repeated tag parameters")
			Attribute("verbose", Boolean)
			Attribute("since", String, func() {
				Format(FormatDateTime)
			})
			Attribute("trace", String, "Sent in the X-Trace-Id header")
			Required("id")
		})
		Result(ThingReply)
		HTTP(func() {
			GET("/things/{id}")
			Param("tags:tag")
			Param("verbose")
			Param("since")
			Header("trace:X-Trace-Id")
			Response(StatusOK, func() {
				Header("etag:ETag")
			})
		})
	})

	Method("head_thing", func() {
		Description("Answers a HEAD request with the version of a thing in the ETag header. Its route is " +
			"get_thing's, so its query key carries $method.")
		PayloadWithToken(func() {
			Attribute("thing_id", String, "Thing identifier")
			Required("thing_id")
		})
		Result(func() {
			Attribute("etag", String)
			Required("etag")
		})
		HTTP(func() {
			HEAD("/things/{thing_id}")
			Response(StatusOK, func() {
				Header("etag:ETag")
			})
			// A HEAD response has no body: the service's errors answer
			// with their status alone.
			Response("not_found", StatusNotFound, func() { Body(Empty) })
			Response("unauthorized", StatusUnauthorized, func() { Body(Empty) })
			Response("bad_upstream", StatusBadGateway, func() { Body(Empty) })
			Response("precondition_failed", StatusBadRequest, func() { Body(Empty) })
		})
	})

	Method("get_thing_viewed", func() {
		Description("Returns a result type rendered with the requested view.")
		PayloadWithToken(func() {
			Attribute("view", String, "View to render", func() {
				Enum("default", "extended")
				Default("default")
			})
		})
		Result(ThingWithViews)
		HTTP(func() {
			GET("/viewed/thing")
			Param("view")
		})
	})

	Method("list_things", func() {
		Description("Returns a collection of two things in the default view.")
		PayloadWithToken(nil)
		Result(CollectionOf(ThingWithViews))
		HTTP(func() {
			GET("/things")
		})
	})

	Method("create_thing", func() {
		Description("Creates a thing. The name \"bad\" fails with invalid, the name \"taken\" with conflict.")
		PayloadWithToken(func() {
			Attribute("name", String)
			Attribute("kind", String, func() {
				Enum("widget", "gadget")
				Default("widget")
			})
			Required("name")
		})
		Result(Thing)
		Error("invalid", ValidationProblem)
		Error("conflict", func() {
			Description("An error type the design does not name.")
			Attribute("existing_id", String)
			Attribute("since", String, func() {
				Format(FormatDateTime)
			})
			Required("existing_id")
		})
		HTTP(func() {
			POST("/things")
			Response(StatusCreated)
			Response("invalid", StatusUnprocessableEntity, func() {
				Header("retry_after:Retry-After")
			})
			Response("conflict", StatusConflict)
		})
	})

	Method("echo_thing", func() {
		Description("Returns the thing it was sent, with the defaults the server applied to it.")
		// A POST that only reads: query options rather than a mutation.
		Meta("typescript:query:kind", "query")
		PayloadWithToken(func() {
			Attribute("thing", Thing)
			Required("thing")
		})
		Result(Thing)
		HTTP(func() {
			POST("/things/echo")
		})
	})

	Method("upsert_thing", func() {
		Description("Updates a thing, or creates it when the id is \"new\". The status code tells which.")
		PayloadWithToken(func() {
			Attribute("id", String)
			Attribute("name", String)
			Required("id", "name")
		})
		Result(UpsertResult)
		HTTP(func() {
			PUT("/things/{id}")
			Response(StatusOK)
			Response(StatusCreated, func() {
				Tag("outcome", "created")
			})
		})
	})

	Method("find_thing", func() {
		Description("Answers 200 with the match as the body, or 204 when the name is \"nothing\".")
		PayloadWithToken(func() {
			Attribute("name", String)
			Required("name")
		})
		Result(func() {
			Attribute("match", ThingMatch)
			Attribute("found", String)
		})
		HTTP(func() {
			GET("/find")
			Param("name")
			Response(StatusNoContent, func() {
				Body(Empty)
			})
			Response(StatusOK, func() {
				Tag("found", "yes")
				Body("match")
			})
		})
	})

	Method("delete_thing", func() {
		Description("Deletes a thing. The id \"missing\" fails with not_found.")
		PayloadWithToken(func() {
			Attribute("id", String)
			Required("id")
		})
		HTTP(func() {
			DELETE("/things/{id}")
			Response(StatusNoContent)
		})
	})

	Method("download_thing", func() {
		Description("Streams the content of a thing; the client does not decode the body.")
		PayloadWithToken(func() {
			Attribute("id", String)
			Required("id")
		})
		Result(func() {
			Attribute("content_length", Int64)
			Attribute("content_type", String)
			Required("content_length", "content_type")
		})
		HTTP(func() {
			GET("/things/{id}/content")
			SkipResponseBodyEncodeDecode()
			Response(StatusOK, func() {
				Header("content_length:Content-Length")
				Header("content_type:Content-Type")
			})
		})
	})

	Method("upload_thing", func() {
		Description("Receives the content of a thing; the client does not encode the body.")
		PayloadWithToken(func() {
			Attribute("id", String)
			Attribute("content_type", String)
			Required("id")
		})
		Result(func() {
			Attribute("size", Int, "Number of bytes received")
			Attribute("content_type", String, "The Content-Type the server saw")
			Attribute("text", String, "The bytes received, as text")
			Required("size", "text")
		})
		HTTP(func() {
			PUT("/things/{id}/content")
			Header("content_type:Content-Type")
			SkipRequestBodyEncodeDecode()
		})
	})

	Method("stream_or_list", func() {
		Description("Offers the same things as one response or as a stream, depending on the Accept header.")
		PayloadWithToken(func() {
			Attribute("empty", Boolean, "Return no things: a nil slice, which Go encodes as null")
		})
		// Not Thing: goa's server for a mixed method does not import the
		// package of a type that lives outside the service package.
		Result(ArrayOf(ThingSummary))
		StreamingResult(ThingSummary)
		HTTP(func() {
			GET("/stream-or-list")
			Param("empty")
			ServerSentEvents()
		})
	})

	Method("thing_count", func() {
		Description("A primitive result, and an error that is a plain string.")
		PayloadWithToken(func() {
			Attribute("fail", Boolean)
		})
		Result(Int)
		Error("teapot", String)
		HTTP(func() {
			GET("/thing-count")
			Param("fail")
			Response("teapot", StatusTeapot)
		})
	})

	Method("ping", func() {
		Description("A read without a result: its query data is null.")
		NoSecurity()
		HTTP(func() {
			GET("/ping")
			Response(StatusNoContent)
		})
	})

	Method("thing_blob", func() {
		Description("A result made of bytes.")
		PayloadWithToken(nil)
		Result(Bytes)
		HTTP(func() {
			GET("/thing-blob")
		})
	})
})
