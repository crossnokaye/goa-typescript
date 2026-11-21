package tsgen

import (
	"strings"
	"testing"

	. "goa.design/goa/v3/dsl"
	"goa.design/goa/v3/expr"
)

// unsupportedFlatten returns a design whose nested union opts into
// oneof:json:flatten.
func unsupportedFlatten(optOut bool) func() {
	return func() {
		var Done = Type("Done", func() { Attribute("reference", String) })
		var Fail = Type("Fail", func() { Attribute("reason", String) })
		var Outcome = Type("Outcome", func() {
			OneOf("result", func() {
				Meta("oneof:json:flatten")
				Attribute("done", Done)
				Attribute("fail", Fail)
			})
		})
		Service("svc", func() {
			if optOut {
				Meta("typescript:generate", "false")
			}
			Method("get", func() {
				// Nested in an array, so the check has to walk into it.
				Result(ArrayOf(Outcome))
				HTTP(func() { GET("/get") })
			})
		})
	}
}

// unsupportedUntagged returns a design whose inline payload union opts into
// oneof:json:untagged.
func unsupportedUntagged(optOut bool) func() {
	return func() {
		Service("svc", func() {
			if optOut {
				Meta("typescript:generate", "false")
			}
			Method("put", func() {
				Payload(func() {
					OneOf("value", func() {
						Meta("oneof:json:untagged")
						Attribute("name", String)
						Attribute("items", ArrayOf(Int))
					})
				})
				HTTP(func() { PUT("/put") })
			})
		})
	}
}

// unsupportedBodyFieldName returns a design whose service renames a body field.
func unsupportedBodyFieldName(optOut bool) func() {
	return func() {
		var Rec = Type("Rec", func() { Attribute("label_text", String) })
		Service("svc", func() {
			if optOut {
				Meta("typescript:generate", "false")
			}
			HTTP(func() { BodyFieldName(Rec, "label_text:labelText") })
			Method("get", func() {
				Result(Rec)
				HTTP(func() { GET("/get") })
			})
		})
	}
}

func TestBuildRejectsUnsupportedJSONMappings(t *testing.T) {
	for name, c := range map[string]struct {
		dsl  func(optOut bool) func()
		want string
	}{
		"flatten":       {unsupportedFlatten, `OneOf "result": Meta("oneof:json:flatten") is not supported`},
		"untagged":      {unsupportedUntagged, `OneOf "value": Meta("oneof:json:untagged") is not supported`},
		"BodyFieldName": {unsupportedBodyFieldName, `service "svc": BodyFieldName is not supported`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Build([]*expr.RootExpr{design(t, c.dsl(false))}, Options{})
			if err == nil {
				t.Fatalf("Build succeeded, want an error containing %q", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err, c.want)
			}
		})
		// A service left out of the client may use them.
		t.Run(name+" opted out", func(t *testing.T) {
			if _, err := Build([]*expr.RootExpr{design(t, c.dsl(true))}, Options{}); err != nil {
				t.Errorf("Build: %v", err)
			}
		})
	}
}
