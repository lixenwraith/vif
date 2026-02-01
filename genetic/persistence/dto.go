package persistence

import "github.com/lixenwraith/vi-fighter/genetic"

// PopulationDTO is the serializable population state
type PopulationDTO struct {
	Generation int            `toml:"generation"`
	Candidates []CandidateDTO `toml:"candidates"`
}

// CandidateDTO is a serializable candidate
type CandidateDTO struct {
	Genes []float64 `toml:"genes"`
	Score float64   `toml:"score"`
}

// FromPool converts engine pool to DTO
func FromPool(pool *genetic.Pool[[]float64, float64]) PopulationDTO {
	if pool == nil {
		return PopulationDTO{}
	}

	dto := PopulationDTO{
		Generation: pool.Generation,
		Candidates: make([]CandidateDTO, len(pool.Members)),
	}

	for i, m := range pool.Members {
		dto.Candidates[i] = CandidateDTO{
			Genes: m.Data,
			Score: m.Score,
		}
	}

	return dto
}

// ToPool converts DTO to candidates for injection
func (dto PopulationDTO) ToPool() []genetic.Candidate[[]float64, float64] {
	candidates := make([]genetic.Candidate[[]float64, float64], len(dto.Candidates))

	for i, c := range dto.Candidates {
		candidates[i] = genetic.Candidate[[]float64, float64]{
			Data:     c.Genes,
			Score:    c.Score,
			Metadata: make(map[string]any),
		}
	}

	return candidates
}