package game

import "sync"

// keyIndex finds catalogue entries by key without scanning the catalogue.
// It indexes positions in the slice the variable holds, so entries edited
// in place are seen, and it reindexes when the catalogue has grown (the
// catalogues are appended to in init). Safe for concurrent use.
type keyIndex[K comparable, V any] struct {
	list *[]V
	key  func(V) K

	mu  sync.RWMutex
	n   int // len(*list) when indexed
	pos map[K]int
}

func newIndex[K comparable, V any](list *[]V, key func(V) K) *keyIndex[K, V] {
	return &keyIndex[K, V]{list: list, key: key}
}

func (x *keyIndex[K, V]) get(k K) (V, bool) {
	x.mu.RLock()
	i, ok := x.pos[k]
	stale := x.n != len(*x.list)
	x.mu.RUnlock()
	if stale || (ok && (i >= len(*x.list) || x.key((*x.list)[i]) != k)) {
		x.rebuild()
		x.mu.RLock()
		i, ok = x.pos[k]
		x.mu.RUnlock()
	}
	if !ok {
		var zero V
		return zero, false
	}
	return (*x.list)[i], true
}

func (x *keyIndex[K, V]) rebuild() {
	x.mu.Lock()
	defer x.mu.Unlock()
	list := *x.list
	x.pos = make(map[K]int, len(list))
	for i, v := range list {
		k := x.key(v)
		if _, dup := x.pos[k]; !dup { // the first entry wins, as a scan would
			x.pos[k] = i
		}
	}
	x.n = len(list)
}

var (
	matterIndex    = newIndex(&MatterEvents, func(e MatterEvent) string { return e.Key })
	itemIndex      = newIndex(&Items, func(it Item) string { return it.Key })
	lawIndex       = newIndex(&LawDefs, func(d LawDef) LawKey { return d.Key })
	traitIndex     = newIndex(&TraitList, func(t TraitInfo) Trait { return t.Key })
	conditionIndex = newIndex(&ConditionDefs, func(c ConditionDef) string { return c.Key })
	workIndex      = newIndex(&WorkDefs, func(w WorkDef) string { return w.Key })
	sectorIndex    = newIndex(&SectorDefs, func(d SectorDef) SectorKey { return d.Key })
	arcIndex       = newIndex(&Arcs, func(a ArcDef) string { return a.Key })
)
