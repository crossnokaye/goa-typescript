package design

import . "goa.design/goa/v3/dsl"

var _ = Service("toy", func() {
	Description("Toy service with one Server-Sent Events method per feature.")

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
})
