package genetic

import (
	"math"
	"math/rand/v2"
	"sort"
)

// ParameterBounds defines the closed interval for one gene
type ParameterBounds struct {
	Min, Max float64
}

// Bin maps v to one of n equal sub-intervals of the bounds, returning [0, n-1].
// Half-open at the top of each bin, so Max lands in n-1 without a special case
func (b ParameterBounds) Bin(v float64, n int) int {
	if n <= 1 || v != v {
		return 0
	}
	span := b.Max - b.Min
	if span <= 0 {
		return 0
	}
	i := int(float64(n) * (v - b.Min) / span)
	if i < 0 {
		return 0
	}
	if i >= n {
		return n - 1
	}
	return i
}

// BinCenter returns the midpoint of bin i of n. Bin(BinCenter(i, n), n) == i
func (b ParameterBounds) BinCenter(i, n int) float64 {
	if n <= 0 {
		return (b.Min + b.Max) / 2
	}
	i = min(max(i, 0), n-1)
	return b.Min + (float64(i)+0.5)/float64(n)*(b.Max-b.Min)
}

// BoundaryMode selects how an out-of-range mutation returns to the interval
type BoundaryMode uint8

const (
	// BoundaryClamp pins to the nearest bound; accumulates mass at both ends
	BoundaryClamp BoundaryMode = iota
	// BoundaryReflect folds back into the interval, preserving density
	BoundaryReflect
	// BoundaryWrap treats the interval as circular; result is in [Min, Max)
	BoundaryWrap
)

// constrain returns v mapped into the interval under the given mode
func (b ParameterBounds) constrain(v float64, mode BoundaryMode) float64 {
	if v >= b.Min && v <= b.Max {
		return v
	}
	span := b.Max - b.Min
	if span <= 0 {
		return b.Min
	}

	switch mode {
	case BoundaryReflect:
		t := math.Mod(v-b.Min, 2*span)
		if t < 0 {
			t += 2 * span
		}
		if t > span {
			t = 2*span - t
		}
		return b.Min + t

	case BoundaryWrap:
		t := math.Mod(v-b.Min, span)
		if t < 0 {
			t += span
		}
		return b.Min + t

	default:
		if v < b.Min {
			return b.Min
		}
		return b.Max
	}
}

// --- Selection ---

// TournamentSelector samples k members uniformly and keeps the best
type TournamentSelector[S Solution, F Numeric] struct {
	TournamentSize int
}

func (ts *TournamentSelector[S, F]) Select(members, dst []Candidate[S, F], rng *rand.Rand) {
	n := len(members)
	if n == 0 {
		return
	}
	k := min(max(ts.TournamentSize, 2), n)

	for i := range dst {
		best := members[rng.IntN(n)]
		for j := 1; j < k; j++ {
			if c := members[rng.IntN(n)]; c.Score > best.Score {
				best = c
			}
		}
		dst[i] = best
	}
}

// RouletteSelector picks proportionally to score, shifted to tolerate negatives.
// Not goroutine-safe; the owning engine serializes access
type RouletteSelector[S Solution, F Numeric] struct {
	cum []float64
	// Floor is added to every shifted weight so the worst member stays selectable
	Floor float64
}

func (rs *RouletteSelector[S, F]) Select(members, dst []Candidate[S, F], rng *rand.Rand) {
	n := len(members)
	if n == 0 {
		return
	}

	if cap(rs.cum) < n {
		rs.cum = make([]float64, n)
	}
	rs.cum = rs.cum[:n]

	floor := rs.Floor
	if floor <= 0 {
		floor = 1e-9
	}

	minScore := float64(members[0].Score)
	for i := 1; i < n; i++ {
		if v := float64(members[i].Score); v < minScore {
			minScore = v
		}
	}

	total := 0.0
	for i := range members {
		total += float64(members[i].Score) - minScore + floor
		rs.cum[i] = total
	}

	for i := range dst {
		idx := sort.SearchFloat64s(rs.cum, rng.Float64()*total)
		if idx >= n {
			idx = n - 1
		}
		dst[i] = members[idx]
	}
}

