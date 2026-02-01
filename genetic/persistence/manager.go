package persistence

import (
	"os"
	"path/filepath"

	"github.com/lixenwraith/vi-fighter/toml"
)

// Manager handles save/load for species populations
type Manager struct {
	basePath string
}

// NewManager creates a manager with the given base directory
func NewManager(basePath string) *Manager {
	return &Manager{basePath: basePath}
}

// FilePath returns the path for a species file
func (m *Manager) FilePath(speciesName string) string {
	return filepath.Join(m.basePath, speciesName+".toml")
}

// Exists checks if a population file exists
func (m *Manager) Exists(speciesName string) bool {
	_, err := os.Stat(m.FilePath(speciesName))
	return err == nil
}

// Save writes population to disk
func (m *Manager) Save(speciesName string, dto PopulationDTO) error {
	if err := os.MkdirAll(m.basePath, 0755); err != nil {
		return err
	}

	data, err := toml.Marshal(dto)
	if err != nil {
		return err
	}

	return os.WriteFile(m.FilePath(speciesName), data, 0644)
}

// Load reads population from disk
func (m *Manager) Load(speciesName string) (PopulationDTO, error) {
	var dto PopulationDTO

	data, err := os.ReadFile(m.FilePath(speciesName))
	if err != nil {
		return dto, err
	}

	if err := toml.Unmarshal(data, &dto); err != nil {
		return dto, err
	}

	return dto, nil
}