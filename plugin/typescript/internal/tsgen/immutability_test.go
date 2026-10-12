package tsgen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"reflect"
	"sort"
	"testing"

	"goa.design/goa/v3/expr"

	tsruntime "github.com/xeger/goa-typescript/runtime"
)

// goa snapshots the design before plugins run and fails the generation when
// one of them changes it. TestBuildDoesNotMutateDesign holds the plugin to
// that rule for every fixture: the whole expression graph must hash the same
// before and after Build and Render.
func TestBuildDoesNotMutateDesign(t *testing.T) {
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			root := design(t, fx.DSL)
			before := hashDesign(root)
			pkg, err := Build([]*expr.RootExpr{root}, Options{Runtime: tsruntime.Sorted()})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			dir := t.TempDir()
			for _, f := range append(Render(pkg), DebugFile(pkg)) {
				if _, err := f.Render(dir); err != nil {
					t.Fatalf("render %s: %v", f.Path, err)
				}
			}
			if after := hashDesign(root); after != before {
				t.Errorf("Build or Render mutated the design: hash %s became %s", before, after)
			}
		})
	}
}

// The hash must notice the mutations goa forbids, or the test above proves
// nothing.
func TestHashDesignDetectsMutations(t *testing.T) {
	mutations := map[string]func(root *expr.RootExpr){
		"meta added to a method": func(root *expr.RootExpr) {
			m := root.Services[0].Methods[0]
			if m.Meta == nil {
				m.Meta = expr.MetaExpr{}
			}
			m.Meta["x"] = []string{"y"}
		},
		"attribute meta added": func(root *expr.RootExpr) {
			root.Types[0].Attribute().AddMeta("x", "y")
		},
		"type renamed": func(root *expr.RootExpr) {
			root.Types[0].(*expr.UserTypeExpr).TypeName = "Other"
		},
		"validation merged": func(root *expr.RootExpr) {
			att := root.Types[0].Attribute()
			if att.Validation == nil {
				att.Validation = &expr.ValidationExpr{}
			}
			att.Validation.AddRequired("at")
		},
		"attribute deleted": func(root *expr.RootExpr) {
			root.Types[0].Attribute().Delete("at")
		},
		"header mapped": func(root *expr.RootExpr) {
			root.API.HTTP.Services[0].HTTPEndpoints[0].Headers.Map("X-Other", "trace")
		},
	}
	names := make([]string, 0, len(mutations))
	for name := range mutations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			root := design(t, fixtures[11].DSL) // SSEWholeEvent
			if fixtures[11].Name != "SSEWholeEvent" {
				t.Fatalf("fixture 11 is %s", fixtures[11].Name)
			}
			before := hashDesign(root)
			if again := hashDesign(root); again != before {
				t.Fatalf("hash is not stable: %s then %s", before, again)
			}
			mutations[name](root)
			if hashDesign(root) == before {
				t.Errorf("the hash did not change")
			}
		})
	}
}

// hashDesign hashes everything reachable from the root: every field, exported
// or not, following pointers once. Functions are skipped; they cannot change.
func hashDesign(root *expr.RootExpr) string {
	h := &hasher{sum: sha256.New(), seen: make(map[visit]int)}
	h.value(reflect.ValueOf(root))
	return hex.EncodeToString(h.sum.Sum(nil))
}

type visit struct {
	ptr uintptr
	typ reflect.Type
}

type hasher struct {
	sum  hash.Hash
	seen map[visit]int
}

func (h *hasher) write(format string, args ...any) {
	fmt.Fprintf(h.sum, format, args...)
}

func (h *hasher) value(v reflect.Value) {
	if !v.IsValid() {
		h.write("<invalid>")
		return
	}
	h.write("%s:", v.Type())
	switch v.Kind() {
	case reflect.Bool:
		h.write("%t;", v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		h.write("%d;", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		h.write("%d;", v.Uint())
	case reflect.Float32, reflect.Float64:
		h.write("%v;", v.Float())
	case reflect.Complex64, reflect.Complex128:
		h.write("%v;", v.Complex())
	case reflect.String:
		h.write("%q;", v.String())
	case reflect.Func, reflect.Chan, reflect.UnsafePointer:
		h.write("<%t>;", v.IsNil())
	case reflect.Interface:
		if v.IsNil() {
			h.write("<nil>;")
			return
		}
		h.value(v.Elem())
	case reflect.Pointer:
		if v.IsNil() {
			h.write("<nil>;")
			return
		}
		key := visit{ptr: v.Pointer(), typ: v.Type()}
		if id, ok := h.seen[key]; ok {
			h.write("<ref %d>;", id)
			return
		}
		h.seen[key] = len(h.seen)
		h.value(v.Elem())
	case reflect.Slice:
		if v.IsNil() {
			h.write("<nil>;")
			return
		}
		fallthrough
	case reflect.Array:
		h.write("[%d]{", v.Len())
		for i := 0; i < v.Len(); i++ {
			h.value(v.Index(i))
		}
		h.write("}")
	case reflect.Map:
		if v.IsNil() {
			h.write("<nil>;")
			return
		}
		// Entries are hashed on their own and combined in sorted order, so
		// the result does not depend on iteration order. Keys reached this
		// way are scalars or already-numbered pointers.
		entries := make([]string, 0, v.Len())
		for it := v.MapRange(); it.Next(); {
			sub := &hasher{sum: sha256.New(), seen: h.seen}
			sub.value(it.Key())
			sub.write("=>")
			sub.value(it.Value())
			entries = append(entries, hex.EncodeToString(sub.sum.Sum(nil)))
		}
		sort.Strings(entries)
		h.write("map[%d]%v;", len(entries), entries)
	case reflect.Struct:
		h.write("{")
		for i := 0; i < v.NumField(); i++ {
			h.write("%s=", v.Type().Field(i).Name)
			h.value(v.Field(i))
		}
		h.write("}")
	default:
		h.write("<%s>;", v.Kind())
	}
}
