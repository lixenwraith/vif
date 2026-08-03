package persistence

import (
	"os"
	"path/filepath"
)

// Store abstracts population persistence for the registry
type Store interface {
	Save(name string, dto PopulationDTO) error
	Load(name string) (PopulationDTO, error)
}

// Manager persists populations as one file per species
type Manager struct {
	codec    Codec
	basePath string
}

// NewManager creates a file-backed store; a nil codec selects TOML
func NewManager(basePath string, codec Codec) *Manager {
	if codec == nil {
		codec = TOMLCodec{}
	}
	return &Manager{basePath: basePath, codec: codec}
}

func (m *Manager) FilePath(name string) string {
	return filepath.Join(m.basePath, name+m.codec.Ext())
}

func (m *Manager) Exists(name string) bool {
	_, err := os.Stat(m.FilePath(name))
	return err == nil
}

// Save writes atomically via temp file and rename
func (m *Manager) Save(name string, dto PopulationDTO) error {
	if err := os.MkdirAll(m.basePath, 0o755); err != nil {
		return err
	}

	dto.Version = SchemaVersion
	data, err := m.codec.Marshal(dto)
	if err != nil {
		return err
	}

	final := m.FilePath(name)
	tmp, err := os.CreateTemp(m.basePath, name+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), final)
}

func (m *Manager) Load(name string) (PopulationDTO, error) {
	var dto PopulationDTO

	data, err := os.ReadFile(m.FilePath(name))
	if err != nil {
		return dto, err
	}
	if err := m.codec.Unmarshal(data, &dto); err != nil {
		return dto, err
	}
	return dto, nil
}
