package tsgen

import (
	"testing"

	"goa.design/goa/v3/expr"
)

func ptr[T any](v T) *T { return &v }

func str(v *expr.ValidationExpr) *expr.AttributeExpr {
	return &expr.AttributeExpr{Type: expr.String, Validation: v}
}

// TestTypeAndSchema covers the goa -> TypeScript -> Zod table for attributes
// that need no hoisted type.
func TestTypeAndSchema(t *testing.T) {
	label := &expr.Object{
		{Name: "text", Attribute: &expr.AttributeExpr{Type: expr.String}},
		{Name: "at", Attribute: str(&expr.ValidationExpr{Format: expr.FormatDateTime})},
	}
	cases := []struct {
		name  string
		att   *expr.AttributeExpr
		param bool
		ts    string
		zod   string
		note  string
		rt    []string
	}{
		{name: "boolean", att: &expr.AttributeExpr{Type: expr.Boolean}, ts: "boolean", zod: "z.boolean()"},
		{name: "int", att: &expr.AttributeExpr{Type: expr.Int}, ts: "number", zod: "z.int()"},
		{name: "int32", att: &expr.AttributeExpr{Type: expr.Int32}, ts: "number", zod: "z.int32()"},
		{name: "int64", att: &expr.AttributeExpr{Type: expr.Int64}, ts: "number", zod: "z.int()"},
		{name: "uint", att: &expr.AttributeExpr{Type: expr.UInt}, ts: "number", zod: "z.int().min(0)"},
		{name: "uint32", att: &expr.AttributeExpr{Type: expr.UInt32}, ts: "number", zod: "z.uint32()"},
		{name: "uint64", att: &expr.AttributeExpr{Type: expr.UInt64}, ts: "number", zod: "z.int().min(0)"},
		{name: "float32", att: &expr.AttributeExpr{Type: expr.Float32}, ts: "number", zod: "z.number()"},
		{name: "float64", att: &expr.AttributeExpr{Type: expr.Float64}, ts: "number", zod: "z.number()"},
		{name: "string", att: &expr.AttributeExpr{Type: expr.String}, ts: "string", zod: "z.string()"},
		{name: "bytes", att: &expr.AttributeExpr{Type: expr.Bytes}, ts: "Uint8Array", zod: "bytes()", rt: []string{"bytes"}},
		{name: "bytes param", att: &expr.AttributeExpr{Type: expr.Bytes}, param: true, ts: "string", zod: "z.string()"},
		{name: "any", att: &expr.AttributeExpr{Type: expr.Any}, ts: "unknown", zod: "z.unknown()"},
		{name: "any param", att: &expr.AttributeExpr{Type: expr.Any}, param: true, ts: "string", zod: "z.string()"},

		{name: "date-time", att: str(&expr.ValidationExpr{Format: expr.FormatDateTime}), ts: "Date", zod: "dateTime()", rt: []string{"dateTime"}},
		{name: "date-time param", att: str(&expr.ValidationExpr{Format: expr.FormatDateTime}), param: true, ts: "Date", zod: "dateTime()", rt: []string{"dateTime"}},
		{name: "date", att: str(&expr.ValidationExpr{Format: expr.FormatDate}), ts: "string", zod: "z.iso.date()"},
		{name: "uuid", att: str(&expr.ValidationExpr{Format: expr.FormatUUID}), ts: "string", zod: "z.uuid()"},
		{name: "email", att: str(&expr.ValidationExpr{Format: expr.FormatEmail}), ts: "string", zod: "z.email()"},
		{name: "hostname", att: str(&expr.ValidationExpr{Format: expr.FormatHostname}), ts: "string", zod: "z.hostname()"},
		{name: "ipv4", att: str(&expr.ValidationExpr{Format: expr.FormatIPv4}), ts: "string", zod: "z.ipv4()"},
		{name: "ipv6", att: str(&expr.ValidationExpr{Format: expr.FormatIPv6}), ts: "string", zod: "z.ipv6()"},
		{name: "ip", att: str(&expr.ValidationExpr{Format: expr.FormatIP}), ts: "string", zod: "z.union([z.ipv4(), z.ipv6()])"},
		{name: "cidr", att: str(&expr.ValidationExpr{Format: expr.FormatCIDR}), ts: "string", zod: "z.union([z.cidrv4(), z.cidrv6()])"},
		{name: "uri", att: str(&expr.ValidationExpr{Format: expr.FormatURI}), ts: "string", zod: "z.string()", note: "format: uri"},
		{name: "mac", att: str(&expr.ValidationExpr{Format: expr.FormatMAC}), ts: "string", zod: "z.string()", note: "format: mac"},
		{name: "regexp", att: str(&expr.ValidationExpr{Format: expr.FormatRegexp}), ts: "string", zod: "z.string()", note: "format: regexp"},
		{name: "json", att: str(&expr.ValidationExpr{Format: expr.FormatJSON}), ts: "string", zod: "z.string()", note: "format: json"},
		{name: "rfc1123", att: str(&expr.ValidationExpr{Format: expr.FormatRFC1123}), ts: "string", zod: "z.string()", note: "format: rfc1123"},

		{name: "string enum", att: str(&expr.ValidationExpr{Values: []any{"a", "b"}}), ts: `'a' | 'b'`, zod: `z.enum(['a', 'b'])`},
		{
			name: "int enum", att: &expr.AttributeExpr{Type: expr.Int, Validation: &expr.ValidationExpr{Values: []any{1, 2}}},
			ts: "1 | 2", zod: "z.literal([1, 2])",
		},
		{
			name: "float enum", att: &expr.AttributeExpr{Type: expr.Float64, Validation: &expr.ValidationExpr{Values: []any{0.5, 1.0}}},
			ts: "0.5 | 1", zod: "z.literal([0.5, 1])",
		},
		{
			name: "enum wins over format and length",
			att:  str(&expr.ValidationExpr{Values: []any{"a"}, Format: expr.FormatUUID, MinLength: ptr(1)}),
			ts:   `'a'`, zod: `z.enum(['a'])`,
		},
		{
			name: "string lengths and pattern",
			att:  str(&expr.ValidationExpr{MinLength: ptr(1), MaxLength: ptr(8), Pattern: `^\d+$`}),
			ts:   "string", zod: `z.string().min(1).max(8).regex(new RegExp('^\\d+$'))`,
		},
		{
			name: "format with length", att: str(&expr.ValidationExpr{Format: expr.FormatEmail, MaxLength: ptr(64)}),
			ts: "string", zod: "z.email().max(64)",
		},
		{
			name: "RE2-only pattern", att: str(&expr.ValidationExpr{Pattern: `(?P<year>\d{4})`}),
			ts: "string", zod: "z.string()", note: `pattern not enforced (RE2-only syntax): (?P<year>\d{4})`,
		},
		{
			name: "number range",
			att:  &expr.AttributeExpr{Type: expr.Float64, Validation: &expr.ValidationExpr{Minimum: ptr(0.5), Maximum: ptr(1e6)}},
			ts:   "number", zod: "z.number().gte(0.5).lte(1000000)",
		},
		{
			name: "exclusive range",
			att: &expr.AttributeExpr{Type: expr.Int, Validation: &expr.ValidationExpr{
				ExclusiveMinimum: ptr(0.0), ExclusiveMaximum: ptr(10.0),
			}},
			ts: "number", zod: "z.int().gt(0).lt(10)",
		},

		{
			name: "array", att: &expr.AttributeExpr{Type: &expr.Array{ElemType: &expr.AttributeExpr{Type: expr.String}}},
			ts: "string[]", zod: "z.array(z.string())",
		},
		{
			name: "array lengths",
			att: &expr.AttributeExpr{
				Type:       &expr.Array{ElemType: &expr.AttributeExpr{Type: expr.Int}},
				Validation: &expr.ValidationExpr{MinLength: ptr(1), MaxLength: ptr(3)},
			},
			ts: "number[]", zod: "z.array(z.int()).min(1).max(3)",
		},
		{
			name: "array of enum",
			att:  &expr.AttributeExpr{Type: &expr.Array{ElemType: str(&expr.ValidationExpr{Values: []any{"a", "b"}})}},
			ts:   `('a' | 'b')[]`, zod: `z.array(z.enum(['a', 'b']))`,
		},
		{
			name: "array keeps element notes",
			att:  &expr.AttributeExpr{Type: &expr.Array{ElemType: str(&expr.ValidationExpr{Format: expr.FormatURI})}},
			ts:   "string[]", zod: "z.array(z.string())", note: "format: uri",
		},
		{
			name: "map with non-string keys",
			att: &expr.AttributeExpr{Type: &expr.Map{
				KeyType:  &expr.AttributeExpr{Type: expr.Int},
				ElemType: str(&expr.ValidationExpr{Format: expr.FormatDateTime}),
			}},
			ts: "Record<string, Date>", zod: "z.record(z.string(), dateTime())", rt: []string{"dateTime"},
		},
		{
			name: "inline object",
			att:  &expr.AttributeExpr{Type: label, Validation: &expr.ValidationExpr{Required: []string{"text"}}},
			ts:   "{\n  text: string;\n  at?: Date | undefined;\n}",
			zod:  "z.object({\n  text: z.string(),\n  at: dateTime().optional(),\n})",
			rt:   []string{"dateTime"},
		},
		{name: "empty object", att: &expr.AttributeExpr{Type: &expr.Object{}}, ts: "Record<string, never>", zod: "z.object({})"},
		{
			name: "union",
			att: &expr.AttributeExpr{Type: &expr.Union{TypeName: "shape", Values: []*expr.NamedAttributeExpr{
				{Name: "label", Attribute: &expr.AttributeExpr{Type: expr.String}},
				{Name: "count", Attribute: &expr.AttributeExpr{Type: expr.Int}},
			}}},
			ts:  `{ type: 'label'; value: string } | { type: 'count'; value: number }`,
			zod: "z.discriminatedUnion('type', [\n  z.object({ type: z.literal('label'), value: z.string() }),\n  z.object({ type: z.literal('count'), value: z.int() }),\n])",
		},
		{
			name: "union with custom keys",
			att: &expr.AttributeExpr{Type: &expr.Union{TypeName: "pay", TypeKey: "kind", ValueKey: "details", Values: []*expr.NamedAttributeExpr{
				{Name: "cash", Attribute: &expr.AttributeExpr{Type: expr.Int}},
			}}},
			ts:  `{ kind: 'cash'; details: number }`,
			zod: "z.discriminatedUnion('kind', [\n  z.object({ kind: z.literal('cash'), details: z.int() }),\n])",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			refs := newRefs()
			c := &renderCtx{m: newTypeMapper(), refs: refs}
			if got := c.tsType(tc.att, "", tc.param); got != tc.ts {
				t.Errorf("TypeScript:\n got %s\nwant %s", got, tc.ts)
			}
			z := c.zod(tc.att, "", tc.param)
			if z.code != tc.zod {
				t.Errorf("Zod:\n got %s\nwant %s", z.code, tc.zod)
			}
			if z.note() != tc.note {
				t.Errorf("note: got %q, want %q", z.note(), tc.note)
			}
			got := refs.imports().Runtime
			if len(got) != len(tc.rt) {
				t.Fatalf("runtime imports: got %v, want %v", got, tc.rt)
			}
			for i := range got {
				if got[i] != tc.rt[i] {
					t.Errorf("runtime imports: got %v, want %v", got, tc.rt)
				}
			}
		})
	}
}

