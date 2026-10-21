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
	// Warnings are remarks about the design that `goa gen` prints: the client
	// is generated, but part of it cannot work as designed.
	Warnings []string
	// Query is true when Meta("typescript:query", "tanstack") enables the
	// TanStack Query layer.
	Query bool
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
	// TypeAlias is a named primitive, array, map or union.
	TypeAlias TypeKind = "alias"
)

// TypeSpec describes one hoisted design type: a TypeScript type and a Zod
// schema of the same name.
type TypeSpec struct {
	// GoaName is the name of the type in the design, or the name
	// Meta("typescript:name") gives an attribute.
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
	// Request is the request-side variant of the type, declared beside it: the
	// same type with every defaulted attribute optional, referring to the
	// request-side variants of the types it uses. It is nil when the variant
	// would not differ from the type.
	Request *TypeSpec `json:",omitempty"`
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
	// QueryNone lists the generated methods that get no query options, with
	// the reason; empty unless the TanStack Query layer is enabled.
	QueryNone []SkippedEndpoint
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

// EndpointSpec describes the file generated for one method
// (src/<service>/<file>.ts). It holds one function, or two when the method
// offers both a plain and a streamed result.
type EndpointSpec struct {
	// MethodName is the method name in the design.
	MethodName string
	// FuncName is the exported function name. For a mixed method it is the
	// function that returns the plain result.
	FuncName string
	// FileName is the file name without extension.
	FileName    string
	Description string
	// Kind is the style of the function named FuncName.
	Kind EndpointKind
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
	// Stream is the Server-Sent Events sibling of a mixed method's plain
	// function; nil otherwise.
	Stream *StreamFuncSpec
	// ErrorsName is the exported constant holding the error table.
	ErrorsName string
	// Request is nil when the method takes no payload.
	Request *RequestSpec
	// RawBody is true when the function takes the request body as an extra
	// argument and sends it as is (SkipRequestBodyEncodeDecode).
	RawBody bool
	// Result is the value of a REST function; nil for an SSE-only endpoint.
	Result *ResultSpec
	// Responses are the success responses of a REST function, by status.
	Responses []*ResponseSpec
	// ResponsesName is the constant holding the response table.
	ResponsesName string
	// ResponsesTS is the literal assigned to that constant.
	ResponsesTS string
	Errors      []*ErrorSpec
	// ErrorTypeName is the name of the endpoint's error union.
	ErrorTypeName string
	// ErrorTS is the definition of the error union.
	ErrorTS string
	// ErrorTable is the literal passed to the error decoder.
	ErrorTable string
	// ResultTS is the success type of the function named FuncName.
	ResultTS string
	Security []SecurityBinding
	SSE      *SSESpec
	WS       *WebSocketSpec
	// Imports is what the endpoint file imports.
	Imports Imports
	// Queries are the TanStack Query exports of the method, rendered at the
	// end of the endpoint file: the plain function's, then the stream's.
	// Empty unless the layer is enabled and the method is classified.
	Queries []*QuerySpec
}

// QueryKind is how the TanStack Query layer classifies a generated function.
type QueryKind string

const (
	// QueryRead is a read: query options and a key.
	QueryRead QueryKind = "query"
	// QueryStream is a stream: streamed query options and a key.
	QueryStream QueryKind = "stream"
	// QueryMutation is a write: mutation options and the keys it invalidates.
	QueryMutation QueryKind = "mutation"
	// QueryNone gets nothing.
	QueryNone QueryKind = "none"
)

// QuerySpec describes the TanStack Query exports of one generated function.
type QuerySpec struct {
	Kind QueryKind
	// FuncName and SpecName are the SDK function and its transport mapping.
	FuncName string
	SpecName string
	// Verb and Path are the function's HTTP method and goa route.
	Verb string
	Path string
	// RequestName is the request schema, "" when the method takes no payload.
	RequestName string
	// Required is true when the design requires a payload.
	Required bool
	// Raw is true for a raw upload: the variables are { payload, body }.
	Raw bool
	// Name is xyzQuery or xyzMutation.
	Name string
	// KeyName is xyzKey (queries and streams).
	KeyName string
	// InvalidatesName is xyzInvalidates (mutations).
	InvalidatesName string
	// Discriminator is "method" when the key carries $method because another
	// query of the same kind shares its route; "" for a clean key. A stream's
	// key always carries $stream besides.
	Discriminator string
	// Collisions describe the queries that share the path, as `VERB /path` (`name`).
	Collisions []string
	// Invalidates are the path templates of the keys a mutation invalidates.
	Invalidates []string
	// InvalidatesFallback records, by template, the routes named with
	// Meta("typescript:invalidates") that the mutation cannot fill in whole:
	// their run-time key stops before the first parameter it lacks.
	InvalidatesFallback map[string]InvalidationFallback `json:",omitempty"`
	// InvalidatesNoQuery is true when the default invalidation reaches no
	// read: `goa gen` warns about it.
	InvalidatesNoQuery bool `json:",omitempty"`
	// defaultInvalidation is true when the mutation declares no
	// Meta("typescript:invalidates"): finishQueries computes Invalidates.
	defaultInvalidation bool
	// Void is true for a read whose method has no result: its data is null.
	Void bool `json:",omitempty"`
	// InvalidatedBy are the mutations whose invalidation matches this key.
	InvalidatedBy []string
	// Source is the shape of the key, for the state card.
	Source string
	// Decls are the rendered exports: JSDoc and one line of code each.
	Decls []QueryDecl
}

// InvalidationFallback describes a named route whose key a mutation cannot
// fill in whole: `goa gen` warns about it.
type InvalidationFallback struct {
	// Query is the method named in Meta("typescript:invalidates").
	Query string
	// Param is the first parameter of its route that is not a path
	// parameter of the mutation.
	Param string
	// Prefix is the template of the key the mutation invalidates instead.
	Prefix string
}

// QueryDecl is one query export of an endpoint file.
type QueryDecl struct {
	Doc  string
	Code string
}

// StreamFuncSpec names the Server-Sent Events function of a mixed method. It
// has its own transport mapping because its Accept header differs.
type StreamFuncSpec struct {
	FuncName string
	SpecName string
	SpecTS   string
	Accept   string
}

// ResultSpec describes the value a REST function returns.
type ResultSpec struct {
	// TypeName names the type and its schema; "" when the method has no
	// result and the value is void.
	TypeName string
	// Inline is true when the type is declared in the endpoint file because
	// the design does not name it.
	Inline bool
	// TSDef and Zod define an inline type.
	TSDef   string
	Zod     string
	ZodNote string
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
	Scalar bool
	// Optional is true when the payload argument may be omitted: the payload
	// is an object none of whose properties is required. A scalar payload is
	// never optional, nor is the payload of a function that takes a raw body,
	// because an optional parameter cannot precede a required one.
	Optional bool
	Path     []ParamSpec
	Query    []ParamSpec
	Headers  []ParamSpec
	Cookies  []ParamSpec
	// Props are the request properties, with whether each may be absent.
	Props []RequestProp
	// MapQuery is the property whose entries spread into the query string;
	// its Prop is "" when the payload itself is the map.
	MapQuery  *ParamSpec
	Body      BodySpec
	Multipart bool
}

// RequestProp is one property of the request type.
type RequestProp struct {
	Key      string
	Optional bool
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

// HeaderSpec maps a response header onto an attribute.
type HeaderSpec struct {
	// Attr is the design attribute name.
	Attr string
	// Key is the JSON key of the attribute, which is its property name in
	// the decoded value; "" when the header carries the whole value.
	Key string
	// Name is the HTTP header name.
	Name string
	// Shape is the kind of value: "string", "number", "boolean" or one of
	// those followed by "[]".
	Shape string
}

// ResponseSpec describes one success response (REST endpoints).
type ResponseSpec struct {
	Status      int
	ContentType string
	// Body says which part of the result the response body holds.
	Body BodyKind
	// Attr is the JSON key of the attribute a BodyAttr body holds.
	Attr string
	// NullAs is what a null BodyWhole body stands for: "array", "map" or "".
	NullAs string
	// Scalar is true when the result is not an object: the body, or the
	// single header, is the value itself.
	Scalar  bool
	Headers []HeaderSpec
	// Cookies are the JSON keys of the attributes sent as cookies, which a
	// browser never shows to script.
	Cookies []string
	// Tag is the JSON key of the tag attribute and the value this response
	// gives it.
	Tag [2]string
	// Missing are the JSON keys of the attributes the result requires and
	// this response does not carry: decoding it fails validation.
	Missing []string
}

// ErrorSpec describes one error an endpoint declares.
type ErrorSpec struct {
	Name   string
	Status int
	// Default is true when the error uses goa's default error type.
	Default bool
	// TypeName names the type of a custom error and its schema.
	TypeName string
	// Inline is true when that type is declared in the endpoint file because
	// the design does not name it.
	Inline bool
	// TSDef and Zod define an inline type.
	TSDef   string
	Zod     string
	ZodNote string
	// Headers are the attributes of a custom error sent as response headers.
	Headers   []HeaderSpec
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

// WebSocketSpec describes the frames of a WebSocket endpoint.
type WebSocketSpec struct {
	// Kind is the goa stream kind: "server", "client" or "bidirectional".
	Kind string
	// RecvTypeName names the type and schema of the messages the server
	// sends; "" when it sends none.
	RecvTypeName string
	// RecvInline is true when that type is declared in the endpoint file
	// because the design does not name it.
	RecvInline bool
	// RecvTSDef and RecvZod define an inline received type.
	RecvTSDef   string
	RecvZod     string
	RecvZodNote string
	// SendTypeName names the type and schema of the messages the client
	// sends; "" for a server stream.
	SendTypeName string
	// SendInline is true when that type is declared in the endpoint file.
	SendInline bool
	// SendTSDef and SendZod define an inline sent type.
	SendTSDef   string
	SendZod     string
	SendZodNote string
	// Single is true when the server sends one message, then drops the
	// connection: the result of a client stream.
	Single bool
	// HeaderCredentials are the security schemes whose credential travels in
	// a header, which the browser WebSocket API cannot send.
	HeaderCredentials []SecurityBinding
	// HeaderParams are the request properties mapped to headers, likewise.
	HeaderParams []ParamSpec
	// DeclaredErrors are the names of the errors the design declares, which
	// the client cannot tell apart from a failed connection.
	DeclaredErrors []string
}

// RecvTS is the TypeScript type of the received messages.
func (w *WebSocketSpec) RecvTS() string {
	if w.RecvTypeName == "" {
		return "never"
	}
	return w.RecvTypeName
}

// SendTS is the TypeScript type of the sent messages.
func (w *WebSocketSpec) SendTS() string {
	if w.SendTypeName == "" {
		return "never"
	}
	return w.SendTypeName
}
