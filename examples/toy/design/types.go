package design

import . "goa.design/goa/v3/dsl"

var Label = Type("Label", func() {
	Description("A key/value label.")
	Attribute("key", String)
	Attribute("value", String)
	Required("key", "value")
})

var Circle = Type("Circle", func() {
	Attribute("radius", Float64)
	Required("radius")
})

var Square = Type("Square", func() {
	Attribute("side", Float64)
	Required("side")
})

// Thing uses every type feature the generator maps.
var Thing = Type("Thing", func() {
	Description("A thing with one attribute per type feature.")
	Attribute("id", String, "Unique identifier", func() {
		Format(FormatUUID)
	})
	Attribute("name", String, "Display name", func() {
		MinLength(1)
		MaxLength(32)
		Pattern(`^[a-z][a-z0-9-]*$`)
	})
	Attribute("kind", String, "Kind of thing", func() {
		Enum("widget", "gadget")
		Default("widget")
	})
	Attribute("enabled", Boolean, func() {
		Default(true)
	})
	Attribute("created_at", String, "Creation time", func() {
		Format(FormatDateTime)
	})
	Attribute("born_on", String, "Calendar date, which stays a string", func() {
		Format(FormatDate)
	})
	Attribute("count", Int64, func() {
		Minimum(0)
	})
	Attribute("small", Int32)
	Attribute("ratio", Float64)
	Attribute("weight", Float32)
	Attribute("blob", Bytes)
	Attribute("payload", Any, "Free-form JSON")
	Attribute("tags", ArrayOf(String), func() {
		MinLength(1)
		MaxLength(4)
	})
	Attribute("attrs", MapOf(String, Int))
	Attribute("labels", ArrayOf(Label))
	Attribute("parent", "Thing", "The thing this one derives from")
	OneOf("shape", func() {
		Attribute("circle", Circle)
		Attribute("square", Square)
	})
	Attribute("legacy_name", String, "Serialized under a different JSON key", func() {
		Meta("struct:tag:json", "legacyName")
	})
	Required("id")
})

var ThingWithViews = ResultType("application/vnd.goa-typescript.thing", func() {
	TypeName("ThingWithViews")
	Description("A thing whose secret only shows in the extended view.")
	Attributes(func() {
		Attribute("id", String)
		Attribute("name", String)
		Attribute("secret", String)
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
	})
})

var ThingEvent = Type("ThingEvent", func() {
	Description("An event about a thing, sent as the whole SSE data field.")
	Meta("struct:pkg:path", "types")
	Attribute("type", String, func() {
		Default("thing")
	})
	Attribute("id", String, "Event identifier")
	Attribute("thing", Thing)
	Attribute("auth_seen", Boolean, "Whether the request carried an Authorization header")
	Required("id")
})

var ThingEventEnvelope = Type("ThingEventEnvelope", func() {
	Description("An event whose attributes map onto the SSE data, id, event and retry fields.")
	Attribute("data", Thing)
	Attribute("id", String)
	Attribute("event", String)
	Attribute("retry", Int)
	Required("data")
})

var ThingFilter = Type("ThingFilter", func() {
	Description("Selects the things to stream.")
	Attribute("kinds", ArrayOf(String, func() {
		Enum("widget", "gadget")
	}))
	Attribute("since", String, func() {
		Format(FormatDateTime)
	})
})
