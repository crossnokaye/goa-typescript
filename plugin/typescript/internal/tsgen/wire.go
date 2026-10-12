package tsgen

import (
	"strings"

	"goa.design/goa/v3/expr"
)

// wireKey describes how goa names an object attribute in JSON.
type wireKey struct {
	// Key is the JSON object key.
	Key string
	// Omit is true when the attribute never appears in JSON (`json:"-"`).
	Omit bool
	// NullWhenUnset is true when goa writes an unset optional attribute as
	// null instead of leaving it out: a "struct:tag:json" override replaces
	// the whole tag, including the omitempty option goa would have added.
	NullWhenUnset bool
}

// jsonKey returns the JSON key of the attribute named name. goa uses the
// attribute name unless Meta("struct:tag:json", ...) overrides the tag.
// Go-only metadata (struct:field:name, struct:pkg:path, struct:field:type,
// struct:tag:json:name) and openapi:* do not change the wire format.
func jsonKey(name string, att *expr.AttributeExpr) wireKey {
	// Mapped attributes are named "attribute:element".
	name, _, _ = strings.Cut(name, ":")
	if att == nil {
		return wireKey{Key: name}
	}
	tag, ok := att.Meta["struct:tag:json"]
	if !ok || len(tag) == 0 {
		return wireKey{Key: name}
	}
	parts := strings.Split(strings.Join(tag, ","), ",")
	if parts[0] == "-" && len(parts) == 1 {
		return wireKey{Key: name, Omit: true}
	}
	key := parts[0]
	if key == "" {
		key = name
	}
	omitEmpty := false
	for _, opt := range parts[1:] {
		if opt == "omitempty" || opt == "omitzero" {
			omitEmpty = true
		}
	}
	return wireKey{Key: key, NullWhenUnset: !omitEmpty}
}
