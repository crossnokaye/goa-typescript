package tsgen

import (
	"strings"
	"testing"

	"github.com/xeger/goa-typescript/plugin/typescript/internal/tsgen/testdata"
)

// typeSpec returns the hoisted type with the given name.
func typeSpec(t *testing.T, pkg *PackageSpec, name string) *TypeSpec {
	t.Helper()
	for _, ts := range pkg.Types {
		if ts.Name == name {
			return ts
		}
	}
	t.Fatalf("type %q not found in %v", name, typeNames(pkg))
	return nil
}

func wantContains(t *testing.T, what, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("%s lacks %q:\n%s", what, want, got)
		}
	}
}

func TestBuildNamedAttrs(t *testing.T) {
	pkg := build(t, testdata.NamedAttrsDSL)
	wantEqual(t, "types", typeNames(pkg), []string{
		"ColorKey", "ColorKey2", "Cursor", "Kind", "Label", "Labels", "Priority", "Scores", "Shape", "View",
		"Widget", "Widgets",
	})

	// A declared type keeps its name; the attribute that asks for the same
	// one is renamed.
	wantEqual(t, "declared kind", typeSpec(t, pkg, "ColorKey").Kind, TypeObject)
	enum := typeSpec(t, pkg, "ColorKey2")
	wantEqual(t, "enum kind", enum.Kind, TypeAlias)
	wantEqual(t, "enum goa name", enum.GoaName, "ColorKey")
	wantEqual(t, "enum description", enum.Description, "The key of the widget's color.")
	wantEqual(t, "enum ts", enum.TSDef, `"blue" | "green"`)
	wantEqual(t, "enum zod", enum.Zod, `z.enum(["blue", "green"])`)

	// The declaration carries the validations; the parent applies the default.
	wantEqual(t, "priority zod", typeSpec(t, pkg, "Priority").Zod, "z.int().gte(1).lte(5)")
	widget := typeSpec(t, pkg, "Widget")
	wantContains(t, "widget type", widget.TSDef,
		"color_key: ColorKey2;", "priority: Priority;", "labels?: Labels | undefined;",
		"scores?: Scores | undefined;", "kinds?: Kind[] | undefined;", "children?: Widgets | undefined;",
		"shape?: Shape | undefined;",
		// An inline object and a reference to a declared type stay as they are.
		"position?: {", "label?: Label | undefined;")
	wantContains(t, "widget schema", widget.Zod,
		"color_key: ColorKey2,", "priority: Priority.default(3),", "kinds: z.array(Kind).optional(),",
		"shape: Shape.optional(),", "position: z.object({")
	wantEqual(t, "labels", typeSpec(t, pkg, "Labels").TSDef, "Label[]")
	wantEqual(t, "scores", typeSpec(t, pkg, "Scores").Zod, "z.record(z.string(), z.number())")

	// A named array of the parent takes part in the cycle.
	widgets := typeSpec(t, pkg, "Widgets")
	wantEqual(t, "widgets recursive", widgets.Recursive, true)
	wantEqual(t, "widgets ts", widgets.TSDef, "Widget[]")

	wantEqual(t, "warnings", pkg.Warnings, []string{
		`Meta("typescript:name", "Position") on attribute "position" is ignored: an inline object attribute cannot be named; declare a Type instead`,
		`Meta("typescript:name", "OneLabel") on attribute "label" is ignored: its type Label already has a name`,
	})

	// A query parameter refers to its declaration unless its parameter
	// encoding differs from the body's: bytes travel as a plain string.
	req := endpoint(t, service(t, pkg, "svc"), "create").Request
	wantContains(t, "request", req.TSDef, "view?: View | undefined;", "cursor?: string | undefined;")
	wantContains(t, "request schema", req.Zod, "view: View.optional(),", "cursor: z.string().optional(),")
	wantEqual(t, "imports", endpoint(t, service(t, pkg, "svc"), "create").Imports.Types, []string{"View", "Widget"})
	wantEqual(t, "cursor", typeSpec(t, pkg, "Cursor").TSDef, "Uint8Array")
}