// --- Recombination ---

// UniformCombiner mixes parents element-wise, reusing dst buffers
type UniformCombiner[S ~[]T, T any, F Numeric] struct {
	MixProbability float64
}

func (uc *UniformCombiner[S, T, F]) Combine(parents []Candidate[S, F], dst []S, rng *rand.Rand) int {
	if len(parents) == 0 || len(dst) == 0 {
		return 0
	}

	p1 := parents[0].Data
	p2 := p1
	if len(parents) > 1 {
		p2 = parents[1].Data
	}

	length := min(len(p1), len(p2))
	out := min(len(dst), 2)
	for i := range out {
		dst[i] = resize(dst[i], length)
	}

	for i := range length {
		a, b := p1[i], p2[i]
		if rng.Float64() >= uc.MixProbability {
			a, b = b, a
		}
		dst[0][i] = a
		if out > 1 {
			dst[1][i] = b
		}
	}
	return out
}

// NPointCombiner alternates parent segments at N crossover points
type NPointCombiner[S ~[]T, T any, F Numeric] struct {
	Points int

	cuts []int
}

func (nc *NPointCombiner[S, T, F]) Combine(parents []Candidate[S, F], dst []S, rng *rand.Rand) int {
	if len(parents) < 2 || len(dst) == 0 {
		return 0
	}

	p1, p2 := parents[0].Data, parents[1].Data
	length := min(len(p1), len(p2))
	out := min(len(dst), 2)
	for i := range out {
		dst[i] = resize(dst[i], length)
	}

	nc.cuts = nc.cuts[:0]
	for i := 0; i < nc.Points && length > 1; i++ {
		nc.cuts = append(nc.cuts, rng.IntN(length-1)+1)
	}
	sort.Ints(nc.cuts)
	nc.cuts = append(nc.cuts, length)

	start, swap := 0, false
	for _, end := range nc.cuts {
		for j := start; j < end; j++ {
			a, b := p1[j], p2[j]
			if swap {
				a, b = b, a
			}
			dst[0][j] = a
			if out > 1 {
				dst[1][j] = b
			}
		}
		start, swap = end, !swap
	}
	return out
}

// --- Mutation ---

// BoundedPerturbator applies Gaussian noise scaled to each gene's range and
// returns out-of-range results to the interval per Boundary
type BoundedPerturbator struct {
	Bounds []ParameterBounds
	// Boundary defaults to BoundaryClamp
	Boundary BoundaryMode
}

func (bp *BoundedPerturbator) Perturb(solution *[]float64, rate, strength float64, rng *rand.Rand) {
	if solution == nil || strength == 0 {
		return
	}

	s := *solution
	for i := range s {
		if i >= len(bp.Bounds) {
			return
		}
		if rng.Float64() >= rate {
			continue
		}

		b := bp.Bounds[i]
		v := s[i] + rng.NormFloat64()*strength*(b.Max-b.Min)
		if v != v { // NaN from a non-finite range; leave the gene untouched
			continue
		}
		s[i] = b.constrain(v, bp.Boundary)
	}
}

// Clamp enforces bounds in place
func (bp *BoundedPerturbator) Clamp(solution []float64) {
	for i := range solution {
		if i >= len(bp.Bounds) {
			return
		}
		b := bp.Bounds[i]
		if solution[i] < b.Min {
			solution[i] = b.Min
		} else if solution[i] > b.Max {
			solution[i] = b.Max
		}
	}
}

// GaussianPerturbator adds unbounded Gaussian noise to numeric genotypes
type GaussianPerturbator[S ~[]F, F Numeric] struct{}

func (gp *GaussianPerturbator[S, F]) Perturb(solution *S, rate, strength float64, rng *rand.Rand) {
	if solution == nil {
		return
	}
	for i := range *solution {
		if rng.Float64() < rate {
			(*solution)[i] += F(rng.NormFloat64() * strength)
		}
	}
}

func resize[S ~[]T, T any](s S, n int) S {
	if cap(s) >= n {
		return s[:n]
	}
	return make(S, n)
}
