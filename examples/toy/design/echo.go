package design

import . "goa.design/goa/v3/dsl"

// The echo service reuses a method name and a type of the toy service: each
// service gets its own module in the generated client.
var _ = Service("echo", func() {
	Description("Echo service that shares names with the toy service.")

	Error("unauthorized")

	HTTP(func() {
		Path("/echo")
		Response("unauthorized", StatusUnauthorized)
	})

	Method("stream_things_sse", func() {
		Description("Streams a single event that echoes the identifier.")
		PayloadWithToken(func() {
			Attribute("id", String, "Value to echo")
			Required("id")
		})
		StreamingResult(ThingEvent)
		HTTP(func() {
			GET("/things/{id}/events")
			ServerSentEvents()
		})
	})
})
