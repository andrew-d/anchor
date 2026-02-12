// Package typekey derives unique type keys from Go types using reflect.
// These keys include the full package path and type name, suitable for
// deduplication at registration time.
package typekey

import (
	"fmt"
	"reflect"
)

// Of returns the full type path for T in the form "full/pkg/path.TypeName".
// It panics if T has no package path (e.g. builtin or unnamed types).
func Of[T any]() string {
	var zero T
	t := reflect.TypeOf(zero)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	pkg := t.PkgPath()
	if pkg == "" {
		panic(fmt.Sprintf("typekey: type %s has no package path", t.Name()))
	}
	return pkg + "." + t.Name()
}
