package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/engine"
)

// FileSource supplies categorized configuration roots in lookup order.
// Existing paths remain explicit compatibility overrides.
type FileSource struct {
	Roots []string
}

// FileService owns runtime access to external files. It contributes only a
// narrow open capability; systems neither discover roots nor open host paths.
type FileService struct {
	roots []string
}

func NewFileService(src FileSource) *FileService {
	return &FileService{roots: append([]string(nil), src.Roots...)}
}

func (s *FileService) Name() string           { return "file" }
func (s *FileService) Dependencies() []string { return nil }
func (s *FileService) Init() error            { return nil }
func (s *FileService) Start() error           { return nil }
func (s *FileService) Stop() error            { return nil }

func (s *FileService) Contribute(r *engine.Resource) {
	r.Files = &engine.FileResource{Provider: s}
}

// Open opens an existing path directly. A missing path is instead treated as a
// logical name below category/ in each configured root. This keeps old authored
// relative paths working while making installed assets portable.
func (s *FileService) Open(category, name string) (io.ReadCloser, error) {
	if !validFileCategory(category) {
		return nil, fmt.Errorf("invalid file category %q", category)
	}
	if name == "" {
		return nil, fmt.Errorf("%s file name is empty", category)
	}

	f, err := openRegularFile(name)
	if err == nil {
		return f, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("open %s file %q: %w", category, name, err)
	}
	if !validLogicalFileName(name) {
		return nil, fmt.Errorf("%s file name %q escapes its category", category, name)
	}

	for _, root := range s.roots {
		candidate := filepath.Join(root, category, name)
		f, err = openRegularFile(candidate)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("open %s file %q: %w", category, name, err)
		}
	}
	return nil, fmt.Errorf("%s file %q not found as a path or in any configuration root: %w",
		category, name, os.ErrNotExist)
}

func openRegularFile(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, fmt.Errorf("%s is a directory", path)
	}
	return f, nil
}

func validFileCategory(category string) bool {
	if filepath.IsAbs(category) {
		return false
	}
	clean := filepath.Clean(category)
	return clean != "." && clean != ".." && filepath.Base(clean) == clean
}

func validLogicalFileName(name string) bool {
	if filepath.IsAbs(name) {
		return false
	}
	clean := filepath.Clean(name)
	return clean != "." && clean != ".." &&
		!strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
