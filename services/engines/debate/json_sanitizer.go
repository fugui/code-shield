package debate

import (
	"reflect"
	"strings"
)

// sanitizeNullStrings replaces Unicode NUL characters with the conventional C-style
// literal "\0". PostgreSQL cannot store NUL in text or JSONB, and AI output often
// uses \u0000 when quoting null-terminated C/C++ strings.
func sanitizeNullStrings(v any) {
	sanitizeNullStringsValue(reflect.ValueOf(v))
}

func sanitizeNullStringsValue(rv reflect.Value) {
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !rv.IsNil() {
			sanitizeNullStringsValue(rv.Elem())
		}
	case reflect.Struct:
		for i := 0; i < rv.NumField(); i++ {
			sanitizeNullStringsValue(rv.Field(i))
		}
	case reflect.Slice:
		for i := 0; i < rv.Len(); i++ {
			sanitizeNullStringsValue(rv.Index(i))
		}
	case reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			sanitizeNullStringsValue(rv.Index(i))
		}
	case reflect.String:
		if rv.CanSet() && strings.ContainsRune(rv.String(), '\x00') {
			rv.SetString(strings.ReplaceAll(rv.String(), "\x00", `\0`))
		}
	}
}
