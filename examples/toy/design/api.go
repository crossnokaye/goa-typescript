package design

import (
	_ "github.com/crossnokaye/goa-typescript/plugin/typescript"
	. "goa.design/goa/v3/dsl"
)

// JWT is the scheme every method requires unless it says otherwise.
var JWT = JWTSecurity("jwt", func() {
	Description("A bearer token in the Authorization header.")
})

// APIKeyAuth is an alternative scheme that travels in the query string.
var APIKeyAuth = APIKeySecurity("api_key", func() {
	Description("A key in the query string.")
})

var _ = API("toy", func() {
	Title("goa-typescript toy service")
	Description("Toy Goa services used to doc-test the goa-typescript plugin output.")
	Version("0.0.1")
	Meta("typescript:package", "toy-client")
	Security(JWT)

	Server("toy", func() {
		Host("localhost", func() {
			URI("http://localhost:0")
		})
	})
})

// PayloadWithToken declares a payload that carries the JWT next to the
// attributes fn defines. The generated client leaves the token out of the
// request type and takes it from its auth option instead.
func PayloadWithToken(fn func()) {
	Payload(func() {
		Token("token", String, "JWT, supplied by the client's auth option")
		if fn != nil {
			fn()
		}
	})
}
