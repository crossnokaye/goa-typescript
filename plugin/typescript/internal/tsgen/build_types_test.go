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
	if enum.Input != nil {
		t.Errorf("an enum has no input-side variant, got %+v", enum.Input)
	}

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
	wantEqual(t, "widgets input", widgets.Input.TSDef, "WidgetInput[]")
	wantContains(t, "widget input", widget.Input.TSDef, "priority?: Priority | undefined;", "children?: WidgetsInput | undefined;")

	wantEqual(t, "warnings", pkg.Warnings, []string{
		`Meta("typescript:name", "Position") on attribute "position" is ignored: an inline object attribute cannot be named; declare a Type instead`,
		`Meta("typescript:name", "OneLabel") on attribute "label" is ignored: its type Label already has a name`,
	})

	// A query parameter refers to its declaration unless its parameter
	// encoding differs from the body's: bytes travel as a plain string.
	req := endpoint(t, service(t, pkg, "svc"), "create").Request
	wantContains(t, "request", req.TSDef, "view?: View | undefined;", "cursor?: string | undefined;")
	wantContains(t, "request schema", req.Zod, "view: View.optional(),", "cursor: z.string().optional(),")
	// The request uses the input side of Widget; the result, Widget itself.
	wantEqual(t, "imports", endpoint(t, service(t, pkg, "svc"), "create").Imports.Types, []string{"View", "Widget", "WidgetInput"})
	wantEqual(t, "cursor", typeSpec(t, pkg, "Cursor").TSDef, "Uint8Array")
}

func TestBuildInputs(t *testing.T) {
	pkg := build(t, testdata.InputsDSL)
	inputs := map[string]string{}
	for _, ts := range pkg.Types {
		if ts.Input != nil {
			inputs[ts.Name] = ts.Input.Name
		}
	}
	// Only a type with a defaulted attribute, or one that refers to such a
	// type, has a variant; its name yields to a declared type.
	wantEqual(t, "inputs", inputs, map[string]string{
		"Snapshot": "SnapshotInput2", "Node": "NodeInput", "Report": "ReportInput", "Snapshots": "SnapshotsInput",
	})

	snapshot := typeSpec(t, pkg, "Snapshot")
	wantContains(t, "snapshot", snapshot.TSDef, "pending: boolean;")
	wantContains(t, "snapshot schema", snapshot.Zod, "pending: z.boolean().default(false),")
	wantContains(t, "snapshot input", snapshot.Input.TSDef,
		"Default: false (applied by the server when omitted).", "pending?: boolean | undefined;", "taken_at: Date;")
	wantContains(t, "snapshot input schema", snapshot.Input.Zod, "pending: z.boolean().optional(),", "taken_at: dateTime(),")
	wantEqual(t, "snapshot input kind", snapshot.Input.Kind, TypeObject)

	// A nested reference, an array and a map use the variant; a type without
	// one is referred to as is.
	report := typeSpec(t, pkg, "Report")
	wantContains(t, "report", report.TSDef, "latest: Snapshot;", "history?: Snapshot[] | undefined;")
	wantContains(t, "report input", report.Input.TSDef,
		"latest: SnapshotInput2;", "history?: SnapshotInput2[] | undefined;",
		"by_name?: Record<string, SnapshotInput2> | undefined;", "plain?: Plain | undefined;")
	wantContains(t, "report input schema", report.Input.Zod,
		"latest: SnapshotInput2,", "history: z.array(SnapshotInput2).optional(),",
		"by_name: z.record(z.string(), SnapshotInput2).optional(),", "plain: Plain.optional(),")

	// A recursive type refers to its own variant, lazily.
	node := typeSpec(t, pkg, "Node")
	wantEqual(t, "node recursive", node.Input.Recursive, true)
	wantContains(t, "node input", node.Input.TSDef, "weight?: number | undefined;", "children?: NodeInput[] | undefined;")
	wantContains(t, "node input schema", node.Input.Zod, "children: z.array(NodeInput).optional(),")

	// A named array of such a type.
	wantEqual(t, "snapshots input", typeSpec(t, pkg, "Snapshots").Input.TSDef, "SnapshotInput2[]")

	// Request payloads use the variants, at the top level and inside inline
	// objects; the result keeps the decoded type.
	svc := service(t, pkg, "svc")
	submit := endpoint(t, svc, "submit")
	wantContains(t, "submit request", submit.Request.TSDef,
		"report: ReportInput;", "note?: string | undefined;", "flag?: boolean | undefined;",
		"named?: SnapshotInput | undefined;", "all?: SnapshotsInput | undefined;")
	wantContains(t, "submit request schema", submit.Request.Zod,
		"report: ReportInput,", "note: z.string().optional(),", "flag: z.boolean().optional(),")
	wantEqual(t, "submit imports", submit.Imports.Types, []string{"Plain", "Report", "ReportInput", "SnapshotInput", "SnapshotsInput"})
	wantEqual(t, "submit result", submit.Result.TypeName, "Report")

	node2 := endpoint(t, svc, "submit_node")
	wantContains(t, "submit_node request", node2.Request.TSDef, "weight?: number | undefined;", "children?: NodeInput[] | undefined;")
	wantEqual(t, "submit_node result", node2.Result.TypeName, "Node")
}
