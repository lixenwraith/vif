package component

// GenotypeComponent stores evolution data for tracked entities
type GenotypeComponent struct {
	Genes   []float64
	EvalID  uint64
	Species SpeciesType
	SubType uint8

	// Observed metrics (updated by GeneticSystem)
	CumulativeDistSq float64
	DistSamples      int
}

// GeneticStats holds telemetry for a species population
type GeneticStats struct {
	Generation    int
	Best          float64
	Worst         float64
	Avg           float64
	PendingCount  int
	OutcomesTotal uint64
}

// DetachSnapshot returns a copy whose gene vector shares no storage with this
// one; GeneticSystem writes genes in place. See HeaderComponent.DetachSnapshot.
func (g GenotypeComponent) DetachSnapshot() GenotypeComponent {
	g.Genes = append([]float64(nil), g.Genes...)
	return g
}
