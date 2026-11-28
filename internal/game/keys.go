package game

import (
	"cmp"
	"maps"
	"slices"
)

// sortedKeys returns a map's keys in order, for loops whose result must not
// depend on Go's random map order (float sums, picks, golden results).
func sortedKeys[M ~map[K]V, K cmp.Ordered, V any](m M) []K {
	return slices.Sorted(maps.Keys(m))
}
