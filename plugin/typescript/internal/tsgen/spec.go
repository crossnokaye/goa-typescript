package tsgen

// The spec structs are the plain data handed to the templates. TypeScript and
// Zod snippets are rendered into strings while building so that the templates
// stay free of type logic.

// PackageSpec describes the whole generated npm package (gen/ts).
type PackageSpec struct {
	// Name is the npm package name.
	Name string
	// Version is the npm package version.
	Version string
	// Description is the npm package description.
	Description string
	// GoaVersion is the version of goa that ran the generation.
	GoaVersion string
	// Types are the hoisted design types, dependencies first.
	Types []*TypeSpec
	// TypeImports is what src/types.ts imports.
	TypeImports Imports
	// Services are the HTTP services, by name.
	Services []*ServiceSpec
	// Runtime are the runtime sources copied into src/runtime.
	Runtime []RuntimeFile
}

// RuntimeFile is one TypeScript runtime source file.
type RuntimeFile struct {
	Name    string
	Content []byte `json:"-"`
}

// Imports lists what a generated file needs from its siblings.
type Imports struct {
	// Zod is true when the file uses the `z` namespace.
	Zod bool
	// Runtime are the names imported from the runtime, sorted. Type-only
	// names carry the "type " prefix.
	Runtime []string
	// Types are the hoisted design types imported from src/types.ts, sorted.
	Types []string
}

// TypeKind says how a hoisted type is declared.
type TypeKind string

const (
	// TypeObject is a type with attributes.
	TypeObject TypeKind = "object"
	// TypeAlias is a named primitive, array or map.
	TypeAlias TypeKind = "alias"
)

// TypeSpec describes one hoisted design type: a TypeScript type and a Zod
// schema of the same name.
type TypeSpec struct {
	// GoaName is the name of the type in the design.
	GoaName string
	// Name is the TypeScript identifier.
	Name        string
	Description string
	Kind        TypeKind
	// TSDef is the right-hand side of the type declaration.
	TSDef string
	// Zod is the schema expression.
	Zod string
	// ZodNote is a trailing comment for the schema declaration.
	ZodNote string
	// Recursive is true when the type takes part in a reference cycle; its
	// schema is then declared lazily with an explicit annotation.
	Recursive bool
	// Props are the properties of an object type.
	Props []PropSpec
}

// PropSpec describes one property of an object type.
type PropSpec struct {
	// Attr is the design attribute name.
	Attr string
	// Key is the property name, which is the JSON key.
	Key         string
	TSType      string
	Description string
	Optional    bool
}

// ServiceSpec describes one HTTP service (src/<dir>).
type ServiceSpec struct {
	// Name is the service name in the design.
	Name string
	// Dir is the directory under src.
	Dir string
	// Namespace is the identifier the root barrel exports the service as.
	Namespace   string
	Description string
	Endpoints   []*EndpointSpec
	// Skipped lists the endpoints for which no function is generated.
	Skipped []SkippedEndpoint
	HasSSE  bool
	HasREST bool
	HasWS   bool
}

// SkippedEndpoint records why an endpoint has no generated function.
type SkippedEndpoint struct {
	Method string
	Reason string
}

// EndpointKind is the transport style of an endpoint.
type EndpointKind string

const (
	EndpointREST      EndpointKind = "rest"
	EndpointSSE       EndpointKind = "sse"
	EndpointRaw       EndpointKind = "raw"
	EndpointWebSocket EndpointKind = "websocket"
)

// EndpointSpec describes one generated function (src/<service>/<file>.ts).
type EndpointSpec struct {
	// MethodName is the method name in the design.
	MethodName string
	// FuncName is the exported function name.
	FuncName string
	// FileName is the file name without extension.
	FileName    string
	Description string
	Kind        EndpointKind
	// Mixed is true when the method offers both a plain and a streamed result.
	Mixed bool
	// Verb is the HTTP method.
	Verb string
	// PathTemplate is the goa route, e.g. "/things/{id}".
	PathTemplate string
	// Accept is the Accept header sent with the request.
	Accept string
	// SpecName is the constant holding the endpoint's transport mapping.
	SpecName string
	// SpecTS is the literal assigned to that constant.
	SpecTS string
	// ErrorsName is the exported constant holding the error table.
	ErrorsName string
	// Request is nil when the method takes no payload.
	Request   *RequestSpec
	Responses []*ResponseSpec
	Errors    []*ErrorSpec
	// ErrorTypeName is the name of the endpoint's error union.
	ErrorTypeName string
	// ErrorTS is the definition of the error union.
	ErrorTS string
	// ErrorTable is the literal passed to the error decoder.
	ErrorTable string
	// ResultTS is the success type of the function.
	ResultTS string
	Security []SecurityBinding
	SSE      *SSESpec
	WS       *WebSocketSpec
	// Imports is what the endpoint file imports.
	Imports Imports
}