func TestJSCompatiblePattern(t *testing.T) {
	cases := map[string]bool{
		`^[a-z][a-z0-9-]*$`:       true,
		`^\d{3}-\d{4}$`:           true,
		`(?:a|b)+`:                true,
		`^(?=.*\d)`:               true, // Go rejects lookaheads itself; nothing to translate.
		`\bword\b`:                true,
		`(?P<year>\d{4})`:         false,
		`(?i)abc`:                 false,
		`(?i:abc)`:                false,
		`(?s).*`:                  false,
		`\Aabc\z`:                 false,
		`\Qa.b\E`:                 false,
		`[[:alpha:]]+`:            false,
		`\p{Greek}`:               false,
		`\pL`:                     false,
		`\x{10FFFF}`:              false,
		`^[\w.+-]+@[\w-]+\.\w+$`:  true,
		`^https?://[^\s/$.?#].*$`: true,
	}
	for pattern, want := range cases {
		if got := jsCompatiblePattern(pattern); got != want {
			t.Errorf("jsCompatiblePattern(%q) = %v, want %v", pattern, got, want)
		}
	}
}

func TestDefaultLiteral(t *testing.T) {
	stringsOf := &expr.Array{ElemType: &expr.AttributeExpr{Type: expr.String}}
	timesOf := &expr.Array{ElemType: str(&expr.ValidationExpr{Format: expr.FormatDateTime})}
	cases := []struct {
		name string
		att  *expr.AttributeExpr
		want string
		ok   bool
	}{
		{"none", &expr.AttributeExpr{Type: expr.String}, "", false},
		{"string", &expr.AttributeExpr{Type: expr.String, DefaultValue: `a "b"`}, `'a "b"'`, true},
		{"int", &expr.AttributeExpr{Type: expr.Int, DefaultValue: 3}, "3", true},
		{"zero", &expr.AttributeExpr{Type: expr.Int, DefaultValue: 0}, "0", true},
		{"float", &expr.AttributeExpr{Type: expr.Float64, DefaultValue: 0.25}, "0.25", true},
		{"bool", &expr.AttributeExpr{Type: expr.Boolean, DefaultValue: false}, "false", true},
		{
			"date-time",
			&expr.AttributeExpr{Type: expr.String, Validation: &expr.ValidationExpr{Format: expr.FormatDateTime}, DefaultValue: "2026-01-01T00:00:00Z"},
			`new Date('2026-01-01T00:00:00Z')`, true,
		},
		{"bytes", &expr.AttributeExpr{Type: expr.Bytes, DefaultValue: []byte("x")}, "", false},
		{"array", &expr.AttributeExpr{Type: stringsOf, DefaultValue: []any{"a", "b"}}, `['a','b']`, true},
		{"array of date-time", &expr.AttributeExpr{Type: timesOf, DefaultValue: []any{"2026-01-01T00:00:00Z"}}, "", false},
		{
			"map",
			&expr.AttributeExpr{
				Type:         &expr.Map{KeyType: &expr.AttributeExpr{Type: expr.String}, ElemType: &expr.AttributeExpr{Type: expr.Int}},
				DefaultValue: map[any]any{"a": 1},
			},
			`{'a':1}`, true,
		},
		{"any", &expr.AttributeExpr{Type: expr.Any, DefaultValue: map[string]any{"k": []any{1, "x"}}}, `{'k':[1,'x']}`, true},
		{"object", &expr.AttributeExpr{Type: &expr.Object{}, DefaultValue: map[string]any{}}, "", false},
	}
	for _, tc := range cases {
		got, ok := defaultLiteral(tc.att)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: defaultLiteral = (%q, %v), want (%q, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func TestJSONKey(t *testing.T) {
	tagged := func(tag ...string) *expr.AttributeExpr {
		return &expr.AttributeExpr{Type: expr.String, Meta: expr.MetaExpr{"struct:tag:json": tag}}
	}
	cases := []struct {
		name string
		attr string
		att  *expr.AttributeExpr
		want wireKey
	}{
		{"plain", "created_at", &expr.AttributeExpr{Type: expr.String}, wireKey{Key: "created_at"}},
		{"nil attribute", "x", nil, wireKey{Key: "x"}},
		{"mapped name", "page_size:pageSize", &expr.AttributeExpr{Type: expr.String}, wireKey{Key: "page_size"}},
		{"override", "legacy_name", tagged("legacyName"), wireKey{Key: "legacyName", NullWhenUnset: true}},
		{"override with omitempty", "a", tagged("b,omitempty"), wireKey{Key: "b"}},
		{"override split across values", "a", tagged("b", "omitempty"), wireKey{Key: "b"}},
		{"override with other option", "a", tagged("b,string"), wireKey{Key: "b", NullWhenUnset: true}},
		{"empty name keeps the attribute name", "a", tagged(",omitempty"), wireKey{Key: "a"}},
		{"omitted", "secret", tagged("-"), wireKey{Key: "secret", Omit: true}},
		{"dash as a name", "dash", tagged("-,"), wireKey{Key: "-", NullWhenUnset: true}},
		{
			"go-only metadata is ignored", "a",
			&expr.AttributeExpr{Type: expr.String, Meta: expr.MetaExpr{
				"struct:field:name":    {"Renamed"},
				"struct:tag:json:name": {"other"},
				"openapi:name":         {"x"},
			}},
			wireKey{Key: "a"},
		},
	}
	for _, tc := range cases {
		if got := jsonKey(tc.attr, tc.att); got != tc.want {
			t.Errorf("%s: jsonKey = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}
