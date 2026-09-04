// Package deps validates the dependency sets services are constructed from.
//
// It exists because every bounded context builds its services the same way and
// fails the same way when one is missed: a nil interface field that reads as a
// behaviour change rather than an error. Keeping the check here means each
// context reports missing dependencies in one voice, and the report names the
// Deps fields the caller has to go and fill in.
package deps

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Missing reports every nil entry at once rather than the first one.
//
// Boot is a slow edit-retry loop, so naming one missing dependency per restart
// turns a single mistake into as many restarts as there are fields.
//
// The reflect check is what makes this catch a typed nil pointer: a nil *T
// stored in an interface reads as non-nil to a plain == comparison, which is
// exactly the shape a caller produces by passing along a constructor that
// returned nil on failure.
func Missing(fields map[string]any) error {
	var missing []string
	for name, value := range fields {
		if isNil(value) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return fmt.Errorf("missing required dependencies: %s", strings.Join(missing, ", "))
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	default:
		return false
	}
}