// BodyKind says which part of the request becomes the HTTP body.
type BodyKind string

const (
	BodyNone   BodyKind = "none"
	BodyFields BodyKind = "fields"
	BodyAttr   BodyKind = "attr"
	BodyWhole  BodyKind = "whole"
	BodyRaw    BodyKind = "raw"
)

// BodySpec describes the HTTP request body.
type BodySpec struct {
	Kind BodyKind
	// Fields are the request properties sent as a JSON object (BodyFields).
	Fields []string
	// Attr is the request property sent as the body (BodyAttr).
	Attr string
}

// RequestSpec describes the request argument of a generated function.
type RequestSpec struct {
	// TypeName names both the request type and its schema.
	TypeName string
	// TSDef is the right-hand side of the type declaration.
	TSDef string
	// Zod is the schema expression.
	Zod string
	// ZodNote is a trailing comment for the schema declaration.
	ZodNote string
	// Scalar is true when the payload is not an object.
	Scalar  bool
	Path    []ParamSpec
	Query   []ParamSpec
	Headers []ParamSpec
	Cookies []ParamSpec
	// MapQuery is the property whose entries spread into the query string;
	// its Prop is "" when the payload itself is the map.
	MapQuery  *ParamSpec
	Body      BodySpec
	Multipart bool
}

// ParamSpec maps a request property to an HTTP element.
type ParamSpec struct {
	// Attr is the design attribute name.
	Attr string
	// Prop is the request property that holds the value.
	Prop string
	// Wire is the name of the path parameter, query key, header or cookie.
	Wire     string
	Required bool
	// Shape is the kind of value: "scalar", "array" or "map".
	Shape string
}

// SecurityBinding says where a security scheme puts its credential.
type SecurityBinding struct {
	// Scheme is the scheme name in the design.
	Scheme string
	// Kind is "bearer", "apiKey" or "basic".
	Kind string
	// In is "header" or "query".
	In string
	// Name is the header or query parameter name.
	Name string
	// Attr is the payload attribute the credential replaces.
	Attr string
}

// ResponseSpec describes one success response (REST endpoints).
type ResponseSpec struct {
	Status      int
	ContentType string
	Body        BodyKind
	Attr        string
	Headers     []ParamSpec
	Tag         [2]string
	ResultTS    string
	SchemaExpr  string
}

// ErrorSpec describes one error an endpoint declares.
type ErrorSpec struct {
	Name   string
	Status int
	// Default is true when the error uses goa's default error type.
	Default bool
	// TypeName is the hoisted type of a custom error.
	TypeName  string
	Headers   []ParamSpec
	Temporary bool
	Timeout   bool
	Fault     bool
}

// SSESpec describes how an endpoint maps events onto Server-Sent Events.
type SSESpec struct {
	// EventTypeName names both the event type and its schema.
	EventTypeName string
	// EventInline is true when the event type is declared in the endpoint
	// file because the design does not name it.
	EventInline bool
	// EventTSDef and EventZod define an inline event type.
	EventTSDef   string
	EventZod     string
	EventZodNote string
	// DataField is the JSON key of the attribute sent in the data field, or
	// "" when the whole event is.
	DataField string
	// DataEncoding is how goa writes the data: "string", "boolean", "int",
	// "float", "bytes" or "json".
	DataEncoding string
	// IDField, EventField and RetryField are the JSON keys of the attributes
	// sent in those SSE fields, if any.
	IDField    string
	EventField string
	RetryField string
	// RequestIDField is the payload attribute goa maps to Last-Event-ID.
	RequestIDField string
}

// WebSocketSpec describes a WebSocket endpoint.
type WebSocketSpec struct {
	SendTypeName string
	RecvTypeName string
	ServerOnly   bool
}
