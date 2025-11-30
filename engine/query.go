package engine

import "sort"

// QueryBuilder queries entities by component intersection, optimizing by starting with the smallest store
type QueryBuilder struct {
	world    *World
	stores   []QueryableStore
	executed bool
	results  []Entity
}

// Query creates a new QueryBuilder for finding entities with specific component combinations
// Use With() to add component filters, then Execute() to get the results
//
// Example:
//
//	entities := world.Query().
//	    With(world.Positions).
//	    With(world.Characters).
//	    Execute()
func (w *World) Query() *QueryBuilder {
	return &QueryBuilder{
		world:   w,
		stores:  make([]QueryableStore, 0, 4), // Pre-allocate for common case
		results: nil,
	}
}

// With filters entities that have components in ALL specified stores
// Panics if called after Execute()
func (qb *QueryBuilder) With(store QueryableStore) *QueryBuilder {
	if qb.executed {
		panic("query already executed - cannot modify after Execute()")
	}
	qb.stores = append(qb.stores, store)
	return qb
}

// Execute runs the query, returning entities in all specified stores
// Optimizes by sorting stores by size. Results are cached on subsequent calls
func (qb *QueryBuilder) Execute() []Entity {
	if qb.executed {
		return qb.results
	}
	qb.executed = true

	// Empty query returns no results
	if len(qb.stores) == 0 {
		qb.results = make([]Entity, 0)
		return qb.results
	}

	// Single store: just return all entities from that store
	if len(qb.stores) == 1 {
		qb.results = qb.stores[0].All()
		return qb.results
	}

	// Sort stores by count (ascending) for optimal intersection performance
	// Starting with the smallest store minimizes the number of Has() checks
	sort.Slice(qb.stores, func(i, j int) bool {
		return qb.stores[i].Count() < qb.stores[j].Count()
	})

	// Start with smallest store as initial candidates
	candidates := qb.stores[0].All()

	// Filter candidates through remaining stores
	// We reuse the candidates slice to avoid allocations
	for i := 1; i < len(qb.stores); i++ {
		store := qb.stores[i]
		filtered := candidates[:0] // Reuse underlying array
		for _, e := range candidates {
			if store.Has(e) {
				filtered = append(filtered, e)
			}
		}
		candidates = filtered

		// Early exit if no candidates remain
		if len(candidates) == 0 {
			break
		}
	}

	qb.results = candidates
	return qb.results
}